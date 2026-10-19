package ingest_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/ingest"
)

// Estos tests generan tonos con FFmpeg (Mandamientos del proyecto, 5): ni una
// canción en Git.

var toneTags = []string{
	"-metadata", "title=Tono",
	"-metadata", "artist=Artista",
	"-metadata", "album=Álbum",
	"-metadata", "album_artist=Varios",
	"-metadata", "track=3/12",
	"-metadata", "disc=1/2",
	"-metadata", "date=1977",
}

var fullTags = ingest.Tags{
	Title: "Tono", Artist: "Artista", Album: "Álbum", AlbumArtist: "Varios",
	Track: 3, TrackTotal: 12, Disc: 1, DiscTotal: 2, Date: "1977",
}

func needFFmpeg(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("hace falta %s en el PATH: %v", bin, err)
		}
	}
}

// ffmpeg genera dir/name con los argumentos dados y devuelve su ruta.
func ffmpeg(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	out := filepath.Join(dir, name)
	full := append([]string{"-v", "error", "-y"}, args...)
	full = append(full, out)
	if msg, err := exec.Command("ffmpeg", full...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg %v: %v\n%s", args, err, msg)
	}
	return out
}

// tone genera dos segundos de un tono de 1 kHz.
func tone(t *testing.T, dir, name string, rate string, args ...string) string {
	t.Helper()
	in := []string{"-f", "lavfi", "-i", "sine=frequency=1000:duration=2:sample_rate=" + rate}
	return ffmpeg(t, dir, name, append(in, args...)...)
}

func TestProbeFormats(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	tests := []struct {
		name string
		rate string
		args []string
		want ingest.Info // sin Duration, BitRate ni Tags, que se miran aparte
		full bool        // el contenedor guarda todas las etiquetas de toneTags
	}{
		{"a24.flac", "96000", []string{"-ac", "2", "-c:a", "flac", "-sample_fmt", "s32"},
			ingest.Info{Codec: "flac", Lossless: true, SampleRate: 96000, Bits: 24, Channels: 2}, true},
		{"a16.flac", "44100", []string{"-c:a", "flac"},
			ingest.Info{Codec: "flac", Lossless: true, SampleRate: 44100, Bits: 16, Channels: 1}, true},
		{"a.wav", "44100", []string{"-c:a", "pcm_s16le"},
			ingest.Info{Codec: "pcm_s16le", Lossless: true, SampleRate: 44100, Bits: 16, Channels: 1}, false},
		{"a.aiff", "48000", []string{"-c:a", "pcm_s24be"},
			ingest.Info{Codec: "pcm_s24be", Lossless: true, SampleRate: 48000, Bits: 24, Channels: 1}, false},
		{"alac.m4a", "44100", []string{"-c:a", "alac", "-sample_fmt", "s32p"},
			ingest.Info{Codec: "alac", Lossless: true, SampleRate: 44100, Bits: 24, Channels: 1}, true},
		{"a.mp3", "44100", []string{"-c:a", "libmp3lame", "-b:a", "320k"},
			ingest.Info{Codec: "mp3", SampleRate: 44100, Channels: 1}, true},
		{"aac.m4a", "44100", []string{"-c:a", "aac", "-b:a", "256k"},
			ingest.Info{Codec: "aac", SampleRate: 44100, Channels: 1}, true},
		// Ogg guarda las etiquetas en la pista, no en el contenedor.
		{"a.opus", "48000", []string{"-c:a", "libopus"},
			ingest.Info{Codec: "opus", SampleRate: 48000, Channels: 1}, true},
		{"a.ogg", "44100", []string{"-c:a", "libvorbis"},
			ingest.Info{Codec: "vorbis", SampleRate: 44100, Channels: 1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := tone(t, dir, tt.name, tt.rate, append(tt.args, toneTags...)...)
			got, err := ingest.Probe(context.Background(), file)
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}
			if d := got.Duration - 2*time.Second; d < -50*time.Millisecond || d > 50*time.Millisecond {
				t.Errorf("Duration = %v, want unos 2s", got.Duration)
			}
			if got.BitRate <= 0 {
				t.Errorf("BitRate = %d, want > 0", got.BitRate)
			}
			if tt.full && got.Tags != fullTags {
				t.Errorf("Tags = %+v\nwant   %+v", got.Tags, fullTags)
			}
			if got.Tags.Title != "Tono" {
				t.Errorf("Title = %q, want Tono", got.Tags.Title)
			}
			got.Duration, got.BitRate, got.Tags = 0, 0, ingest.Tags{}
			if got != tt.want {
				t.Errorf("Probe = %+v\nwant    %+v", got, tt.want)
			}
		})
	}
}

// pipedFLAC genera un FLAC escribiéndolo a una tubería, como haría una
// grabación en directo.
func pipedFLAC(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "tuberia.flac")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=2",
		"-c:a", "flac", "-f", "flac", "pipe:1")
	cmd.Stdout = f
	if err := cmd.Run(); err != nil {
		t.Fatalf("ffmpeg: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProbeRejectsBadFiles(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	tests := []struct {
		desc string
		file string
	}{
		// FLAC no puede guardar coma flotante sin cambiar los bits.
		{"PCM en coma flotante", tone(t, dir, "f32.wav", "44100", "-c:a", "pcm_f32le")},
		// ffprobe lo da por bueno sin error, pero sin frecuencia.
		{"texto con extensión flac", write("texto.flac", "esto no es audio")},
		{"lista HLS disfrazada de m4a", write("hls.m4a",
			"#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\nhttp://127.0.0.1:9/x.ts\n#EXT-X-ENDLIST\n")},
		{"archivo vacío", write("vacio.mp3", "")},
		{"dos pistas de audio", ffmpeg(t, dir, "dos.m4a",
			"-f", "lavfi", "-i", "sine=duration=1", "-f", "lavfi", "-i", "sine=duration=1",
			"-map", "0:a", "-map", "1:a", "-c:a", "aac")},
		{"sin pista de audio", ffmpeg(t, dir, "video.m4a",
			"-f", "lavfi", "-i", "color=c=black:s=16x16:d=1", "-c:v", "mpeg4")},
		// El codificador no puede volver atrás a escribir el número de muestras.
		{"FLAC escrito a una tubería, sin duración", pipedFLAC(t, dir)},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			_, err := ingest.Probe(context.Background(), tt.file)
			if !errors.Is(err, ingest.ErrUnreadable) {
				t.Errorf("error %v, want ErrUnreadable", err)
			}
			t.Logf("motivo: %v", err)
		})
	}
}

// Los fallos del sistema no deben mandar el archivo a fallidos/.
func TestProbeSystemErrorsAreNotUnreadable(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	file := tone(t, dir, "a.flac", "44100", "-c:a", "flac")

	t.Run("archivo que desaparece", func(t *testing.T) {
		_, err := ingest.Probe(context.Background(), filepath.Join(dir, "no-existe.flac"))
		if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ingest.ErrUnreadable) {
			t.Errorf("error %v, want fs.ErrNotExist sin ErrUnreadable", err)
		}
	})
	t.Run("ruta relativa", func(t *testing.T) {
		_, err := ingest.Probe(context.Background(), "a.flac")
		if err == nil || errors.Is(err, ingest.ErrUnreadable) {
			t.Errorf("error %v, want error sin ErrUnreadable", err)
		}
	})
	t.Run("contexto cancelado", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := ingest.Probe(ctx, file)
		if !errors.Is(err, context.Canceled) || errors.Is(err, ingest.ErrUnreadable) {
			t.Errorf("error %v, want context.Canceled sin ErrUnreadable", err)
		}
	})
	t.Run("ffprobe no instalado", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := ingest.Probe(context.Background(), file)
		if err == nil || errors.Is(err, ingest.ErrUnreadable) {
			t.Errorf("error %v, want error sin ErrUnreadable", err)
		}
	})
}
