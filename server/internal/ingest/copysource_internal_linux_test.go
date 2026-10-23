package ingest

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Una tubería con nombre en lugar del archivo no deja la importación esperando a un
// escritor que no llegará: se rechaza como algo que no es un archivo regular.
func TestCopySourceDoesNotBlockOnAFIFO(t *testing.T) {
	im, dir, work := newCopyEnv(t)
	if err := syscall.Mkfifo(filepath.Join(dir, "entrada", "tuberia.flac"), 0o600); err != nil {
		t.Skipf("no se puede crear una tubería con nombre aquí: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := im.copySource("tuberia.flac", work)
		done <- err
	}()
	select {
	case err := <-done:
		var unsafe *unsafeSourceError
		if !errors.As(err, &unsafe) {
			t.Errorf("copySource = %v, want unsafeSourceError", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("copySource se quedó esperando en una tubería con nombre")
	}
}
