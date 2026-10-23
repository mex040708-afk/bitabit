package ingest_test

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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

// Un enlace simbólico en entrada/ que apunta fuera de la carpeta de datos no se
// lee: la fuente va a fallidos/ sin que su destino llegue a ningún proceso (ADR-032).
func TestImportRefusesASymlinkLeavingTheDataDir(t *testing.T) {
	e := newImportEnv(t)
	outside := filepath.Join(t.TempDir(), "secreto.flac")
	if err := os.WriteFile(outside, []byte("fuera de la carpeta de datos"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := e.path("entrada", "enlace.flac")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("no se pueden crear enlaces simbólicos aquí: %v", err)
	}

	res, err := e.im.Import(context.Background(), "enlace.flac")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Outcome != ingest.Failed || !strings.Contains(res.Reason, "de forma segura") {
		t.Errorf("resultado %v (%q), want Failed por no abrirse con seguridad", res.Outcome, res.Reason)
	}
	if n := countRows(t, e.pool, "tracks"); n != 0 {
		t.Errorf("hay %d pistas, want 0", n)
	}
	if got := readFile(t, outside); string(got) != "fuera de la carpeta de datos" {
		t.Error("el archivo de fuera cambió")
	}
	if exists(t, link) {
		t.Error("el enlace sigue en entrada/")
	}
}

// Un enlace simbólico a una carpeta de fuera: ni la vigilancia la recorre ni Import
// lee lo que hay dentro (ADR-032).
func TestInboxAndImportDoNotFollowADirectorySymlinkOut(t *testing.T) {
	e := newImportEnv(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "a.flac"), []byte("fuera de la carpeta de datos"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, e.path("entrada", "carpeta")); err != nil {
		t.Skipf("no se pueden crear enlaces simbólicos aquí: %v", err)
	}

	root, err := os.OpenRoot(e.path("entrada"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ready, err := ingest.NewInboxFS(root.FS(), 0).Scan(time.Now())
	if err != nil || len(ready) != 0 {
		t.Errorf("Scan = %v, %v; no debía ver nada tras el enlace", ready, err)
	}

	res, err := e.im.Import(context.Background(), "carpeta/a.flac")
	if err == nil && res.Outcome == ingest.Imported {
		t.Fatalf("Import importó algo de fuera: %+v", res)
	}
	if n := countRows(t, e.pool, "tracks"); n != 0 {
		t.Errorf("hay %d pistas, want 0", n)
	}
	if got := readFile(t, filepath.Join(outside, "a.flac")); string(got) != "fuera de la carpeta de datos" {
		t.Error("el archivo de fuera cambió")
	}
}

// Solo se borra lo que el importador pudo escribir: ID positivo y jpg o png. Un
// archivo viejo con otro nombre, una carpeta o un enlace simbólico se quedan.
func TestRecoverOnlyRemovesFilesShapedLikeCovers(t *testing.T) {
	e := newImportEnv(t)
	covers := e.path("biblioteca", "covers")
	stay := []string{"12.txt", "12.jpg.bak", "-5.jpg", "0.jpg", "012.jpg", "12-256.png"}
	longAgo := time.Now().Add(-2 * ingest.OrphanCoverGrace)
	for _, name := range stay {
		p := e.path("biblioteca", "covers", name)
		if err := os.WriteFile(p, []byte("no es nuestro"), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, longAgo, longAgo); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(e.path("biblioteca", "covers", "777.jpg"), 0o750); err != nil {
		t.Fatal(err)
	}
	stay = append(stay, "777.jpg")
	if err := os.Symlink("12.txt", e.path("biblioteca", "covers", "778.jpg")); err == nil {
		stay = append(stay, "778.jpg")
	}

	im, err := ingest.NewImporter(e.dir, e.pool, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := im.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	for _, name := range stay {
		if _, err := os.Lstat(covers + "/" + name); err != nil {
			t.Errorf("Recover borró %s: %v", name, err)
		}
	}
}

// Si una carátula sin dueño no se puede borrar, el servidor arranca igual: Recover
// avisa y la deja entre los huérfanos.
func TestRecoverSurvivesAnOrphanCoverItCannotRemove(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root puede borrar en una carpeta de solo lectura")
	}
	e := newImportEnv(t)
	// Un álbum con carátula, para que la base de datos no parezca equivocada.
	pic, _ := picture(t, t.TempDir(), "cover.jpg")
	e.put(t, "a/01.flac", "44100", embedArgs(pic)...)
	e.importOK(t, "a/01.flac", ingest.Imported)
	old := e.path("biblioteca", "covers", "4242.jpg")
	if err := os.WriteFile(old, []byte("resto de un corte"), 0o640); err != nil {
		t.Fatal(err)
	}
	longAgo := time.Now().Add(-2 * ingest.OrphanCoverGrace)
	if err := os.Chtimes(old, longAgo, longAgo); err != nil {
		t.Fatal(err)
	}
	covers := e.path("biblioteca", "covers")
	if err := os.Chmod(covers, 0o500); err != nil {
		t.Fatal(err)
	}
	// Sin permisos de escritura, t.TempDir no podría borrar la carpeta al acabar.
	t.Cleanup(func() {
		if err := os.Chmod(covers, 0o750); err != nil {
			t.Error(err)
		}
	})

	var logs bytes.Buffer
	im, err := ingest.NewImporter(e.dir, e.pool, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := im.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v; un fallo al borrar no debe impedir el arranque", err)
	}
	if !slices.Equal(got, []string{old}) {
		t.Errorf("huérfanos %v, want [%s]", got, old)
	}
	if !strings.Contains(logs.String(), "no se pudo borrar una carátula sin dueño") {
		t.Errorf("falta el aviso en el registro: %s", logs.String())
	}
}

// La salvaguarda: con una base de datos que no tiene ninguna carátula mientras en
// disco hay, no se borra nada aunque los archivos sean viejos; y tampoco si habría que
// borrar más de la mitad y más de 20.
func TestRecoverDoesNotDeleteWhenTheDatabaseLooksWrong(t *testing.T) {
	e := newImportEnv(t)
	longAgo := time.Now().Add(-2 * ingest.OrphanCoverGrace)
	old := func(name string) string {
		p := e.path("biblioteca", "covers", name)
		if err := os.WriteFile(p, []byte("carátula de otra biblioteca"), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, longAgo, longAgo); err != nil {
			t.Fatal(err)
		}
		return p
	}
	recover := func() {
		t.Helper()
		im, err := ingest.NewImporter(e.dir, e.pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := im.Recover(context.Background()); err != nil {
			t.Fatalf("Recover: %v", err)
		}
	}

	// 1. La base no tiene ninguna carátula y en disco hay una (vieja).
	lonely := old("500.jpg")
	recover()
	if !exists(t, lonely) {
		t.Fatal("se borró una carátula con una base de datos sin ninguna carátula")
	}

	// 2. Hay una carátula buena, pero habría que borrar 25 de 26: más de la mitad y más de 20.
	pic, _ := picture(t, t.TempDir(), "cover.jpg")
	e.put(t, "a/01.flac", "44100", embedArgs(pic)...)
	e.importOK(t, "a/01.flac", ingest.Imported)
	var many []string
	for i := 0; i < 24; i++ {
		many = append(many, old(fmt.Sprintf("%d.jpg", 1000+i)))
	}
	recover()
	for _, p := range append(many, lonely) {
		if !exists(t, p) {
			t.Fatalf("se borró %s con una base de datos que parece equivocada", p)
		}
	}

	// 3. Con pocas sin dueño frente a las buenas, sí se borra (lo normal).
	for _, p := range many[1:] {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(lonely); err != nil {
		t.Fatal(err)
	}
	recover()
	if exists(t, many[0]) {
		t.Error("una sola carátula sin dueño y vieja, con la base en orden, debía borrarse")
	}
}

// Una fuente que pasa del tope (4 GiB con la biblioteca vacía) va a fallidos/ con su
// nota, sin leerse: es un archivo disperso, así que leerlo entero costaría segundos.
func TestImportSendsAFileOverTheLimitToFailedWithItsNote(t *testing.T) {
	e := newImportEnv(t)
	big := e.path("entrada", "enorme.flac")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(5 << 30); err != nil {
		f.Close()
		t.Skipf("no se puede crear un archivo disperso de 5 GiB aquí: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	res, err := e.im.Import(context.Background(), "enorme.flac")
	if err != nil || res.Outcome != ingest.Failed || !strings.Contains(res.Reason, "máximo") {
		t.Fatalf("Import = %+v, %v; want Failed por el tope", res, err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("tardó %v: parece que se leyó entero", took)
	}
	if exists(t, big) {
		t.Error("el archivo sigue en entrada/")
	}
	if !exists(t, e.path("fallidos", "enorme.flac")) || !exists(t, e.path("fallidos", "enorme.flac.error.txt")) {
		t.Error("faltan el archivo o su nota en fallidos/")
	}
	if n := countRows(t, e.pool, "tracks"); n != 0 {
		t.Errorf("hay %d pistas, want 0", n)
	}
}

// Cambiar una carpeta de entrada/ por un enlace a otra de la carpeta de datos (como
// biblioteca/orig) no deja leer ni borrar nada de ahí: el Root de entrada/ lo corta.
func TestImportDoesNotFollowADirectorySymlinkToTheLibrary(t *testing.T) {
	e := newImportEnv(t)
	victim := e.path("biblioteca", "orig", "999.flac")
	if err := os.WriteFile(victim, []byte("un original de la biblioteca"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(e.path("biblioteca", "orig"), e.path("entrada", "atajo")); err != nil {
		t.Skipf("no se pueden crear enlaces simbólicos aquí: %v", err)
	}
	res, err := e.im.Import(context.Background(), "atajo/999.flac")
	if err == nil && res.Outcome == ingest.Imported {
		t.Fatalf("Import importó algo a través del enlace: %+v", res)
	}
	if got := readFile(t, victim); string(got) != "un original de la biblioteca" {
		t.Error("el original de la biblioteca cambió")
	}
	if n := countRows(t, e.pool, "tracks"); n != 0 {
		t.Errorf("hay %d pistas, want 0", n)
	}
}
