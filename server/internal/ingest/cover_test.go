package ingest_test

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mex040708-afk/bitabit/server/internal/ingest"
)

// picture genera una imagen de 64×64 en dir/name: JPEG o PNG según la extensión.
// Devuelve su ruta y sus bytes.
func picture(t *testing.T, dir, name string) (string, []byte) {
	t.Helper()
	p := ffmpeg(t, dir, name, "-f", "lavfi", "-i", "color=c=red:s=64x64", "-frames:v", "1")
	return p, readFile(t, p)
}

// embedArgs son los argumentos de tone para que el FLAC lleve pic como carátula.
func embedArgs(pic string) []string {
	return []string{"-i", pic, "-map", "0:a", "-map", "1:v", "-c:a", "flac",
		"-c:v", "copy", "-disposition:v:0", "attached_pic"}
}

// albumIDOf devuelve el álbum de una pista.
func albumIDOf(t *testing.T, pool *pgxpool.Pool, trackID int64) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), "SELECT album_id FROM tracks WHERE id = $1", trackID).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// coverExtOf devuelve la extensión de la carátula de un álbum, o nil si no tiene.
func coverExtOf(t *testing.T, pool *pgxpool.Pool, albumID int64) *string {
	t.Helper()
	var ext *string
	err := pool.QueryRow(context.Background(), "SELECT cover_ext FROM albums WHERE id = $1", albumID).Scan(&ext)
	if err != nil {
		t.Fatal(err)
	}
	return ext
}

func TestImportTakesTheEmbeddedCover(t *testing.T) {
	e := newImportEnv(t)
	pic, want := picture(t, t.TempDir(), "cover.jpg")
	e.put(t, "disco/01.flac", "44100", embedArgs(pic)...)
	res := e.importOK(t, "disco/01.flac", ingest.Imported)

	album := albumIDOf(t, e.pool, res.TrackID)
	if ext := coverExtOf(t, e.pool, album); ext == nil || *ext != "jpg" {
		t.Fatalf("cover_ext = %v, want jpg", ext)
	}
	got := readFile(t, e.path("biblioteca", "covers", fmt.Sprintf("%d.jpg", album)))
	if !bytes.Equal(got, want) {
		t.Error("la carátula guardada no es la incrustada en el archivo")
	}
}

func TestImportTakesAPNGCover(t *testing.T) {
	e := newImportEnv(t)
	pic, want := picture(t, t.TempDir(), "cover.png")
	e.put(t, "disco/01.flac", "44100", embedArgs(pic)...)
	res := e.importOK(t, "disco/01.flac", ingest.Imported)

	album := albumIDOf(t, e.pool, res.TrackID)
	if ext := coverExtOf(t, e.pool, album); ext == nil || *ext != "png" {
		t.Fatalf("cover_ext = %v, want png", ext)
	}
	got := readFile(t, e.path("biblioteca", "covers", fmt.Sprintf("%d.png", album)))
	if !bytes.Equal(got, want) {
		t.Error("la carátula guardada no es la incrustada en el archivo")
	}
}

// La primera pista del álbum con imagen fija la carátula; una segunda imagen no la cambia.
func TestImportFirstCoverWinsForTheAlbum(t *testing.T) {
	e := newImportEnv(t)
	dir := t.TempDir()
	jpgPath, jpg := picture(t, dir, "a.jpg")
	pngPath, _ := picture(t, dir, "b.png")
	e.put(t, "disco/01.flac", "44100", embedArgs(jpgPath)...)
	e.put(t, "disco/02.flac", "44100", embedArgs(pngPath)...)
	first := e.importOK(t, "disco/01.flac", ingest.Imported)
	second := e.importOK(t, "disco/02.flac", ingest.Imported)

	album := albumIDOf(t, e.pool, first.TrackID)
	if other := albumIDOf(t, e.pool, second.TrackID); other != album {
		t.Fatalf("las pistas están en los álbumes %d y %d, want el mismo", album, other)
	}
	if ext := coverExtOf(t, e.pool, album); ext == nil || *ext != "jpg" {
		t.Fatalf("cover_ext = %v, want jpg", ext)
	}
	if !bytes.Equal(readFile(t, e.path("biblioteca", "covers", fmt.Sprintf("%d.jpg", album))), jpg) {
		t.Error("la carátula del álbum no es la de la primera pista")
	}
	if exists(t, e.path("biblioteca", "covers", fmt.Sprintf("%d.png", album))) {
		t.Error("se colocó la carátula de la segunda pista")
	}
}

// Sin imagen incrustada no hay carátula, y covers/ queda vacía.
func TestImportWithoutPictureLeavesNoCover(t *testing.T) {
	e := newImportEnv(t)
	e.put(t, "sin/01.flac", "44100", "-c:a", "flac")
	res := e.importOK(t, "sin/01.flac", ingest.Imported)

	if ext := coverExtOf(t, e.pool, albumIDOf(t, e.pool, res.TrackID)); ext != nil {
		t.Errorf("cover_ext = %q, want NULL", *ext)
	}
	entries, err := os.ReadDir(e.path("biblioteca", "covers"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("covers/ tiene %d archivos, want ninguno", len(entries))
	}
}

// Si covers/ ya tiene un archivo que la base de datos no referencia, la importación
// para sin pisarlo, y la pista con sus versiones no queda a medias.
func TestImportKeepsAStrayCover(t *testing.T) {
	e := newImportEnv(t)
	e.put(t, "a/01.flac", "44100", "-c:a", "flac")
	first := e.importOK(t, "a/01.flac", ingest.Imported)
	album := albumIDOf(t, e.pool, first.TrackID)
	stray := e.path("biblioteca", "covers", fmt.Sprintf("%d.jpg", album))
	if err := os.WriteFile(stray, []byte("resto de un corte"), 0o640); err != nil {
		t.Fatal(err)
	}
	pic, _ := picture(t, t.TempDir(), "cover.jpg")
	e.put(t, "a/02.flac", "44100", embedArgs(pic)...)

	if _, err := e.im.Import(context.Background(), "a/02.flac"); err == nil {
		t.Fatal("Import aceptó una carátula que ya existía")
	}
	if got := readFile(t, stray); string(got) != "resto de un corte" {
		t.Error("el archivo que ya estaba en covers/ cambió")
	}
	if n := countRows(t, e.pool, "tracks"); n != 1 {
		t.Errorf("hay %d pistas, want 1", n)
	}
	orig, err := os.ReadDir(e.path("biblioteca", "orig"))
	if err != nil {
		t.Fatal(err)
	}
	if len(orig) != 1 {
		t.Errorf("orig/ tiene %d archivos, want 1: las versiones de la pista fallida se retiran", len(orig))
	}
	if !exists(t, e.path("entrada", "a", "02.flac")) {
		t.Error("la fuente se borró de la entrada")
	}
}

// La carátula grande se importa con su miniatura de 256 px en JPEG; una carátula que
// ya es pequeña no la necesita (ADR-017).
func TestImportMakesAThumbnailOnlyForBigCovers(t *testing.T) {
	e := newImportEnv(t)
	dir := t.TempDir()
	big := ffmpeg(t, dir, "big.png", "-f", "lavfi", "-i", "testsrc=s=1024x512", "-frames:v", "1")
	e.put(t, "grande/01.flac", "44100", embedArgs(big)...)
	bigAlbum := albumIDOf(t, e.pool, e.importOK(t, "grande/01.flac", ingest.Imported).TrackID)

	thumb := readFile(t, e.path("biblioteca", "covers", fmt.Sprintf("%d-256.jpg", bigAlbum)))
	if !bytes.HasPrefix(thumb, []byte{0xFF, 0xD8, 0xFF}) {
		t.Fatal("la miniatura no es JPEG")
	}
	out := ffmpeg(t, dir, "thumb.png", "-i", e.path("biblioteca", "covers", fmt.Sprintf("%d-256.jpg", bigAlbum)), "-frames:v", "1")
	if w, h := pngSize(t, out); w != 256 || h != 128 {
		t.Errorf("la miniatura mide %dx%d, want 256x128", w, h)
	}
	if got := readFile(t, e.path("biblioteca", "covers", fmt.Sprintf("%d.png", bigAlbum))); len(got) <= len(thumb) {
		t.Error("la carátula original quedó más pequeña que la miniatura")
	}

	small, _ := picture(t, dir, "small.jpg") // 64x64
	e.put(t, "pequeno/01.flac", "44100", append(embedArgs(small), "-metadata", "album=Pequeño")...)
	smallAlbum := albumIDOf(t, e.pool, e.importOK(t, "pequeno/01.flac", ingest.Imported).TrackID)
	if exists(t, e.path("biblioteca", "covers", fmt.Sprintf("%d-256.jpg", smallAlbum))) {
		t.Error("una carátula de 64 px no necesita miniatura")
	}
	if orphans, err := e.im.Recover(context.Background()); err != nil || len(orphans) != 0 {
		t.Errorf("Recover = %v, %v; la miniatura de un álbum con carátula no es huérfana", orphans, err)
	}
	// Ni siquiera pasado el plazo de gracia: la miniatura es .jpg aunque la carátula sea .png.
	thumbFile := e.path("biblioteca", "covers", fmt.Sprintf("%d-256.jpg", bigAlbum))
	longAgo := time.Now().Add(-2 * ingest.OrphanCoverGrace)
	if err := os.Chtimes(thumbFile, longAgo, longAgo); err != nil {
		t.Fatal(err)
	}
	if _, err := e.im.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if !exists(t, thumbFile) {
		t.Error("Recover borró la miniatura de un álbum con carátula")
	}
}

// Una carátula con demasiados píxeles se importa con su original y sin miniatura:
// decodificarla para reducirla costaría a ffmpeg gigas de memoria.
func TestImportSkipsTheThumbnailOfAHugeCover(t *testing.T) {
	e := newImportEnv(t)
	dir := t.TempDir()
	huge := ffmpeg(t, dir, "huge.png", "-f", "lavfi", "-i", "color=c=gray:s=9000x9000", "-pix_fmt", "gray", "-frames:v", "1")
	e.put(t, "enorme/01.flac", "44100", embedArgs(huge)...)
	album := albumIDOf(t, e.pool, e.importOK(t, "enorme/01.flac", ingest.Imported).TrackID)

	if !exists(t, e.path("biblioteca", "covers", fmt.Sprintf("%d.png", album))) {
		t.Error("la carátula original no se importó")
	}
	if exists(t, e.path("biblioteca", "covers", fmt.Sprintf("%d-256.jpg", album))) {
		t.Error("una carátula de 81 Mpx no debía decodificarse para la miniatura")
	}
}

// pngSize lee el ancho y el alto de un PNG.
func pngSize(t *testing.T, path string) (int, int) {
	t.Helper()
	cfg, err := png.DecodeConfig(bytes.NewReader(readFile(t, path)))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Width, cfg.Height
}

// Recover avisa de las carátulas que ningún álbum referencia y las devuelve, sin
// borrarlas mientras no pase el plazo de gracia. La carátula buena de un álbum no
// cuenta.
func TestRecoverReportsOrphanCovers(t *testing.T) {
	e := newImportEnv(t)
	pic, _ := picture(t, t.TempDir(), "cover.jpg")
	e.put(t, "a/01.flac", "44100", embedArgs(pic)...)
	withCover := albumIDOf(t, e.pool, e.importOK(t, "a/01.flac", ingest.Imported).TrackID)
	e.put(t, "b/01.flac", "44100", "-c:a", "flac", "-metadata", "album=Otro")
	without := albumIDOf(t, e.pool, e.importOK(t, "b/01.flac", ingest.Imported).TrackID)
	if withCover == without {
		t.Fatal("las dos pistas cayeron en el mismo álbum")
	}

	good := e.path("biblioteca", "covers", fmt.Sprintf("%d.jpg", withCover))
	blocking := fmt.Sprintf("%d.jpg", without) // el caso de ADR-015: álbum sin cover_ext
	orphans := []string{
		e.path("biblioteca", "covers", blocking),
		e.path("biblioteca", "covers", fmt.Sprintf("%d.png", withCover)),
		e.path("biblioteca", "covers", "999999.jpg"),
		e.path("biblioteca", "covers", "nota.txt"),
		e.path("biblioteca", "covers", fmt.Sprintf("+%d.jpg", withCover)),
	}
	for _, p := range orphans {
		if err := os.WriteFile(p, []byte("resto de un corte"), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	var logs bytes.Buffer
	im, err := ingest.NewImporter(e.dir, e.pool, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := im.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	slices.Sort(orphans)
	if !slices.Equal(got, orphans) {
		t.Errorf("huérfanos %v, want %v", got, orphans)
	}
	for _, p := range append(orphans, good) {
		if !exists(t, p) {
			t.Errorf("Recover borró %s", p)
		}
	}
	want := fmt.Sprintf("level=WARN msg=\"carátula que ningún álbum referencia; se borrará pasado el plazo de gracia, y hasta entonces puede bloquear las pistas nuevas del álbum\" álbum=%d archivo=covers/%s", without, blocking)
	if !strings.Contains(logs.String(), want) {
		t.Errorf("falta el aviso %q en el registro:\n%s", want, logs.String())
	}
}

// Pasado el plazo de gracia, Recover borra las carátulas sin dueño con forma de
// carátula. Lo que no lo tiene (nota.txt) y la carátula de un álbum se quedan.
func TestRecoverRemovesOldOrphanCovers(t *testing.T) {
	e := newImportEnv(t)
	pic, _ := picture(t, t.TempDir(), "cover.jpg")
	e.put(t, "a/01.flac", "44100", embedArgs(pic)...)
	withCover := albumIDOf(t, e.pool, e.importOK(t, "a/01.flac", ingest.Imported).TrackID)
	e.put(t, "b/01.flac", "44100", "-c:a", "flac", "-metadata", "album=Otro")
	without := albumIDOf(t, e.pool, e.importOK(t, "b/01.flac", ingest.Imported).TrackID)

	good := e.path("biblioteca", "covers", fmt.Sprintf("%d.jpg", withCover))
	old := e.path("biblioteca", "covers", fmt.Sprintf("%d.jpg", without))
	young := e.path("biblioteca", "covers", fmt.Sprintf("%d.png", without))
	note := e.path("biblioteca", "covers", "nota.txt")
	for _, p := range []string{old, young, note} {
		if err := os.WriteFile(p, []byte("resto de un corte"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	// La carátula buena también es antigua: que sea vieja no basta, tiene que no tener dueño.
	longAgo := time.Now().Add(-2 * ingest.OrphanCoverGrace)
	for _, p := range []string{good, old, note} {
		if err := os.Chtimes(p, longAgo, longAgo); err != nil {
			t.Fatal(err)
		}
	}

	var logs bytes.Buffer
	im, err := ingest.NewImporter(e.dir, e.pool, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := im.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	want := []string{young, note}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("huérfanos que quedan %v, want %v", got, want)
	}
	if exists(t, old) {
		t.Error("la carátula sin dueño y vieja no se borró")
	}
	for _, p := range []string{good, young, note} {
		if !exists(t, p) {
			t.Errorf("Recover borró %s", p)
		}
	}
	if !strings.Contains(logs.String(), "carátula sin dueño borrada") {
		t.Errorf("falta el aviso del borrado en el registro: %s", logs.String())
	}
}
