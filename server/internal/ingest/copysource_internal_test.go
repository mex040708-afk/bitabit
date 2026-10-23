package ingest

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newCopyEnv prepara una carpeta de datos con entrada/ y una carpeta de trabajo, y
// un Importer que solo sabe abrir con el Root: lo único que copySource necesita.
func newCopyEnv(t *testing.T) (im *Importer, dataDir, work string) {
	t.Helper()
	dataDir = t.TempDir()
	if err := os.Mkdir(filepath.Join(dataDir, "entrada"), 0o750); err != nil {
		t.Fatal(err)
	}
	work = t.TempDir()
	root, err := os.OpenRoot(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return &Importer{data: root}, dataDir, work
}

func TestCopySourceCopiesAndHashesTheSameBytes(t *testing.T) {
	im, dir, work := newCopyEnv(t)
	content := []byte("unos bytes de audio")
	if err := os.WriteFile(filepath.Join(dir, "entrada", "a.FLAC"), content, 0o640); err != nil {
		t.Fatal(err)
	}
	copied, sum, err := im.copySource("a.FLAC", work)
	if err != nil {
		t.Fatalf("copySource: %v", err)
	}
	got, err := os.ReadFile(copied)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) || sum != sha256.Sum256(content) {
		t.Errorf("la copia %q y su hash %x no son los de la fuente", got, sum)
	}
	if filepath.Ext(copied) != ".flac" {
		t.Errorf("la copia %s debería llevar la extensión en minúsculas", copied)
	}
}

// Un archivo que no está, o que no se puede abrir por permisos, es un fallo del
// sistema que se reintenta: no se manda a fallidos/.
func TestCopySourceMissingFileIsASystemError(t *testing.T) {
	im, _, work := newCopyEnv(t)
	_, _, err := im.copySource("no-existe.flac", work)
	var unsafe *unsafeSourceError
	if err == nil || errors.As(err, &unsafe) || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("copySource = %v, want un error de sistema con ErrNotExist", err)
	}
}

// Un enlace que sale de la carpeta de datos es lo único que va a fallidos/ por no
// poder abrirse con seguridad.
func TestCopySourceSymlinkOutIsUnsafe(t *testing.T) {
	im, dir, work := newCopyEnv(t)
	outside := filepath.Join(t.TempDir(), "secreto.flac")
	if err := os.WriteFile(outside, []byte("fuera"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "entrada", "enlace.flac")); err != nil {
		t.Skipf("no se pueden crear enlaces simbólicos aquí: %v", err)
	}
	_, _, err := im.copySource("enlace.flac", work)
	var unsafe *unsafeSourceError
	if !errors.As(err, &unsafe) {
		t.Fatalf("copySource = %v, want unsafeSourceError", err)
	}
	// Si una versión futura de Go cambia el texto del error, esto avisa.
	if !isUnsafePath(errors.New("openat entrada/x: path escapes from parent")) {
		t.Error("isUnsafePath no reconoce el error de os.Root")
	}
	if isUnsafePath(fs.ErrPermission) || isUnsafePath(fs.ErrNotExist) {
		t.Error("isUnsafePath toma un fallo del sistema por un camino inseguro")
	}
}

func TestCheckUnchanged(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkUnchanged(before, before, 5); err != nil {
		t.Errorf("un archivo igual da error: %v", err)
	}
	if err := checkUnchanged(before, before, 3); err == nil {
		t.Error("copiar menos bytes de los que mide no da error")
	}
	if err := os.WriteFile(p, []byte("123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	grown, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkUnchanged(before, grown, 9); err == nil {
		t.Error("un archivo que creció mientras se copiaba no da error")
	}
	later := before.ModTime().Add(time.Second)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	touched, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkUnchanged(before, touched, 5); err == nil {
		t.Error("un archivo con otra fecha de modificación no da error")
	}
}
