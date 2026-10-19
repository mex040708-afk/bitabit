package ingest

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	encodeTimeout = 10 * time.Minute
	maxAACRate    = 48000
	// aacDurationSlack es lo que puede diferir la duración del AAC: el
	// codificador añade relleno al principio y al final.
	aacDurationSlack = 200 * time.Millisecond
	maxExtLen        = 8
)

// Versions son los archivos que BuildVersions deja en la carpeta de trabajo.
type Versions struct {
	Original    string // ruta de original.{ext}
	OriginalExt string // "flac", o la extensión de la fuente con pérdida
	AudioMD5    string // MD5 de STREAMINFO en hexadecimal; "" si no consta
	AAC         string // ruta de aac.m4a; "" si la fuente tiene pérdida
	AACInfo     Info   // lo que Probe dijo del AAC; vacío si no hay AAC
}

// BuildVersions prepara en workDir el original y, si la fuente no tiene
// pérdida, el AAC 256. info es lo que Probe dijo de src. Los bits del audio
// original nunca cambian: un FLAC se copia tal cual, y lo que se comprime a
// FLAC se compara por MD5 con la fuente. Los errores envuelven ErrUnreadable
// cuando el problema es el archivo.
func BuildVersions(ctx context.Context, src string, info Info, workDir string) (Versions, error) {
	if !filepath.IsAbs(src) || !filepath.IsAbs(workDir) {
		return Versions{}, fmt.Errorf("versiones: las rutas deben ser absolutas: %q, %q", src, workDir)
	}
	// Sin esta comprobación, ffmpeg y flac fallarían al escribir y el archivo
	// acabaría en fallidos/ por un problema del sistema.
	if fi, err := os.Stat(workDir); err != nil {
		return Versions{}, fmt.Errorf("versiones: %w", err)
	} else if !fi.IsDir() {
		return Versions{}, fmt.Errorf("versiones: %q no es una carpeta", workDir)
	}

	var v Versions
	var err error
	switch {
	case !info.Lossless:
		v, err = copyLossy(src, workDir)
	case info.Codec == "flac":
		v, err = copyFLAC(ctx, src, workDir)
	default:
		v, err = encodeFLAC(ctx, src, info, workDir)
	}
	if err != nil {
		return Versions{}, err
	}
	if info.Lossless {
		if v.AAC, v.AACInfo, err = encodeAAC(ctx, v.Original, info, workDir); err != nil {
			return Versions{}, err
		}
	}
	return v, nil
}

// copyLossy guarda la fuente con pérdida tal cual: recodificarla solo la
// empeoraría.
func copyLossy(src, workDir string) (Versions, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(src), "."))
	if !isLowerAlnum(ext, maxExtLen) {
		return Versions{}, fmt.Errorf("%w: extensión no válida %q", ErrUnreadable, ext)
	}
	dst := filepath.Join(workDir, "original."+ext)
	if err := copyFile(src, dst); err != nil {
		return Versions{}, err
	}
	return Versions{Original: dst, OriginalExt: ext}, nil
}

// copyFLAC copia un FLAC byte a byte y solo le añade la tabla de saltos, que es
// un bloque de metadatos: el audio no se toca.
func copyFLAC(ctx context.Context, src, workDir string) (Versions, error) {
	magic, err := readMagic(src)
	if err != nil {
		return Versions{}, err
	}
	if magic != "fLaC" {
		return Versions{}, fmt.Errorf("%w: el FLAC no empieza por fLaC (¿va dentro de Ogg o con ID3 delante?) y un FLAC no se recodifica", ErrUnreadable)
	}
	dst := filepath.Join(workDir, "original.flac")
	if err := copyFile(src, dst); err != nil {
		return Versions{}, err
	}
	if _, err := runTool(ctx, encodeTimeout, maxToolOutput, "metaflac", "--add-seekpoint=10s", dst); err != nil {
		return Versions{}, err
	}
	if _, err := runTool(ctx, encodeTimeout, maxToolOutput, "flac", "-t", "--silent", dst); err != nil {
		return Versions{}, err
	}
	sum, err := streamMD5(ctx, dst)
	if err != nil {
		return Versions{}, err
	}
	return Versions{Original: dst, OriginalExt: "flac", AudioMD5: sum}, nil
}

// encodeFLAC comprime a FLAC un WAV, AIFF o ALAC pasando por un WAV con los
// mismos bits, y exige que el MD5 del FLAC sea el del PCM de la fuente.
func encodeFLAC(ctx context.Context, src string, info Info, workDir string) (Versions, error) {
	pcm, err := pcmCodec(info.Bits)
	if err != nil {
		return Versions{}, err
	}
	wav := filepath.Join(workDir, "fuente.wav")
	args := append([]string{"-nostdin", "-v", "error", "-y"}, ffmpegInput(src)...)
	// Sin metadatos ni marcas del codificador, para que flac no tropiece con
	// bloques que no conoce. rf64 por si pasa de 4 GiB.
	args = append(args, "-map", "0:a:0", "-c:a", pcm, "-map_metadata", "-1", "-fflags", "+bitexact",
		"-rf64", "auto", "-f", "wav", ffmpegPath(wav))
	if _, err := runTool(ctx, encodeTimeout, maxToolOutput, "ffmpeg", args...); err != nil {
		return Versions{}, err
	}

	dst := filepath.Join(workDir, "original.flac")
	if _, err := runTool(ctx, encodeTimeout, maxToolOutput, "flac", "-8", "--verify", "--silent", "-o", dst, wav); err != nil {
		return Versions{}, err
	}
	if err := os.Remove(wav); err != nil {
		return Versions{}, fmt.Errorf("borrar el WAV intermedio: %w", err)
	}

	got, err := streamMD5(ctx, dst)
	if err != nil {
		return Versions{}, err
	}
	want, err := pcmMD5(ctx, src, pcm)
	if err != nil {
		return Versions{}, err
	}
	if got != want {
		return Versions{}, fmt.Errorf("%w: el FLAC no es idéntico a la fuente (MD5 %q, fuente %q)", ErrUnreadable, got, want)
	}
	return Versions{Original: dst, OriginalExt: "flac", AudioMD5: got}, nil
}

// encodeAAC genera la versión para datos móviles a partir del original.
func encodeAAC(ctx context.Context, original string, info Info, workDir string) (string, Info, error) {
	dst := filepath.Join(workDir, "aac.m4a")
	args := append([]string{"-nostdin", "-v", "error", "-y"}, ffmpegInput(original)...)
	args = append(args, "-map", "0:a:0", "-c:a", "aac", "-b:a", "256k")
	if rate := aacSampleRate(info.SampleRate); rate != info.SampleRate {
		args = append(args, "-ar", strconv.Itoa(rate))
	}
	if info.Channels > 2 {
		args = append(args, "-ac", "2")
	}
	args = append(args, "-movflags", "+faststart", "-f", "ipod", ffmpegPath(dst))
	if _, err := runTool(ctx, encodeTimeout, maxToolOutput, "ffmpeg", args...); err != nil {
		return "", Info{}, err
	}

	got, err := Probe(ctx, dst)
	if err != nil {
		return "", Info{}, fmt.Errorf("comprobar el AAC: %w", err)
	}
	if diff := got.Duration - info.Duration; got.Codec != "aac" || diff < -aacDurationSlack || diff > aacDurationSlack {
		return "", Info{}, fmt.Errorf("%w: el AAC salió mal: %s de %v, y la fuente dura %v", ErrUnreadable, got.Codec, got.Duration, info.Duration)
	}
	return dst, got, nil
}

// aacSampleRate deja la frecuencia hasta 48 kHz. Por encima, el móvil no gana
// nada y el codificador AAC de FFmpeg no pasa de 96 kHz: se baja dentro de la
// misma familia (44,1 o 48 kHz).
func aacSampleRate(rate int) int {
	switch {
	case rate <= maxAACRate:
		return rate
	case rate%44100 == 0:
		return 44100
	default:
		return 48000
	}
}

// pcmCodec elige el PCM intermedio con el mismo ancho por muestra con el que
// FLAC calcula su MD5, para que los dos MD5 se puedan comparar.
func pcmCodec(bits int) (string, error) {
	switch {
	case bits >= 1 && bits <= 16:
		return "pcm_s16le", nil
	case bits > 16 && bits <= 24:
		return "pcm_s24le", nil
	case bits > 24 && bits <= 32:
		return "pcm_s32le", nil
	}
	return "", fmt.Errorf("%w: %d bits por muestra", ErrUnreadable, bits)
}

// streamMD5 devuelve el MD5 del audio que guarda STREAMINFO, o "" si el
// codificador no lo escribió (todo ceros).
func streamMD5(ctx context.Context, flacFile string) (string, error) {
	out, err := runTool(ctx, encodeTimeout, maxToolOutput, "metaflac", "--show-md5sum", flacFile)
	if err != nil {
		return "", err
	}
	sum := strings.TrimSpace(string(out))
	if b, err := hex.DecodeString(sum); err != nil || len(b) != 16 {
		return "", fmt.Errorf("%w: metaflac devolvió un MD5 no válido: %q", ErrUnreadable, sum)
	}
	if sum == strings.Repeat("0", 32) {
		return "", nil
	}
	return sum, nil
}

// pcmMD5 calcula con ffmpeg el MD5 del audio de file decodificado a pcm.
func pcmMD5(ctx context.Context, file, pcm string) (string, error) {
	args := append([]string{"-nostdin", "-v", "error"}, ffmpegInput(file)...)
	args = append(args, "-map", "0:a:0", "-c:a", pcm, "-f", "md5", "-")
	out, err := runTool(ctx, encodeTimeout, maxToolOutput, "ffmpeg", args...)
	if err != nil {
		return "", err
	}
	sum, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "MD5=")
	if !ok {
		return "", fmt.Errorf("%w: ffmpeg no devolvió un MD5: %q", ErrUnreadable, firstLine(string(out)))
	}
	return sum, nil
}

func readMagic(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", fmt.Errorf("abrir la fuente: %w", err)
	}
	defer f.Close() // solo lectura: cerrar no puede perder datos
	var b [4]byte
	if _, err := io.ReadFull(f, b[:]); errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("%w: el archivo es demasiado corto", ErrUnreadable)
	} else if err != nil {
		return "", fmt.Errorf("leer la fuente: %w", err)
	}
	return string(b[:]), nil
}

// copyFile copia src en dst, que no debe existir, y lo lleva a disco antes de
// volver: después se renombra y tiene que sobrevivir a un corte de luz.
func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("abrir la fuente: %w", err)
	}
	defer in.Close() // solo lectura: cerrar no puede perder datos
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("crear la copia: %w", err)
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("cerrar la copia: %w", cerr)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copiar: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("llevar la copia a disco: %w", err)
	}
	return nil
}

func isLowerAlnum(s string, maxLen int) bool {
	if len(s) == 0 || len(s) > maxLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
