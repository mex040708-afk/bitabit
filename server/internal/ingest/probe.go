package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	probeTimeout   = 30 * time.Second
	maxProbeOutput = 1 << 20
	maxTagRunes    = 512
	maxTagNumber   = 9999
	// maxDuration descarta duraciones absurdas antes de pasarlas a
	// time.Duration, que se desbordaría.
	maxDuration = 1e6 // segundos, unos 11 días
)

var losslessCodecs = map[string]bool{
	"flac":      true,
	"alac":      true,
	"pcm_s16le": true,
	"pcm_s16be": true,
	"pcm_s24le": true,
	"pcm_s24be": true,
	"pcm_s32le": true,
	"pcm_s32be": true,
}

var lossyCodecs = map[string]bool{
	"mp3":    true,
	"aac":    true,
	"vorbis": true,
	"opus":   true,
}

// Info es lo que ffprobe dice de un archivo de audio.
type Info struct {
	Codec      string // nombre del códec en FFmpeg: flac, alac, pcm_s24le, mp3...
	Lossless   bool
	SampleRate int // Hz
	Bits       int // bits por muestra; 0 si tiene pérdida
	Channels   int
	Duration   time.Duration
	BitRate    int // bit/s del archivo entero; 0 si no consta
	Tags       Tags
}

// Tags son las etiquetas que usa el catálogo. Los números valen 0 si no constan.
type Tags struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Track       int
	TrackTotal  int
	Disc        int
	DiscTotal   int
	Date        string // tal cual: "1977" o "1977-05-25"
}

// Probe lee con ffprobe los datos técnicos y las etiquetas de file, que debe
// ser una ruta absoluta. Los errores envuelven ErrUnreadable cuando el
// problema es el archivo.
func Probe(ctx context.Context, file string) (Info, error) {
	if !filepath.IsAbs(file) {
		return Info{}, fmt.Errorf("probe: la ruta debe ser absoluta: %q", file)
	}
	// Si el archivo desapareció, no es culpa suya: que no acabe en fallidos/.
	if _, err := os.Stat(file); err != nil {
		return Info{}, fmt.Errorf("probe: %w", err)
	}

	args := []string{
		"-v", "error",
		"-select_streams", "a",
		"-show_entries", "stream=codec_name,sample_rate,channels,bits_per_sample,bits_per_raw_sample,duration" +
			":stream_tags:format=duration,bit_rate:format_tags",
		"-of", "json",
	}
	out, err := runTool(ctx, probeTimeout, maxProbeOutput, "ffprobe", append(args, ffmpegInput(file)...)...)
	if err != nil {
		return Info{}, err
	}
	return parseProbe(out)
}

type probeOutput struct {
	Streams []probeStream `json:"streams"`
	Format  struct {
		Duration string            `json:"duration"`
		BitRate  string            `json:"bit_rate"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

type probeStream struct {
	CodecName        string            `json:"codec_name"`
	SampleRate       string            `json:"sample_rate"`
	Channels         int               `json:"channels"`
	BitsPerSample    int               `json:"bits_per_sample"`
	BitsPerRawSample string            `json:"bits_per_raw_sample"`
	Duration         string            `json:"duration"`
	Tags             map[string]string `json:"tags"`
}

func parseProbe(data []byte) (Info, error) {
	var out probeOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return Info{}, fmt.Errorf("%w: salida de ffprobe ilegible: %w", ErrUnreadable, err)
	}
	if len(out.Streams) != 1 {
		return Info{}, fmt.Errorf("%w: tiene %d pistas de audio y debe tener una", ErrUnreadable, len(out.Streams))
	}
	s := out.Streams[0]

	info := Info{
		Codec:      s.CodecName,
		Lossless:   losslessCodecs[s.CodecName],
		SampleRate: atoiOrZero(s.SampleRate),
		Channels:   s.Channels,
		BitRate:    atoiOrZero(out.Format.BitRate),
		Tags:       parseTags(out.Format.Tags, s.Tags),
	}
	if !info.Lossless && !lossyCodecs[s.CodecName] {
		return Info{}, fmt.Errorf("%w: códec no admitido: %q", ErrUnreadable, s.CodecName)
	}
	if info.SampleRate <= 0 {
		return Info{}, fmt.Errorf("%w: falta la frecuencia de muestreo", ErrUnreadable)
	}
	if info.Channels <= 0 {
		return Info{}, fmt.Errorf("%w: faltan los canales", ErrUnreadable)
	}
	info.Duration = parseSeconds(s.Duration)
	if info.Duration == 0 {
		info.Duration = parseSeconds(out.Format.Duration)
	}
	if info.Duration == 0 {
		return Info{}, fmt.Errorf("%w: duración ausente o no válida", ErrUnreadable)
	}
	if info.Lossless {
		// FLAC y ALAC lo dan en bits_per_raw_sample; WAV y AIFF, en bits_per_sample.
		info.Bits = atoiOrZero(s.BitsPerRawSample)
		if info.Bits == 0 {
			info.Bits = s.BitsPerSample
		}
		if info.Bits <= 0 {
			return Info{}, fmt.Errorf("%w: faltan los bits por muestra", ErrUnreadable)
		}
	}
	return info, nil
}

// parseTags lee las etiquetas del contenedor y, si faltan, las de la pista (Ogg
// las guarda ahí). Las claves no distinguen mayúsculas: FFmpeg convierte unas
// (TRACKNUMBER pasa a track) y deja otras como vienen (TITLE, TRACKTOTAL).
func parseTags(sources ...map[string]string) Tags {
	get := func(keys ...string) string {
		for _, src := range sources {
			for _, key := range keys {
				for k, v := range src {
					if strings.EqualFold(k, key) {
						if v := cleanTag(v); v != "" {
							return v
						}
					}
				}
			}
		}
		return ""
	}
	track, trackTotal := parseNumberPair(get("track", "tracknumber"))
	disc, discTotal := parseNumberPair(get("disc", "discnumber"))
	if n := tagNumber(get("tracktotal", "totaltracks")); n != 0 {
		trackTotal = n
	}
	if n := tagNumber(get("disctotal", "totaldiscs")); n != 0 {
		discTotal = n
	}
	return Tags{
		Title:       get("title"),
		Artist:      get("artist"),
		Album:       get("album"),
		AlbumArtist: get("album_artist", "albumartist"),
		Track:       track,
		TrackTotal:  trackTotal,
		Disc:        disc,
		DiscTotal:   discTotal,
		Date:        get("date", "year"),
	}
}

// cleanTag cambia los caracteres de control por espacios, recorta los extremos
// y corta en maxTagRunes. Así ninguna etiqueta lleva NUL (PostgreSQL no lo
// admite) ni saltos de línea a los registros.
func cleanTag(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if r := []rune(s); len(r) > maxTagRunes {
		s = string(r[:maxTagRunes])
	}
	return strings.TrimSpace(s)
}

// parseNumberPair lee "3" o "3/12".
func parseNumberPair(s string) (n, total int) {
	a, b, _ := strings.Cut(s, "/")
	return tagNumber(a), tagNumber(b)
}

func tagNumber(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > maxTagNumber {
		return 0
	}
	return n
}

// atoiOrZero devuelve 0 si s no es un número: ffprobe escribe "N/A" o nada
// cuando no sabe un dato, y 0 significa justo eso.
func atoiOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func parseSeconds(s string) time.Duration {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || f <= 0 || f > maxDuration {
		return 0
	}
	return time.Duration(f * float64(time.Second))
}
