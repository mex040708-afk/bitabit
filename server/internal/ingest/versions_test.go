package ingest_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/ingest"
)

// Estos tests necesitan ffmpeg, ffprobe, flac y metaflac. En Windows no hay
// flac: se pasan en el servidor de pruebas (bitabit-servidor).
func needEncoders(t *testing.T) {
	t.Helper()
	needFFmpeg(t)
	for _, bin := range []string{"flac", "metaflac"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("hace falta %s en el PATH: %v", bin, err)
		}
	}
}

// tone25 genera 25 segundos de tono: da tres puntos en una tabla de saltos de
// uno cada 10 s.
func tone25(t *testing.T, dir, name string, rate int, args ...string) string {
	t.Helper()
	in := []string{"-f", "lavfi", "-i", "sine=frequency=1000:duration=25:sample_rate=" + strconv.Itoa(rate)}
	return ffmpeg(t, dir, name, append(in, args...)...)
}

func output(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
	return strings.TrimSpace(string(out))
}

// pcmMD5 calcula el MD5 del audio decodificado sin pasar por el paquete: es la
// prueba independiente de que no cambió ni un bit.
func pcmMD5(t *testing.T, file, pcm string) string {
	t.Helper()
	out := output(t, "ffmpeg", "-v", "error", "-i", file, "-map", "0:a:0", "-c:a", pcm, "-f", "md5", "-")
	return strings.TrimPrefix(out, "MD5=")
}

var seekPointRE = regexp.MustCompile(`(?m)^\s*point \d+:`)

func seekPoints(t *testing.T, file string) int {
	t.Helper()
	return len(seekPointRE.FindAllString(output(t, "metaflac", "--list", "--block-type=SEEKTABLE", file), -1))
}

func probe(t *testing.T, file string) ingest.Info {
	t.Helper()
	info, err := ingest.Probe(context.Background(), file)
	if err != nil {
		t.Fatalf("Probe(%s): %v", filepath.Base(file), err)
	}
	return info
}

func build(t *testing.T, src string) (ingest.Versions, string) {
	t.Helper()
	work := t.TempDir()
	v, err := ingest.BuildVersions(context.Background(), src, probe(t, src), work)
	if err != nil {
		t.Fatalf("BuildVersions: %v", err)
	}
	return v, work
}

func files(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names // os.ReadDir ya las da ordenadas
}

func TestBuildVersionsLossless(t *testing.T) {
	needEncoders(t)
	dir := t.TempDir()
	tests := []struct {
		name     string
		rate     int
		args     []string
		pcm      string // ancho con el que se comparan fuente y original
		bits     int
		channels int
		aacRate  int
		aacChans int
	}{
		{"nativo-24-96.flac", 96000, []string{"-ac", "2", "-c:a", "flac", "-sample_fmt", "s32"},
			"pcm_s24le", 24, 2, 48000, 2},
		// El FLAC de ffmpeg no trae tabla de saltos: la pone metaflac.
		{"nativo-16-44.flac", 44100, []string{"-c:a", "flac"},
			"pcm_s16le", 16, 1, 44100, 1},
		{"nativo-24-88.flac", 88200, []string{"-c:a", "flac", "-sample_fmt", "s32"},
			"pcm_s24le", 24, 1, 44100, 1},
		{"wav-16-44.wav", 44100, []string{"-c:a", "pcm_s16le"},
			"pcm_s16le", 16, 1, 44100, 1},
		{"aiff-24-48.aiff", 48000, []string{"-c:a", "pcm_s24be"},
			"pcm_s24le", 24, 1, 48000, 1},
		{"alac-24-44.m4a", 44100, []string{"-c:a", "alac", "-sample_fmt", "s32p"},
			"pcm_s24le", 24, 1, 44100, 1},
		{"wav-6-canales.wav", 48000, []string{"-ac", "6", "-c:a", "pcm_s24le"},
			"pcm_s24le", 24, 6, 48000, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := tone25(t, dir, tt.name, tt.rate, tt.args...)
			v, work := build(t, src)

			if v.OriginalExt != "flac" || v.Original != filepath.Join(work, "original.flac") {
				t.Fatalf("original: %q (%s)", v.Original, v.OriginalExt)
			}
			orig := probe(t, v.Original)
			if orig.Codec != "flac" || orig.Bits != tt.bits || orig.Channels != tt.channels || orig.SampleRate != tt.rate {
				t.Errorf("original: %+v, want flac de %d bits, %d canales y %d Hz", orig, tt.bits, tt.channels, tt.rate)
			}
			srcMD5 := pcmMD5(t, src, tt.pcm)
			if got := pcmMD5(t, v.Original, tt.pcm); got != srcMD5 {
				t.Errorf("el audio cambió: MD5 %s, fuente %s", got, srcMD5)
			}
			if v.AudioMD5 != srcMD5 || v.AudioMD5 != output(t, "metaflac", "--show-md5sum", v.Original) {
				t.Errorf("AudioMD5 = %q, want %q (el de STREAMINFO)", v.AudioMD5, srcMD5)
			}
			if n := seekPoints(t, v.Original); n < 3 {
				t.Errorf("tabla de saltos con %d puntos, want al menos 3", n)
			}

			aac := probe(t, v.AAC)
			if aac.Codec != "aac" || aac.SampleRate != tt.aacRate || aac.Channels != tt.aacChans {
				t.Errorf("AAC: %+v, want aac a %d Hz con %d canales", aac, tt.aacRate, tt.aacChans)
			}
			if d := aac.Duration - 25*time.Second; d < -200*time.Millisecond || d > 200*time.Millisecond {
				t.Errorf("AAC dura %v, want unos 25s", aac.Duration)
			}
			if got := files(t, work); !slices.Equal(got, []string{"aac.m4a", "original.flac"}) {
				t.Errorf("la carpeta de trabajo tiene %v, want solo aac.m4a y original.flac", got)
			}
		})
	}
}

func TestBuildVersionsLossyIsCopied(t *testing.T) {
	needEncoders(t)
	dir := t.TempDir()
	tests := []struct {
		name, ext string
		args      []string
	}{
		{"a.mp3", "mp3", []string{"-c:a", "libmp3lame", "-b:a", "320k"}},
		{"MAYUS.MP3", "mp3", []string{"-c:a", "libmp3lame", "-b:a", "192k"}},
		{"aac.m4a", "m4a", []string{"-c:a", "aac", "-b:a", "256k"}},
		{"a.opus", "opus", []string{"-c:a", "libopus"}},
		{"a.ogg", "ogg", []string{"-c:a", "libvorbis"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := tone(t, dir, tt.name, "48000", tt.args...)
			v, work := build(t, src)
			if v.OriginalExt != tt.ext || v.Original != filepath.Join(work, "original."+tt.ext) {
				t.Errorf("original: %q (%s), want original.%s", v.Original, v.OriginalExt, tt.ext)
			}
			if v.AAC != "" || v.AudioMD5 != "" {
				t.Errorf("con pérdida no hay AAC ni MD5: %+v", v)
			}
			want, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(v.Original)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Error("la copia no es idéntica byte a byte")
			}
		})
	}
}

// Algunos codificadores no escriben el MD5 (lo dejan a ceros). Se acepta y se
// guarda sin MD5: flac -t sigue comprobando el CRC de cada trama.
func TestBuildVersionsFLACWithoutMD5(t *testing.T) {
	needEncoders(t)
	dir := t.TempDir()
	src := tone(t, dir, "sin-md5.flac", "44100", "-c:a", "flac")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	// "fLaC", la cabecera del bloque STREAMINFO (tipo 0) y sus 34 bytes, de
	// los que el MD5 son los 16 últimos.
	if string(data[:4]) != "fLaC" || data[4]&0x7f != 0 {
		t.Fatal("el FLAC no empieza por STREAMINFO")
	}
	clear(data[26:42])
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if sum := output(t, "metaflac", "--show-md5sum", src); sum != strings.Repeat("0", 32) {
		t.Fatalf("la fuente debería quedar sin MD5 y trae %s", sum)
	}

	v, _ := build(t, src)
	if v.AudioMD5 != "" || v.AAC == "" {
		t.Errorf("Versions = %+v, want sin MD5 y con AAC", v)
	}
	if got, want := pcmMD5(t, v.Original, "pcm_s16le"), pcmMD5(t, src, "pcm_s16le"); got != want {
		t.Errorf("el audio cambió: MD5 %s, fuente %s", got, want)
	}
}

func TestBuildVersionsRejectsBadFiles(t *testing.T) {
	needEncoders(t)
	dir := t.TempDir()

	corrupt := tone(t, dir, "corrupto.flac", "44100", "-c:a", "flac")
	data, err := os.ReadFile(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(data) / 2; i < len(data)/2+64; i++ {
		data[i] ^= 0xff
	}
	if err := os.WriteFile(corrupt, data, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct{ desc, file string }{
		{"FLAC dañado", corrupt},
		{"FLAC dentro de Ogg", tone(t, dir, "en-ogg.ogg", "44100", "-c:a", "flac")},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			work := t.TempDir()
			_, err := ingest.BuildVersions(context.Background(), tt.file, probe(t, tt.file), work)
			if !errors.Is(err, ingest.ErrUnreadable) {
				t.Errorf("error %v, want ErrUnreadable", err)
			}
			t.Logf("motivo: %v", err)
		})
	}
}

// Los fallos del sistema no deben mandar el archivo a fallidos/.
func TestBuildVersionsSystemErrorsAreNotUnreadable(t *testing.T) {
	needEncoders(t)
	dir := t.TempDir()
	src := tone(t, dir, "a.wav", "44100", "-c:a", "pcm_s16le")
	info := probe(t, src)

	t.Run("carpeta de trabajo que no existe", func(t *testing.T) {
		_, err := ingest.BuildVersions(context.Background(), src, info, filepath.Join(dir, "no-existe"))
		if err == nil || errors.Is(err, ingest.ErrUnreadable) {
			t.Errorf("error %v, want error sin ErrUnreadable", err)
		}
	})
	t.Run("ruta relativa", func(t *testing.T) {
		_, err := ingest.BuildVersions(context.Background(), "a.wav", info, t.TempDir())
		if err == nil || errors.Is(err, ingest.ErrUnreadable) {
			t.Errorf("error %v, want error sin ErrUnreadable", err)
		}
	})
	t.Run("contexto cancelado", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := ingest.BuildVersions(ctx, src, info, t.TempDir())
		if !errors.Is(err, context.Canceled) || errors.Is(err, ingest.ErrUnreadable) {
			t.Errorf("error %v, want context.Canceled sin ErrUnreadable", err)
		}
	})
}
