package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mex040708-afk/bitabit/server/internal/db"
	"github.com/mex040708-afk/bitabit/server/internal/library"
)

const (
	// ScanInterval y QuietPeriod son los de ADR-010: se mira la entrada cada
	// 30 s y un archivo está listo tras 1 minuto sin cambiar.
	ScanInterval = 30 * time.Second
	QuietPeriod  = time.Minute

	dirPerm        = 0o750
	filePerm       = 0o640
	workDirPrefix  = ".tmp-"
	errorNoteExt   = ".error.txt"
	maxFailedTries = 1000
)

// Outcome es lo que pasó con un archivo de la entrada.
type Outcome int

const (
	Imported  Outcome = iota + 1 // está en la biblioteca
	Duplicate                    // ya estaba: se movió a fallidos/
	Failed                       // no vale: se movió a fallidos/
)

func (o Outcome) String() string {
	switch o {
	case Imported:
		return "importado"
	case Duplicate:
		return "repetido"
	case Failed:
		return "fallido"
	}
	return "desconocido"
}

// Result describe cómo acabó la importación de un archivo.
type Result struct {
	Outcome Outcome
	TrackID int64  // la pista creada, o la que ya tenía esa fuente
	Reason  string // por qué fue a fallidos/
}

// Importer lleva los archivos de entrada/ a biblioteca/ y a la base de datos, o
// a fallidos/ si no valen. Importa de uno en uno: no admite llamadas
// simultáneas.
type Importer struct {
	inbox   string
	library string
	failed  string
	pool    *pgxpool.Pool
	log     *slog.Logger
}

// NewImporter prepara la importación en dataDir, que debe tener entrada/,
// biblioteca/ y fallidos/ (las crea setup-server.sh). No las crea él: si faltan,
// lo más probable es que BITABIT_DATA_DIR apunte a otro sitio. Sí crea
// biblioteca/orig, biblioteca/aac y biblioteca/covers.
func NewImporter(dataDir string, pool *pgxpool.Pool, log *slog.Logger) (*Importer, error) {
	if !filepath.IsAbs(dataDir) {
		return nil, fmt.Errorf("importación: la carpeta de datos debe ser una ruta absoluta: %q", dataDir)
	}
	im := &Importer{
		inbox:   filepath.Join(dataDir, "entrada"),
		library: filepath.Join(dataDir, "biblioteca"),
		failed:  filepath.Join(dataDir, "fallidos"),
		pool:    pool,
		log:     log,
	}
	for _, dir := range []string{im.inbox, im.library, im.failed} {
		fi, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("importación: %w", err)
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("importación: %s no es una carpeta", dir)
		}
	}
	for _, dir := range []string{im.versionDir(db.QualityOriginal), im.versionDir(db.QualityAAC256), im.coverDir()} {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return nil, fmt.Errorf("importación: crear %s: %w", dir, err)
		}
	}
	return im, nil
}

// Run importa lo que esté listo en la entrada cada interval, hasta que se
// cancele ctx. Los fallos del sistema se registran y el archivo se reintenta en
// la siguiente pasada.
func (im *Importer) Run(ctx context.Context, interval, quiet time.Duration) error {
	inbox := NewInbox(im.inbox, quiet)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		im.importReady(ctx, inbox)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (im *Importer) importReady(ctx context.Context, inbox *Inbox) {
	ready, err := inbox.Scan(time.Now())
	if err != nil {
		im.log.Error("no se pudo mirar la entrada", "error", err)
		return
	}
	for _, c := range ready {
		if ctx.Err() != nil {
			return
		}
		res, err := im.Import(ctx, c.Path)
		switch {
		case err != nil:
			im.log.Error("no se pudo importar; se reintentará", "archivo", c.Path, "error", err)
		case res.Outcome == Imported:
			im.log.Info("importado", "archivo", c.Path, "pista", res.TrackID)
		default:
			im.log.Warn("movido a fallidos/", "archivo", c.Path, "resultado", res.Outcome, "motivo", res.Reason)
		}
	}
}

// Import importa un archivo de la entrada; rel es su ruta relativa con '/', como
// la da Inbox. Si devuelve error, el problema es del sistema y el archivo sigue
// en la entrada para reintentarlo.
func (im *Importer) Import(ctx context.Context, rel string) (res Result, err error) {
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return Result{}, fmt.Errorf("importar: ruta fuera de la entrada: %q", rel)
	}
	src := filepath.Join(im.inbox, filepath.FromSlash(rel))

	sum, _, err := hashFile(src)
	if err != nil {
		return Result{}, fmt.Errorf("importar %s: %w", rel, err)
	}
	id, found, err := db.TrackIDBySource(ctx, im.pool, sum)
	if err != nil {
		return Result{}, fmt.Errorf("importar %s: %w", rel, err)
	}
	if found {
		return im.fail(rel, src, Duplicate, id, fmt.Sprintf("ya importado como pista %d", id))
	}

	info, err := Probe(ctx, src)
	if errors.Is(err, ErrUnreadable) {
		return im.fail(rel, src, Failed, 0, err.Error())
	}
	if err != nil {
		return Result{}, fmt.Errorf("importar %s: %w", rel, err)
	}

	work, err := os.MkdirTemp(im.library, workDirPrefix)
	if err != nil {
		return Result{}, fmt.Errorf("importar %s: crear la carpeta de trabajo: %w", rel, err)
	}
	defer func() {
		if rerr := os.RemoveAll(work); rerr != nil {
			err = errors.Join(err, fmt.Errorf("borrar la carpeta de trabajo: %w", rerr))
		}
	}()

	v, err := BuildVersions(ctx, src, info, work)
	if errors.Is(err, ErrUnreadable) {
		return im.fail(rel, src, Failed, 0, err.Error())
	}
	if err != nil {
		return Result{}, fmt.Errorf("importar %s: %w", rel, err)
	}

	cover, err := im.stageCover(ctx, rel, src, work)
	if err != nil {
		return Result{}, fmt.Errorf("importar %s: %w", rel, err)
	}

	id, err = im.store(ctx, rel, sum, info, v, cover)
	if err != nil {
		return Result{}, fmt.Errorf("importar %s: %w", rel, err)
	}
	// La pista ya está guardada. Si no se puede borrar la fuente, en la próxima
	// pasada saldrá como repetida y acabará en fallidos/.
	if err := os.Remove(src); err != nil {
		im.log.Error("importado, pero no se pudo borrar de la entrada", "archivo", rel, "pista", id, "error", err)
	} else {
		im.removeEmptyDirs(filepath.Dir(src))
	}
	return Result{Outcome: Imported, TrackID: id}, nil
}

// store guarda la pista y coloca sus versiones y su carátula. Se renombran a su
// sitio dentro de la transacción y antes de confirmarla: si la confirmación
// falla, se borran.
func (im *Importer) store(ctx context.Context, rel string, sum [32]byte, info Info, v Versions, cover Cover) (id int64, err error) {
	files, err := trackFiles(info, v)
	if err != nil {
		return 0, err
	}

	tx, err := im.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("empezar la transacción: %w", err)
	}
	defer func() {
		if rerr := tx.Rollback(ctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("deshacer la transacción: %w", rerr))
		}
	}()

	id, err = db.InsertTrack(ctx, tx, newTrack(rel, sum, info), files)
	if err != nil {
		return 0, err
	}
	placed, err := im.place(id, v)
	if err == nil {
		placed, err = im.placeCover(ctx, tx, id, cover, placed)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		for _, p := range placed {
			if rerr := os.Remove(p); rerr != nil {
				err = errors.Join(err, fmt.Errorf("retirar %s: %w", p, rerr))
			}
		}
		return 0, err
	}
	return id, nil
}

// place mueve las versiones a biblioteca/ con su nombre por ID y devuelve las
// rutas que llegó a colocar.
func (im *Importer) place(id int64, v Versions) ([]string, error) {
	moves := [][2]string{{v.Original, im.versionPath(db.QualityOriginal, id, v.OriginalExt)}}
	if v.AAC != "" {
		moves = append(moves, [2]string{v.AAC, im.versionPath(db.QualityAAC256, id, "m4a")})
	}
	return placeAll(moves)
}

// placeAll mueve cada origen a su destino y devuelve los destinos que llegó a
// colocar. Un destino que ya existe no se pisa: o la base de datos no es la de
// esta biblioteca, o un corte dejó una carátula sin dueño (ADR-015). En los dos
// casos, mejor parar.
func placeAll(moves [][2]string) ([]string, error) {
	var placed []string
	for _, m := range moves {
		src, dst := m[0], m[1]
		if _, err := os.Lstat(dst); err == nil {
			return placed, fmt.Errorf("ya existe %s: ¿la base de datos no corresponde a esta biblioteca?", dst)
		} else if !errors.Is(err, os.ErrNotExist) {
			return placed, fmt.Errorf("comprobar %s: %w", dst, err)
		}
		if err := os.Rename(src, dst); err != nil {
			return placed, fmt.Errorf("colocar %s: %w", dst, err)
		}
		placed = append(placed, dst)
		if err := syncDir(filepath.Dir(dst)); err != nil {
			return placed, err
		}
	}
	return placed, nil
}

func trackFiles(info Info, v Versions) ([]db.TrackFile, error) {
	orig := db.TrackFile{
		Quality:    db.QualityOriginal,
		Ext:        v.OriginalExt,
		Codec:      info.Codec,
		Lossless:   info.Lossless,
		SampleRate: info.SampleRate,
		Bits:       info.Bits,
		Channels:   info.Channels,
	}
	if info.Lossless {
		orig.Codec = "flac" // WAV, AIFF y ALAC se guardan comprimidos a FLAC
	}
	if v.AudioMD5 != "" {
		md5, err := hex.DecodeString(v.AudioMD5)
		if err != nil {
			return nil, fmt.Errorf("MD5 del audio no válido %q: %w", v.AudioMD5, err)
		}
		orig.AudioMD5 = md5
	}
	var err error
	if orig.SHA256, orig.Bytes, err = hashFile(v.Original); err != nil {
		return nil, err
	}
	files := []db.TrackFile{orig}

	if v.AAC != "" {
		aac := db.TrackFile{
			Quality:    db.QualityAAC256,
			Ext:        "m4a",
			Codec:      v.AACInfo.Codec,
			SampleRate: v.AACInfo.SampleRate,
			Channels:   v.AACInfo.Channels,
		}
		if aac.SHA256, aac.Bytes, err = hashFile(v.AAC); err != nil {
			return nil, err
		}
		files = append(files, aac)
	}
	return files, nil
}

// newTrack toma las etiquetas de Probe. Sin título, usa el nombre del archivo.
func newTrack(rel string, sum [32]byte, info Info) db.NewTrack {
	tags := info.Tags
	title := tags.Title
	if title == "" {
		title = cleanTag(strings.TrimSuffix(path.Base(rel), path.Ext(rel)))
	}
	if title == "" {
		title = "Sin título"
	}
	return db.NewTrack{
		Title:        title,
		Artist:       tags.Artist,
		Album:        tags.Album,
		AlbumArtist:  tags.AlbumArtist,
		ReleaseDate:  tags.Date,
		TrackNumber:  tags.Track,
		TrackTotal:   tags.TrackTotal,
		DiscNumber:   tags.Disc,
		DiscTotal:    tags.DiscTotal,
		Duration:     info.Duration,
		SourceSHA256: sum,
	}
}

// fail mueve la fuente a fallidos/ con la misma ruta relativa y deja al lado una
// nota con el motivo.
func (im *Importer) fail(rel, src string, outcome Outcome, id int64, reason string) (Result, error) {
	dst, err := im.failedPath(rel)
	if err != nil {
		return Result{}, fmt.Errorf("mover %s a fallidos/: %w", rel, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), dirPerm); err != nil {
		return Result{}, fmt.Errorf("mover %s a fallidos/: %w", rel, err)
	}
	if err := os.Rename(src, dst); err != nil {
		return Result{}, fmt.Errorf("mover %s a fallidos/: %w", rel, err)
	}
	note := fmt.Sprintf("Archivo: %s\nFecha: %s\nMotivo: %s\n", rel, time.Now().Format(time.RFC3339), reason)
	// El archivo ya está en fallidos/: sin la nota se ve igual, así que basta
	// con registrarlo.
	if err := os.WriteFile(dst+errorNoteExt, []byte(note), filePerm); err != nil {
		im.log.Error("no se pudo escribir la nota del fallo", "archivo", dst, "error", err)
	}
	im.removeEmptyDirs(filepath.Dir(src))
	return Result{Outcome: outcome, TrackID: id, Reason: reason}, nil
}

// failedPath busca en fallidos/ un nombre libre para rel, sin pisar ni el
// archivo ni su nota: «a.flac», «a (2).flac», «a (3).flac»…
func (im *Importer) failedPath(rel string) (string, error) {
	base := filepath.Join(im.failed, filepath.FromSlash(rel))
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for n := 1; n <= maxFailedTries; n++ {
		candidate := base
		if n > 1 {
			candidate = stem + " (" + strconv.Itoa(n) + ")" + ext
		}
		free, err := notExists(candidate, candidate+errorNoteExt)
		if err != nil {
			return "", err
		}
		if free {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("hay más de %d archivos llamados %s", maxFailedTries, rel)
}

func notExists(paths ...string) (bool, error) {
	for _, p := range paths {
		_, err := os.Lstat(p)
		if err == nil {
			return false, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return true, nil
}

// removeEmptyDirs borra dir y sus padres mientras estén vacíos, sin llegar a
// borrar entrada/. Lo que no es audio se queda, y con ello su carpeta.
func (im *Importer) removeEmptyDirs(dir string) {
	for dir != im.inbox && strings.HasPrefix(dir, im.inbox+string(filepath.Separator)) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			im.log.Error("no se pudo mirar una carpeta de la entrada", "carpeta", dir, "error", err)
			return
		}
		if len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			im.log.Error("no se pudo borrar una carpeta vacía de la entrada", "carpeta", dir, "error", err)
			return
		}
		dir = filepath.Dir(dir)
	}
}

// Recover se ejecuta al arrancar. Borra las carpetas de trabajo que dejó un
// corte y devuelve los archivos de la biblioteca sin pista en la base de datos y
// las carátulas sin álbum que las referencie.
// Esos no los borra: una cadena de conexión a una base vacía se llevaría la
// biblioteca entera.
func (im *Importer) Recover(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(im.library)
	if err != nil {
		return nil, fmt.Errorf("recuperar: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), workDirPrefix) {
			p := filepath.Join(im.library, e.Name())
			if err := os.RemoveAll(p); err != nil {
				return nil, fmt.Errorf("recuperar: borrar %s: %w", p, err)
			}
			im.log.Info("borrada una carpeta de trabajo que dejó un corte", "carpeta", p)
		}
	}

	var orphans []string
	byID := map[int64][]string{}
	for _, quality := range []string{db.QualityOriginal, db.QualityAAC256} {
		dir := im.versionDir(quality)
		files, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("recuperar: %w", err)
		}
		for _, f := range files {
			p := filepath.Join(dir, f.Name())
			idText, _, _ := strings.Cut(f.Name(), ".")
			id, err := strconv.ParseInt(idText, 10, 64)
			if err != nil || f.IsDir() {
				orphans = append(orphans, p) // no tiene forma de versión: no es nuestro
				continue
			}
			byID[id] = append(byID[id], p)
		}
	}
	ids := make([]int64, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	exists, err := db.ExistingTrackIDs(ctx, im.pool, ids)
	if err != nil {
		return nil, fmt.Errorf("recuperar: %w", err)
	}
	for id, paths := range byID {
		if !exists[id] {
			orphans = append(orphans, paths...)
		}
	}
	slices.Sort(orphans)
	for _, p := range orphans {
		im.log.Warn("archivo de la biblioteca sin pista en la base de datos; no se borra", "archivo", p)
	}

	covers, err := im.orphanCovers(ctx)
	if err != nil {
		return nil, fmt.Errorf("recuperar: %w", err)
	}
	orphans = append(orphans, covers...)
	slices.Sort(orphans)
	return orphans, nil
}

// orphanCovers avisa de las carátulas de covers/ que ningún álbum referencia y
// devuelve sus rutas. Las deja un corte entre colocar la carátula y confirmar la
// transacción (ADR-015). Tampoco se borran, por la misma razón que las versiones:
// con la base de datos equivocada, todas parecerían huérfanas.
func (im *Importer) orphanCovers(ctx context.Context) ([]string, error) {
	files, err := os.ReadDir(im.coverDir())
	if err != nil {
		return nil, fmt.Errorf("leer las carátulas: %w", err)
	}
	type cover struct {
		name, ext string
		albumID   int64
	}
	var orphans []string
	var named []cover
	for _, f := range files {
		idText, ext, _ := strings.Cut(f.Name(), ".")
		id, err := strconv.ParseInt(idText, 10, 64)
		// ParseInt acepta «+12»; la carátula del álbum 12 solo puede llamarse «12».
		if err != nil || f.IsDir() || idText != strconv.FormatInt(id, 10) {
			orphans = append(orphans, filepath.Join(im.coverDir(), f.Name()))
			im.log.Warn("archivo en covers/ que no tiene forma de carátula; no se borra",
				"archivo", path.Join(library.CoverDir, f.Name()))
			continue
		}
		named = append(named, cover{name: f.Name(), ext: ext, albumID: id})
	}
	ids := make([]int64, 0, len(named))
	for _, c := range named {
		ids = append(ids, c.albumID)
	}
	exts, err := db.AlbumCovers(ctx, im.pool, ids)
	if err != nil {
		return nil, fmt.Errorf("carátulas sin dueño: %w", err)
	}
	for _, c := range named {
		if ext, ok := exts[c.albumID]; ok && ext == c.ext {
			continue
		}
		orphans = append(orphans, filepath.Join(im.coverDir(), c.name))
		// Mientras siga ahí, las pistas nuevas del álbum con una carátula del mismo
		// formato no se importan: placeAll no pisa un destino que ya existe.
		im.log.Warn("carátula que ningún álbum referencia; no se borra, y hasta que se quite puede bloquear las pistas nuevas del álbum",
			"álbum", c.albumID, "archivo", path.Join(library.CoverDir, c.name))
	}
	return orphans, nil
}

func (im *Importer) versionDir(quality string) string {
	return filepath.Join(im.library, library.Dir(quality))
}

func (im *Importer) coverDir() string {
	return filepath.Join(im.library, library.CoverDir)
}

func (im *Importer) coverPath(albumID int64, ext string) string {
	return filepath.Join(im.library, filepath.FromSlash(library.CoverFile(albumID, ext)))
}

func (im *Importer) versionPath(quality string, id int64, ext string) string {
	return filepath.Join(im.library, filepath.FromSlash(library.File(quality, id, ext)))
}

// hashFile devuelve el SHA-256 y el tamaño de un archivo.
func hashFile(name string) (sum [32]byte, size int64, err error) {
	f, err := os.Open(name)
	if err != nil {
		return sum, 0, fmt.Errorf("abrir %s: %w", name, err)
	}
	defer f.Close() // solo lectura: cerrar no puede perder datos
	h := sha256.New()
	if size, err = io.Copy(h, f); err != nil {
		return sum, 0, fmt.Errorf("leer %s: %w", name, err)
	}
	copy(sum[:], h.Sum(nil))
	return sum, size, nil
}

// syncDir lleva a disco la entrada de un archivo recién renombrado, para que el
// renombrado sobreviva a un corte de luz.
func syncDir(dir string) error {
	// Windows no deja abrir una carpeta para sincronizarla; el servidor es Linux.
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("abrir %s: %w", dir, err)
	}
	if err := f.Sync(); err != nil {
		return errors.Join(fmt.Errorf("llevar %s a disco: %w", dir, err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("cerrar %s: %w", dir, err)
	}
	return nil
}
