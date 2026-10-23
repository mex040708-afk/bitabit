package ingest

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Una fuente que ffprobe no entiende no tira la pista: se avisa y se importa sin
// carátula, que es opcional.
func TestStageCoverSkipsUnreadableSource(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skipf("hace falta ffprobe en el PATH: %v", err)
	}
	// Con extensión .flac, ffprobe acepta cualquier basura sin error; con .bin la rechaza.
	dir := t.TempDir()
	src := filepath.Join(dir, "roto.bin")
	if err := os.WriteFile(src, []byte{0x00, 0xFF, 0x13, 0x37, 0x42, 0x00, 0x9A, 0x01}, 0o640); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	im := &Importer{log: slog.New(slog.NewTextHandler(&logs, nil))}
	c, err := im.stageCover(context.Background(), "roto.bin", src, dir)
	if err != nil {
		t.Fatalf("stageCover: %v", err)
	}
	if c.Path != "" {
		t.Errorf("stageCover dejó una carátula en %s", c.Path)
	}
	if !strings.Contains(logs.String(), "pista sin carátula") {
		t.Errorf("no se avisó en el registro: %q", logs.String())
	}
}

func TestImageExt(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"JPEG", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "jpg"},
		{"PNG", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), "png"},
		{"BMP", []byte("BM\x36\x00"), ""},
		{"GIF", []byte("GIF89a"), ""},
		{"JPEG cortado", []byte{0xFF, 0xD8}, ""},
		{"vacío", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := imageExt(tt.data); got != tt.want {
				t.Errorf("imageExt = %q, want %q", got, tt.want)
			}
		})
	}
}
