package ingest_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"

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

// Recover avisa de las carátulas que ningún álbum referencia y las devuelve, sin
// borrarlas. La carátula buena de un álbum no cuenta.
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
	want := fmt.Sprintf("level=WARN msg=\"carátula que ningún álbum referencia; no se borra, y hasta que se quite puede bloquear las pistas nuevas del álbum\" álbum=%d archivo=covers/%s", without, blocking)
	if !strings.Contains(logs.String(), want) {
		t.Errorf("falta el aviso %q en el registro:\n%s", want, logs.String())
	}
}
