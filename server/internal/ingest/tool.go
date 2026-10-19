package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// allowedFormats son los contenedores que ffprobe y ffmpeg pueden abrir.
	// mov cubre m4a.
	allowedFormats = "flac,wav,aiff,mp3,mov,ogg"
	maxToolStderr  = 4 << 10
	maxToolOutput  = 64 << 10
)

// ErrUnreadable indica que el problema es el archivo y no el sistema: se mueve a
// fallidos/ en vez de reintentarlo.
var ErrUnreadable = errors.New("archivo de audio no válido")

// runTool ejecuta un programa externo con plazo y devuelve su salida estándar.
// Si el programa falla o se pasa del plazo, el error envuelve ErrUnreadable: el
// problema es el archivo que procesa. Si no se puede ejecutar o se cancela ctx,
// el problema es del sistema.
func runTool(ctx context.Context, timeout time.Duration, maxOutput int, name string, args ...string) ([]byte, error) {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout := &cappedBuffer{max: maxOutput}
	stderr := &cappedBuffer{max: maxToolStderr}
	cmd := exec.CommandContext(tctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s: %w", name, ctx.Err())
	}
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("%w: %s tardó más de %v", ErrUnreadable, name, timeout)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil, fmt.Errorf("%w: %s: %s", ErrUnreadable, name, firstLine(stderr.String()))
	}
	if err != nil {
		return nil, fmt.Errorf("ejecutar %s: %w", name, err)
	}
	if stdout.truncated {
		return nil, fmt.Errorf("%w: la salida de %s pasa de %d bytes", ErrUnreadable, name, maxOutput)
	}
	return stdout.Bytes(), nil
}

// ffmpegInput son los argumentos de ffmpeg y ffprobe para abrir file: solo
// archivos locales y solo los contenedores admitidos, para que un .m4a que sea
// una lista HLS no consiga que pidan URLs. Con file: delante, ningún nombre se
// toma por otro protocolo (concat:, http:), y al ser absoluta tampoco por una
// opción.
func ffmpegInput(file string) []string {
	return []string{
		"-protocol_whitelist", "file",
		"-format_whitelist", allowedFormats,
		"-i", ffmpegPath(file),
	}
}

// ffmpegPath da una ruta absoluta en la forma que ffmpeg toma siempre por un
// archivo local.
func ffmpegPath(file string) string {
	return "file:" + filepath.ToSlash(file)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

// cappedBuffer guarda hasta max bytes y descarta el resto sin fallar, para que
// el programa no se quede bloqueado escribiendo. Después se mira truncated.
type cappedBuffer struct {
	bytes.Buffer
	max       int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	room := b.max - b.Len()
	if len(p) <= room {
		return b.Buffer.Write(p)
	}
	b.truncated = true
	if room > 0 {
		if _, err := b.Buffer.Write(p[:room]); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}
