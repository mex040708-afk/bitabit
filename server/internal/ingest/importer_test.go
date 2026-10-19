package ingest_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mex040708-afk/bitabit/server/internal/db"
	"github.com/mex040708-afk/bitabit/server/internal/db/dbtest"
	"github.com/mex040708-afk/bitabit/server/internal/ingest"
)

// Estos tests necesitan PostgreSQL (BITABIT_TEST_DATABASE_URL), ffmpeg y flac:
// se pasan en el servidor de pruebas.

type importEnv struct {
	im   *ingest.Importer
	pool *pgxpool.Pool
	dir  string // BITABIT_DATA_DIR
}

func newImportEnv(t *testing.T) importEnv {
	t.Helper()
	needEncoders(t)
	pool := dbtest.Pool(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	dir := t.TempDir()
	for _, sub := range []string{"entrada", "biblioteca", "fallidos"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	im, err := ingest.NewImporter(dir, pool, slog.New(slog.NewTextHandler(t.Output(), nil)))
	if err != nil {
		t.Fatalf("NewImporter: %v", err)
	}
	return importEnv{im: im, pool: pool, dir: dir}
}

// put genera un tono en entrada/rel y devuelve su contenido.
func (e importEnv) put(t *testing.T, rel, rate string, args ...string) []byte {
	t.Helper()
	full := filepath.Join(e.dir, "entrada", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	tone(t, filepath.Dir(full), filepath.Base(full), rate, args...)
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (e importEnv) putBytes(t *testing.T, rel string, data []byte) {
	t.Helper()
	full := filepath.Join(e.dir, "entrada", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o640); err != nil {
		t.Fatal(err)
	}
}

func (e importEnv) importOK(t *testing.T, rel string, want ingest.Outcome) ingest.Result {
	t.Helper()
	res, err := e.im.Import(context.Background(), rel)
	if err != nil {
		t.Fatalf("Import(%s): %v", rel, err)
	}
	if res.Outcome != want {
		t.Fatalf("Import(%s) = %+v, want %v", rel, res, want)
	}
	return res
}

func (e importEnv) path(parts ...string) string {
	return filepath.Join(append([]string{e.dir}, parts...)...)
}

func exists(t *testing.T, p string) bool {
	t.Helper()
	_, err := os.Lstat(p)
	if err == nil {
		return true
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return false
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func countRows(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type fileRow struct {
	ext, codec     string
	lossless       bool
	rate, channels int
	bits           *int16
	bytes          int64
	sha, md5       []byte
}

func trackFile(t *testing.T, pool *pgxpool.Pool, id int64, quality string) (fileRow, bool) {
	t.Helper()
	var r fileRow
	err := pool.QueryRow(context.Background(), `SELECT ext, codec, lossless, sample_rate, channels, bits, bytes, sha256, audio_md5
		FROM track_files WHERE track_id = $1 AND quality = $2`, id, quality).
		Scan(&r.ext, &r.codec, &r.lossless, &r.rate, &r.channels, &r.bits, &r.bytes, &r.sha, &r.md5)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return r, true
}

func checkStored(t *testing.T, r fileRow, file string) {
	t.Helper()
	data := readFile(t, file)
	sum := sha256.Sum256(data)
	if !bytes.Equal(r.sha, sum[:]) || r.bytes != int64(len(data)) {
		t.Errorf("%s: la fila no cuadra con el archivo (sha256 o tamaño)", filepath.Base(file))
	}
}

func TestImportLosslessFLAC(t *testing.T) {
	e := newImportEnv(t)
	src := e.put(t, "Álbum/01 Tono.flac", "96000", append([]string{"-ac", "2", "-c:a", "flac", "-sample_fmt", "s32"}, toneTags...)...)
	srcPCM := pcmMD5(t, e.path("entrada", "Álbum", "01 Tono.flac"), "pcm_s24le")

	res := e.importOK(t, "Álbum/01 Tono.flac", ingest.Imported)
	id := strconv.FormatInt(res.TrackID, 10)

	// La fuente y su carpeta, ya vacía, se van de la entrada; entrada/ se queda.
	if exists(t, e.path("entrada", "Álbum")) || !exists(t, e.path("entrada")) {
		t.Error("la entrada no quedó limpia")
	}
	orig, aac := e.path("biblioteca", "orig", id+".flac"), e.path("biblioteca", "aac", id+".m4a")
	if !exists(t, orig) || !exists(t, aac) {
		t.Fatalf("faltan las versiones en la biblioteca")
	}
	entries, err := os.ReadDir(e.path("biblioteca"))
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), ".tmp-") {
			t.Errorf("quedó la carpeta de trabajo %s", en.Name())
		}
	}

	var title, artist, album, albumArtist, date string
	var track, trackTotal, disc, discTotal *int16
	var durationMS int
	var source []byte
	if err := e.pool.QueryRow(context.Background(), `SELECT title, artist, album, album_artist, release_date,
		track_number, track_total, disc_number, disc_total, duration_ms, source_sha256 FROM tracks WHERE id = $1`, res.TrackID).
		Scan(&title, &artist, &album, &albumArtist, &date, &track, &trackTotal, &disc, &discTotal, &durationMS, &source); err != nil {
		t.Fatal(err)
	}
	srcSum := sha256.Sum256(src)
	if title != "Tono" || artist != "Artista" || album != "Álbum" || albumArtist != "Varios" || date != "1977" ||
		*track != 3 || *trackTotal != 12 || *disc != 1 || *discTotal != 2 || !bytes.Equal(source, srcSum[:]) {
		t.Errorf("pista: %q %q %q %q %q %d/%d %d/%d", title, artist, album, albumArtist, date, *track, *trackTotal, *disc, *discTotal)
	}
	if durationMS < 1950 || durationMS > 2050 {
		t.Errorf("duration_ms = %d, want unos 2000", durationMS)
	}

	o, ok := trackFile(t, e.pool, res.TrackID, "orig")
	if !ok || o.ext != "flac" || o.codec != "flac" || !o.lossless || o.rate != 96000 || o.channels != 2 || *o.bits != 24 {
		t.Errorf("versión orig: %+v", o)
	}
	if hexMD5 := output(t, "metaflac", "--show-md5sum", orig); len(o.md5) != 16 || hexMD5 != hex.EncodeToString(o.md5) {
		t.Errorf("audio_md5 = %x, STREAMINFO dice %s", o.md5, hexMD5)
	}
	checkStored(t, o, orig)
	if got := pcmMD5(t, orig, "pcm_s24le"); got != srcPCM {
		t.Errorf("el audio guardado no es el de la fuente")
	}

	a, ok := trackFile(t, e.pool, res.TrackID, "aac256")
	if !ok || a.ext != "m4a" || a.codec != "aac" || a.lossless || a.rate != 48000 || a.channels != 2 || a.bits != nil || a.md5 != nil {
		t.Errorf("versión aac256: %+v", a)
	}
	checkStored(t, a, aac)
}

func TestImportWAVBecomesFLAC(t *testing.T) {
	e := newImportEnv(t)
	e.put(t, "a.wav", "44100", "-c:a", "pcm_s16le")
	want := pcmMD5(t, e.path("entrada", "a.wav"), "pcm_s16le")
	res := e.importOK(t, "a.wav", ingest.Imported)

	orig := e.path("biblioteca", "orig", strconv.FormatInt(res.TrackID, 10)+".flac")
	o, ok := trackFile(t, e.pool, res.TrackID, "orig")
	if !ok || o.codec != "flac" || *o.bits != 16 || len(o.md5) != 16 {
		t.Errorf("versión orig: %+v", o)
	}
	checkStored(t, o, orig)
	if got := pcmMD5(t, orig, "pcm_s16le"); got != want || got != hex.EncodeToString(o.md5) {
		t.Errorf("el FLAC no es el audio del WAV: %s, fuente %s, fila %x", got, want, o.md5)
	}
	// Sin etiquetas de título, se usa el nombre del archivo.
	var title string
	if err := e.pool.QueryRow(context.Background(), "SELECT title FROM tracks WHERE id = $1", res.TrackID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "a" {
		t.Errorf("title = %q, want a", title)
	}
}

func TestImportLossyIsStoredAsIs(t *testing.T) {
	e := newImportEnv(t)
	src := e.put(t, "Disco/canción.mp3", "44100", "-c:a", "libmp3lame", "-b:a", "320k", "-metadata", "title=Con pérdida")
	res := e.importOK(t, "Disco/canción.mp3", ingest.Imported)

	id := strconv.FormatInt(res.TrackID, 10)
	orig := e.path("biblioteca", "orig", id+".mp3")
	if !bytes.Equal(readFile(t, orig), src) {
		t.Error("el MP3 guardado no es idéntico a la fuente")
	}
	o, ok := trackFile(t, e.pool, res.TrackID, "orig")
	if !ok || o.codec != "mp3" || o.lossless || o.bits != nil || o.md5 != nil {
		t.Errorf("versión orig: %+v", o)
	}
	if _, ok := trackFile(t, e.pool, res.TrackID, "aac256"); ok || exists(t, e.path("biblioteca", "aac", id+".m4a")) {
		t.Error("una fuente con pérdida no lleva AAC")
	}
}

func TestImportDuplicateGoesToFailed(t *testing.T) {
	e := newImportEnv(t)
	src := e.put(t, "a.flac", "44100", "-c:a", "flac")
	first := e.importOK(t, "a.flac", ingest.Imported)

	e.putBytes(t, "Otra carpeta/copia.flac", src)
	res := e.importOK(t, "Otra carpeta/copia.flac", ingest.Duplicate)
	if res.TrackID != first.TrackID {
		t.Errorf("repetido de la pista %d, want %d", res.TrackID, first.TrackID)
	}
	moved := e.path("fallidos", "Otra carpeta", "copia.flac")
	if !bytes.Equal(readFile(t, moved), src) {
		t.Error("el repetido no está intacto en fallidos/")
	}
	note := string(readFile(t, moved+".error.txt"))
	if !strings.Contains(note, "ya importado como pista "+strconv.FormatInt(first.TrackID, 10)) ||
		!strings.Contains(note, "Otra carpeta/copia.flac") {
		t.Errorf("nota: %q", note)
	}
	if exists(t, e.path("entrada", "Otra carpeta")) {
		t.Error("la carpeta vacía sigue en la entrada")
	}
	if n := countRows(t, e.pool, "tracks"); n != 1 {
		t.Errorf("hay %d pistas, want 1", n)
	}
}

func TestImportUnreadableGoesToFailedWithoutOverwriting(t *testing.T) {
	e := newImportEnv(t)
	for i, content := range []string{"primero", "segundo"} {
		// La carpeta guarda algo que no es audio: no se borra.
		e.putBytes(t, "Malos/portada.jpg", []byte("jpg"))
		e.putBytes(t, "Malos/texto.flac", []byte(content))
		res := e.importOK(t, "Malos/texto.flac", ingest.Failed)
		if !strings.Contains(res.Reason, "frecuencia") {
			t.Errorf("motivo %d: %q", i, res.Reason)
		}
	}
	if string(readFile(t, e.path("fallidos", "Malos", "texto.flac"))) != "primero" ||
		string(readFile(t, e.path("fallidos", "Malos", "texto (2).flac"))) != "segundo" {
		t.Error("el segundo fallo pisó al primero")
	}
	for _, note := range []string{"texto.flac.error.txt", "texto (2).flac.error.txt"} {
		if !strings.Contains(string(readFile(t, e.path("fallidos", "Malos", note))), "Motivo: ") {
			t.Errorf("falta la nota %s", note)
		}
	}
	if !exists(t, e.path("entrada", "Malos", "portada.jpg")) {
		t.Error("se borró lo que no es audio")
	}
	if n := countRows(t, e.pool, "tracks"); n != 0 {
		t.Errorf("hay %d pistas, want 0", n)
	}
}

// Un fallo del sistema no toca la fuente: se reintenta en la siguiente pasada.
func TestImportKeepsSourceOnSystemError(t *testing.T) {
	e := newImportEnv(t)
	e.put(t, "a.flac", "44100", "-c:a", "flac")
	e.pool.Close() // la base de datos deja de estar disponible

	if _, err := e.im.Import(context.Background(), "a.flac"); err == nil {
		t.Fatal("Import no falla sin base de datos")
	}
	if !exists(t, e.path("entrada", "a.flac")) {
		t.Error("la fuente desapareció de la entrada")
	}
	if entries, err := os.ReadDir(e.path("fallidos")); err != nil || len(entries) != 0 {
		t.Errorf("fallidos/ no está vacía: %v %v", entries, err)
	}
}

// Si ya existe un archivo con el ID que toca, la base de datos no es la de esta
// biblioteca: no se pisa nada y no queda la pista a medias.
func TestImportRefusesToOverwriteLibrary(t *testing.T) {
	e := newImportEnv(t)
	e.put(t, "a.flac", "44100", "-c:a", "flac")
	// En un esquema nuevo, la primera pista será la 1.
	occupied := e.path("biblioteca", "orig", "1.flac")
	if err := os.WriteFile(occupied, []byte("de otra base de datos"), 0o640); err != nil {
		t.Fatal(err)
	}

	if _, err := e.im.Import(context.Background(), "a.flac"); err == nil {
		t.Fatal("Import pisó un archivo de la biblioteca")
	}
	if string(readFile(t, occupied)) != "de otra base de datos" {
		t.Error("el archivo de la biblioteca cambió")
	}
	if !exists(t, e.path("entrada", "a.flac")) {
		t.Error("la fuente desapareció de la entrada")
	}
	if n := countRows(t, e.pool, "tracks"); n != 0 {
		t.Errorf("quedó una pista a medias: %d", n)
	}
	if exists(t, e.path("biblioteca", "aac", "1.m4a")) {
		t.Error("quedó el AAC colocado")
	}
}

func TestImportRejectsPathsOutsideInbox(t *testing.T) {
	e := newImportEnv(t)
	for _, rel := range []string{"../fuera.flac", "/etc/passwd", ""} {
		if _, err := e.im.Import(context.Background(), rel); err == nil {
			t.Errorf("Import(%q) no falla", rel)
		}
	}
}

func TestRecover(t *testing.T) {
	e := newImportEnv(t)
	e.put(t, "a.flac", "44100", "-c:a", "flac")
	res := e.importOK(t, "a.flac", ingest.Imported)

	work := e.path("biblioteca", ".tmp-corte")
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "original.flac"), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	orphans := []string{e.path("biblioteca", "aac", "999.m4a"), e.path("biblioteca", "orig", "nota.txt")}
	for _, p := range orphans {
		if err := os.WriteFile(p, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	got, err := e.im.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	slices.Sort(orphans)
	if !slices.Equal(got, orphans) {
		t.Errorf("huérfanos %v, want %v", got, orphans)
	}
	if exists(t, work) {
		t.Error("la carpeta de trabajo sigue ahí")
	}
	for _, p := range append(orphans, e.path("biblioteca", "orig", strconv.FormatInt(res.TrackID, 10)+".flac")) {
		if !exists(t, p) {
			t.Errorf("Recover borró %s", p)
		}
	}
}

func TestNewImporterNeedsTheThreeFolders(t *testing.T) {
	dir := t.TempDir()
	if _, err := ingest.NewImporter(dir, nil, slog.New(slog.NewTextHandler(t.Output(), nil))); err == nil {
		t.Error("NewImporter acepta una carpeta sin entrada/, biblioteca/ ni fallidos/")
	}
	if _, err := ingest.NewImporter("datos", nil, slog.New(slog.NewTextHandler(t.Output(), nil))); err == nil {
		t.Error("NewImporter acepta una ruta relativa")
	}
}

// De punta a punta: Run vigila la entrada e importa lo que llega.
func TestRunImportsWhatArrives(t *testing.T) {
	e := newImportEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.im.Run(ctx, 20*time.Millisecond, 0) }()

	e.put(t, "Llega/a.flac", "44100", "-c:a", "flac")
	deadline := time.Now().Add(30 * time.Second)
	for countRows(t, e.pool, "tracks") == 0 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("Run no importó el archivo en 30 s")
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Run terminó con %v, want context.Canceled", err)
	}
	if exists(t, e.path("entrada", "Llega")) {
		t.Error("la entrada no quedó limpia")
	}
}
