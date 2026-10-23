package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
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

	// OrphanCoverGrace es lo que tiene que llevar una carátula sin dueño en covers/
	// para que Recover la borre. Es mucho más de lo que dura cualquier importación
	// (ADR-015): una más joven puede ser de una importación que aún no ha confirmado.
	OrphanCoverGrace = 24 * time.Hour
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
	dataDir string
	data    *os.Root // la carpeta de datos: de aquí sale todo acceso a entrada/ (ADR-032)
	pool    *pgxpool.Pool
	log     *slog.Logger
	now     func() time.Time // los tests lo mueven para no esperar 24 horas
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
		dataDir: dataDir,
		inbox:   filepath.Join(dataDir, "entrada"),
		library: filepath.Join(dataDir, "biblioteca"),
		failed:  filepath.Join(dataDir, "fallidos"),
		pool:    pool,
		log:     log,
		now:     time.Now,
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
	data, err := os.OpenRoot(dataDir)
	if err != nil {
		return nil, fmt.Errorf("importación: %w", err)
	}
	im.data = data
	for _, dir := range []string{im.versionDir(db.QualityOriginal), im.versionDir(db.QualityAAC256), im.coverDir()} {
		if err := im.mkdirAll(dir); err != nil {
			return nil, fmt.Errorf("importación: crear %s: %w", dir, err)
		}
	}
	return im, nil
}

// Close suelta la carpeta de datos abierta. Solo lee de ella al cerrar, así que
// no puede perder datos.
func (im *Importer) Close() error { return im.data.Close() }

// Run importa lo que esté listo en la entrada cada interval, hasta que se
// cancele ctx. Los fallos del sistema se registran y el archivo se reintenta en
// la siguiente pasada.
func (im *Importer) Run(ctx context.Context, interval, quiet time.Duration) error {
	inboxRoot, err := im.data.OpenRoot("entrada")
	if err != nil {
		return fmt.Errorf("abrir la entrada: %w", err)
	}
	defer inboxRoot.Close() // solo lectura: cerrar no puede perder datos
	inbox := NewInboxFS(inboxRoot.FS(), quiet)
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

	work, err := os.MkdirTemp(im.library, workDirPrefix)
	if err != nil {
		return Result{}, fmt.Errorf("importar %s: crear la carpeta de trabajo: %w", rel, err)
	}
	defer func() {
		if rerr := os.RemoveAll(work); rerr != nil {
			err = errors.Join(err, fmt.Errorf("borrar la carpeta de trabajo: %w", rerr))
		}
	}()

	// La fuente se abre una sola vez, con el Root, y se copia a la carpeta de
	// trabajo calculando su hash: la sonda, las versiones y la carátula leen esos
	// mismos bytes, y ningún proceso externo recibe un nombre de entrada/ que
	// alguien pueda cambiar por un enlace simbólico (ADR-032).
	copied, sum, err := im.copySource(rel, work)
	var unsafe *unsafeSourceError
	if errors.As(err, &unsafe) {
		return im.fail(rel, src, Failed, 0, unsafe.Error())
	}
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

	info, err := Probe(ctx, copied)
	if errors.Is(err, ErrUnreadable) {
		return im.fail(rel, src, Failed, 0, err.Error())
	}
	if err != nil {
		return Result{}, fmt.Errorf("importar %s: %w", rel, err)
	}

	v, err := BuildVersions(ctx, copied, info, work)
	if errors.Is(err, ErrUnreadable) {
		return im.fail(rel, src, Failed, 0, err.Error())
	}
	if err != nil {
		return Result{}, fmt.Errorf("importar %s: %w", rel, err)
	}

	cover, err := im.stageCover(ctx, rel, copied, work)
	if err != nil {
		return Result{}, fmt.Errorf("importar %s: %w", rel, err)
	}

	id, err = im.store(ctx, rel, sum, info, v, cover)
	if err != nil {
		return Result{}, fmt.Errorf("importar %s: %w", rel, err)
	}
	// La pista ya está guardada. Si no se puede borrar la fuente, en la próxima
	// pasada saldrá como repetida y acabará en fallidos/.
	if err := im.remove(src); err != nil {
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
			if rerr := im.remove(p); rerr != nil {
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
	return im.placeAll(moves)
}

// placeAll mueve cada origen a su destino y devuelve los destinos que llegó a
// colocar. Un destino que ya existe no se pisa: o la base de datos no es la de
// esta biblioteca, o un corte dejó una carátula sin dueño (ADR-015). En los dos
// casos, mejor parar.
func (im *Importer) placeAll(moves [][2]string) ([]string, error) {
	var placed []string
	for _, m := range moves {
		src, dst := m[0], m[1]
		if _, err := im.lstat(dst); err == nil {
			return placed, fmt.Errorf("ya existe %s: ¿la base de datos no corresponde a esta biblioteca?", dst)
		} else if !errors.Is(err, os.ErrNotExist) {
			return placed, fmt.Errorf("comprobar %s: %w", dst, err)
		}
		if err := im.rename(src, dst); err != nil {
			return placed, fmt.Errorf("colocar %s: %w", dst, err)
		}
		placed = append(placed, dst)
		if err := im.syncDir(filepath.Dir(dst)); err != nil {
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
	if err := im.mkdirAll(filepath.Dir(dst)); err != nil {
		return Result{}, fmt.Errorf("mover %s a fallidos/: %w", rel, err)
	}
	if err := im.rename(src, dst); err != nil {
		return Result{}, fmt.Errorf("mover %s a fallidos/: %w", rel, err)
	}
	note := fmt.Sprintf("Archivo: %s\nFecha: %s\nMotivo: %s\n", rel, time.Now().Format(time.RFC3339), reason)
	// El archivo ya está en fallidos/: sin la nota se ve igual, así que basta
	// con registrarlo.
	if err := im.writeFile(dst+errorNoteExt, []byte(note)); err != nil {
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
		free, err := im.notExists(candidate, candidate+errorNoteExt)
		if err != nil {
			return "", err
		}
		if free {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("hay más de %d archivos llamados %s", maxFailedTries, rel)
}

func (im *Importer) notExists(paths ...string) (bool, error) {
	for _, p := range paths {
		_, err := im.lstat(p)
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
		entries, err := im.readDir(dir)
		if err != nil {
			im.log.Error("no se pudo mirar una carpeta de la entrada", "carpeta", dir, "error", err)
			return
		}
		if len(entries) > 0 {
			return
		}
		if err := im.remove(dir); err != nil {
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
	entries, err := im.readDir(im.library)
	if err != nil {
		return nil, fmt.Errorf("recuperar: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), workDirPrefix) {
			p := filepath.Join(im.library, e.Name())
			if err := im.removeAll(p); err != nil {
				return nil, fmt.Errorf("recuperar: borrar %s: %w", p, err)
			}
			im.log.Info("borrada una carpeta de trabajo que dejó un corte", "carpeta", p)
		}
	}

	var orphans []string
	byID := map[int64][]string{}
	for _, quality := range []string{db.QualityOriginal, db.QualityAAC256} {
		dir := im.versionDir(quality)
		files, err := im.readDir(dir)
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
	files, err := im.readDir(im.coverDir())
	if err != nil {
		return nil, fmt.Errorf("leer las carátulas: %w", err)
	}
	type cover struct {
		name, ext string
		albumID   int64
		thumb     bool // miniatura: sobra si el álbum no tiene carátula
	}
	var orphans []string
	var named []cover
	for _, f := range files {
		idText, ext, _ := strings.Cut(f.Name(), ".")
		thumb := false
		if t, ok := strings.CutSuffix(idText, "-"+strconv.Itoa(library.ThumbSize)); ok && ext == "jpg" {
			idText, thumb = t, true
		}
		id, err := strconv.ParseInt(idText, 10, 64)
		// ParseInt acepta «+12»; la carátula del álbum 12 solo puede llamarse «12».
		// Y solo se borra lo que el importador pudo escribir: ID positivo y jpg o png
		// («12.txt», «12.jpg.bak» o «-5.jpg» no son suyos).
		if err != nil || f.IsDir() || idText != strconv.FormatInt(id, 10) || id <= 0 || (ext != "jpg" && ext != "png") {
			orphans = append(orphans, filepath.Join(im.coverDir(), f.Name()))
			im.log.Warn("archivo en covers/ que no tiene forma de carátula; no se borra",
				"archivo", path.Join(library.CoverDir, f.Name()))
			continue
		}
		named = append(named, cover{name: f.Name(), ext: ext, albumID: id, thumb: thumb})
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
		if ext, ok := exts[c.albumID]; ok && (c.thumb || ext == c.ext) {
			continue
		}
		file := filepath.Join(im.coverDir(), c.name)
		rel := path.Join(library.CoverDir, c.name)
		removed, err := im.removeOrphanCover(ctx, c.albumID, c.name, c.thumb, c.ext)
		if err != nil {
			return nil, err
		}
		if removed {
			im.log.Warn("carátula sin dueño borrada: llevaba más de un día sin álbum que la referencie y se puede regenerar del original",
				"álbum", c.albumID, "archivo", rel)
			continue
		}
		orphans = append(orphans, file)
		// Mientras siga ahí, las pistas nuevas del álbum con una carátula del mismo
		// formato no se importan: placeAll no pisa un destino que ya existe.
		im.log.Warn("carátula que ningún álbum referencia; se borrará pasado el plazo de gracia, y hasta entonces puede bloquear las pistas nuevas del álbum",
			"álbum", c.albumID, "archivo", rel)
	}
	return orphans, nil
}

func (im *Importer) versionDir(quality string) string {
	return filepath.Join(im.library, library.Dir(quality))
}

// removeOrphanCover borra la carátula name del álbum albumID si lleva más de
// OrphanCoverGrace sin que nada la referencie. Antes de borrar vuelve a mirar la base
// de datos: una importación que acaba de confirmarse la habría hecho suya después
// del primer vistazo. Solo borra archivos con forma de carátula; lo demás de covers/
// se avisa y se queda. Devuelve si la borró.
func (im *Importer) removeOrphanCover(ctx context.Context, albumID int64, name string, thumb bool, ext string) (bool, error) {
	file := filepath.Join(im.coverDir(), name)
	info, err := im.lstat(file)
	if err != nil {
		// Una carátula que no se puede ni mirar no debe impedir que el servidor arranque.
		im.log.Warn("no se pudo mirar una carátula sin dueño; se deja", "archivo", name, "error", err)
		return false, nil
	}
	if !info.Mode().IsRegular() || im.now().Sub(info.ModTime()) < OrphanCoverGrace {
		return false, nil
	}
	exts, err := db.AlbumCovers(ctx, im.pool, []int64{albumID})
	if err != nil {
		return false, fmt.Errorf("comprobar la carátula %s antes de borrarla: %w", name, err)
	}
	if got, ok := exts[albumID]; ok && (thumb || got == ext) {
		return false, nil
	}
	if err := im.remove(file); err != nil {
		// Si no se puede borrar (permisos, disco de solo lectura), el servidor arranca
		// igual: la carátula sigue en la lista de huérfanos y se avisa.
		im.log.Warn("no se pudo borrar una carátula sin dueño; se deja", "archivo", name, "error", err)
		return false, nil
	}
	return true, nil
}

func (im *Importer) coverDir() string {
	return filepath.Join(im.library, library.CoverDir)
}

func (im *Importer) thumbPath(albumID int64) string {
	return filepath.Join(im.library, filepath.FromSlash(library.ThumbFile(albumID)))
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
func (im *Importer) syncDir(dir string) error {
	// Windows no deja abrir una carpeta para sincronizarla; el servidor es Linux.
	if runtime.GOOS == "windows" {
		return nil
	}
	rel, err := im.rel(dir)
	if err != nil {
		return err
	}
	f, err := im.data.Open(rel)
	if err != nil {
		return fmt.Errorf("abrir %s para sincronizarla: %w", dir, err)
	}
	if err := f.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sincronizar %s: %w", dir, err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("cerrar %s: %w", dir, err)
	}
	return nil
}

// unsafeSourceError es una fuente que no se puede abrir de forma segura: un enlace
// simbólico que sale de la carpeta de datos, o algo que no es un archivo regular.
type unsafeSourceError struct{ reason string }

func (e *unsafeSourceError) Error() string { return e.reason }

// copySource abre rel dentro de entrada/ con el Root y la copia a work, y devuelve
// la ruta de la copia y el SHA-256 de lo copiado. Un archivo que ya no está es un
// error de sistema (se reintenta); uno que no se puede abrir con seguridad es un
// unsafeSourceError.
func (im *Importer) copySource(rel, work string) (copied string, sum [32]byte, err error) {
	// O_NONBLOCK: si entre el listado y la apertura alguien cambia el archivo por una
	// tubería con nombre, abrirla sin él se quedaría esperando a un escritor para
	// siempre y pararía la importación. Con él se abre y el Stat la rechaza.
	f, err := im.data.OpenFile(path.Join("entrada", rel), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil && isUnsafePath(err) {
		return "", sum, &unsafeSourceError{reason: "no se puede abrir de forma segura: " + err.Error()}
	}
	if err != nil {
		// Un archivo que ya no está, sin permisos o demasiados abiertos son cosas del
		// sistema que pueden pasar: se reintenta, no se manda a fallidos/.
		return "", sum, fmt.Errorf("abrir %s: %w", rel, err)
	}
	defer f.Close() // solo lectura: cerrar no puede perder datos
	// Se mira el archivo ya abierto, no su nombre: entre medias el nombre pudo cambiar.
	info, err := f.Stat()
	if err != nil {
		return "", sum, fmt.Errorf("mirar %s: %w", rel, err)
	}
	if !info.Mode().IsRegular() {
		return "", sum, &unsafeSourceError{reason: "no es un archivo regular"}
	}
	copied = filepath.Join(work, "copia-de-la-fuente"+strings.ToLower(path.Ext(rel)))
	out, err := os.OpenFile(copied, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return "", sum, fmt.Errorf("crear la copia de trabajo: %w", err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), f)
	if err != nil {
		return "", sum, errors.Join(fmt.Errorf("copiar %s: %w", rel, err), out.Close())
	}
	if err := out.Close(); err != nil {
		return "", sum, fmt.Errorf("cerrar la copia de trabajo: %w", err)
	}
	// Si alguien seguía escribiendo el archivo mientras se copiaba, la copia es una
	// foto a medias: se descarta y se reintenta cuando la vigilancia lo vea quieto.
	after, err := f.Stat()
	if err != nil {
		return "", sum, fmt.Errorf("mirar %s tras copiarlo: %w", rel, err)
	}
	if err := checkUnchanged(info, after, n); err != nil {
		return "", sum, fmt.Errorf("copiar %s: %w", rel, err)
	}
	copy(sum[:], h.Sum(nil))
	return copied, sum, nil
}

// checkUnchanged dice si el archivo era el mismo antes y después de copiarlo: mismo
// tamaño, misma fecha de modificación y tantos bytes copiados como medía.
func checkUnchanged(before, after fs.FileInfo, copied int64) error {
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || copied != after.Size() {
		return errors.New("el archivo cambió mientras se copiaba")
	}
	return nil
}

// isUnsafePath dice si err de abrir un archivo con el Root es que el camino sale de
// la carpeta de datos (un enlace simbólico) o es un bucle de enlaces, y no un fallo
// del sistema. os no exporta ese error, así que se reconoce por su texto; un test lo
// vigila.
func isUnsafePath(err error) bool {
	return errors.Is(err, syscall.ELOOP) || strings.Contains(err.Error(), "path escapes from parent")
}

// Los accesos a la carpeta de datos van por im.data (un os.Root): una ruta que
// salga de ella, o un enlace simbólico que apunte fuera, da error (ADR-032). Las
// rutas llegan absolutas, como las construye el importador, y aquí se hacen
// relativas al Root. La carpeta de trabajo, creada por el propio importador, es lo
// único que se toca por ruta absoluta, porque ahí escriben ffmpeg y flac.

// rel da la ruta de abs relativa a la carpeta de datos.
func (im *Importer) rel(abs string) (string, error) {
	r, err := filepath.Rel(im.dataDir, abs)
	if err != nil || !filepath.IsLocal(r) {
		return "", fmt.Errorf("%s está fuera de la carpeta de datos", abs)
	}
	return r, nil
}

func (im *Importer) lstat(abs string) (fs.FileInfo, error) {
	r, err := im.rel(abs)
	if err != nil {
		return nil, err
	}
	return im.data.Lstat(r)
}

func (im *Importer) rename(oldAbs, newAbs string) error {
	o, err := im.rel(oldAbs)
	if err != nil {
		return err
	}
	n, err := im.rel(newAbs)
	if err != nil {
		return err
	}
	return im.data.Rename(o, n)
}

func (im *Importer) remove(abs string) error {
	r, err := im.rel(abs)
	if err != nil {
		return err
	}
	return im.data.Remove(r)
}

func (im *Importer) removeAll(abs string) error {
	r, err := im.rel(abs)
	if err != nil {
		return err
	}
	return im.data.RemoveAll(r)
}

func (im *Importer) mkdirAll(abs string) error {
	r, err := im.rel(abs)
	if err != nil {
		return err
	}
	return im.data.MkdirAll(r, dirPerm)
}

func (im *Importer) writeFile(abs string, data []byte) error {
	r, err := im.rel(abs)
	if err != nil {
		return err
	}
	f, err := im.data.OpenFile(r, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}

// readDir lista abs ordenada por nombre, como os.ReadDir.
func (im *Importer) readDir(abs string) ([]fs.DirEntry, error) {
	r, err := im.rel(abs)
	if err != nil {
		return nil, err
	}
	f, err := im.data.Open(r)
	if err != nil {
		return nil, err
	}
	entries, err := f.ReadDir(-1)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}
