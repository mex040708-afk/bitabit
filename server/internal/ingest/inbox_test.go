package ingest_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/ingest"
)

const quiet = time.Minute

var t0 = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// write crea dir/rel con contenido, y las carpetas que falten.
func write(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func scan(t *testing.T, in *ingest.Inbox, now time.Time) []ingest.Candidate {
	t.Helper()
	got, err := in.Scan(now)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return got
}

func paths(cs []ingest.Candidate) []string {
	var ps []string
	for _, c := range cs {
		ps = append(ps, c.Path)
	}
	return ps
}

func TestScanWaitsUntilQuiet(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.flac", "12345")
	in := ingest.NewInbox(dir, quiet)

	if got := scan(t, in, t0); len(got) != 0 {
		t.Fatalf("recién visto ya está listo: %v", got)
	}
	if got := scan(t, in, t0.Add(quiet-time.Second)); len(got) != 0 {
		t.Fatalf("listo antes del plazo: %v", got)
	}
	got := scan(t, in, t0.Add(quiet))
	want := []ingest.Candidate{{Path: "a.flac", Size: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("al cumplir el plazo: %v, want %v", got, want)
	}
	// Sigue listo mientras nadie lo saque de la entrada.
	if got := scan(t, in, t0.Add(2*quiet)); !reflect.DeepEqual(got, want) {
		t.Fatalf("en el siguiente sondeo: %v, want %v", got, want)
	}
}

func TestScanRestartsWhenSizeChanges(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "a.flac", "123")
	mtime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	in := ingest.NewInbox(dir, quiet)
	scan(t, in, t0)

	// La copia sigue: crece, pero la fecha se queda igual (algunas
	// herramientas copian la fecha de origen antes de terminar).
	write(t, dir, "a.flac", "123456")
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if got := scan(t, in, t0.Add(quiet)); len(got) != 0 {
		t.Fatalf("listo aunque cambió de tamaño: %v", got)
	}
	if got := scan(t, in, t0.Add(2*quiet)); !reflect.DeepEqual(paths(got), []string{"a.flac"}) {
		t.Fatalf("un plazo después del cambio: %v", got)
	}
}

func TestScanRestartsWhenModTimeChanges(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "a.flac", "123")
	if err := os.Chtimes(p, t0, t0); err != nil {
		t.Fatal(err)
	}
	in := ingest.NewInbox(dir, quiet)
	scan(t, in, t0)

	later := t0.Add(time.Second)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	if got := scan(t, in, t0.Add(quiet)); len(got) != 0 {
		t.Fatalf("listo aunque cambió la fecha: %v", got)
	}
	if got := scan(t, in, t0.Add(2*quiet)); !reflect.DeepEqual(paths(got), []string{"a.flac"}) {
		t.Fatalf("un plazo después del cambio: %v", got)
	}
}

func TestScanFiltersAndOrders(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{
		"h.opus", "g.ogg", "f.m4a", "e.aif", "d.aiff", "c.wav", "b.mp3",
		"Album/02.FLAC", "Album/01.flac",
		"Album/cover.jpg", "Album/album.cue", "notes.txt", "j.wma",
		"i.flac.part",           // descarga a medias
		"Album/.01.flac.Xa3b9c", // temporal de rsync
		".oculta/x.flac",        // carpeta oculta
		"sin-extension",
	} {
		write(t, dir, rel, "x")
	}
	in := ingest.NewInbox(dir, quiet)
	scan(t, in, t0)

	got := paths(scan(t, in, t0.Add(quiet)))
	want := []string{
		"Album/01.flac", "Album/02.FLAC",
		"b.mp3", "c.wav", "d.aiff", "e.aif", "f.m4a", "g.ogg", "h.opus",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}

func TestScanForgetsRemovedFiles(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "a.flac", "123")
	in := ingest.NewInbox(dir, quiet)
	scan(t, in, t0)

	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	scan(t, in, t0.Add(quiet/2))

	// Vuelve igual que estaba: el plazo empieza de cero.
	write(t, dir, "a.flac", "123")
	if got := scan(t, in, t0.Add(quiet)); len(got) != 0 {
		t.Fatalf("listo con el plazo de antes de borrarlo: %v", got)
	}
	if got := scan(t, in, t0.Add(2*quiet)); !reflect.DeepEqual(paths(got), []string{"a.flac"}) {
		t.Fatalf("un plazo después de volver: %v", got)
	}
}

func TestScanSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := write(t, t.TempDir(), "fuera.flac", "x")
	if err := os.Symlink(target, filepath.Join(dir, "enlace.flac")); err != nil {
		t.Skipf("este sistema no deja crear enlaces simbólicos: %v", err)
	}
	write(t, dir, "real.flac", "x")
	in := ingest.NewInbox(dir, quiet)
	scan(t, in, t0)

	if got := paths(scan(t, in, t0.Add(quiet))); !reflect.DeepEqual(got, []string{"real.flac"}) {
		t.Fatalf("got %v, want solo real.flac", got)
	}
}

func TestScanEmptyInbox(t *testing.T) {
	in := ingest.NewInbox(t.TempDir(), quiet)
	if got := scan(t, in, t0); len(got) != 0 {
		t.Fatalf("entrada vacía: %v", got)
	}
}

func TestScanMissingInbox(t *testing.T) {
	in := ingest.NewInbox(filepath.Join(t.TempDir(), "no-existe"), quiet)
	if _, err := in.Scan(t0); err == nil {
		t.Fatal("Scan no falla con una entrada que no existe")
	}
}
