package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mex040708-afk/bitabit/server/internal/db"
)

const (
	// Una carátula incrustada suele pesar de 2 a 10 MB. Lo que pase de 32 MiB se
	// trata como ilegible (ADR-015), y el plazo acota a ffmpeg como a los demás.
	coverTimeout  = 30 * time.Second
	maxCoverBytes = 32 << 20
)

var (
	jpegSignature = []byte{0xFF, 0xD8, 0xFF}
	pngSignature  = []byte("\x89PNG\r\n\x1a\n")
)

// Cover es una carátula ya sacada de la fuente. Path es su archivo en la carpeta
// de trabajo y Ext su formato, jpg o png. Con Path vacío no hay carátula.
type Cover struct {
	Path string
	Ext  string
}

// stageCover deja la carátula de src en la carpeta de trabajo, lista para
// colocarla con el ID del álbum. Es opcional: si la imagen no vale, se avisa y la
// pista se importa sin ella. Los errores del sistema sí paran la importación.
func (im *Importer) stageCover(ctx context.Context, rel, src, work string) (Cover, error) {
	img, ext, err := readCover(ctx, src)
	if errors.Is(err, ErrUnreadable) {
		im.log.Warn("pista sin carátula: la imagen no se puede usar", "archivo", rel, "motivo", err)
		return Cover{}, nil
	}
	if err != nil {
		return Cover{}, err
	}
	if ext == "" {
		return Cover{}, nil
	}
	path := filepath.Join(work, "cover."+ext)
	if err := os.WriteFile(path, img, filePerm); err != nil {
		return Cover{}, fmt.Errorf("guardar la carátula en la carpeta de trabajo: %w", err)
	}
	return Cover{Path: path, Ext: ext}, nil
}

// placeCover coloca la carátula como carátula del álbum de la pista, si el álbum
// no tiene una, y la marca en la base de datos dentro de la misma transacción.
// Devuelve placed con lo que llegó a colocar, para que quien llama lo retire si
// algo falla después.
func (im *Importer) placeCover(ctx context.Context, tx pgx.Tx, trackID int64, c Cover, placed []string) ([]string, error) {
	if c.Path == "" {
		return placed, nil
	}
	albumID, hasCover, err := db.AlbumOfTrack(ctx, tx, trackID)
	if err != nil {
		return placed, err
	}
	if hasCover {
		return placed, nil
	}
	moved, err := placeAll([][2]string{{c.Path, im.coverPath(albumID, c.Ext)}})
	placed = append(placed, moved...)
	if err != nil {
		return placed, err
	}
	return placed, db.SetAlbumCover(ctx, tx, albumID, c.Ext)
}

// readCover saca de file la primera imagen incrustada que sea JPEG o PNG (ADR-015),
// tal cual, y devuelve sus bytes y su extensión. Sin imágenes, devuelve nil y una
// extensión vacía, sin error. Si hay imágenes pero ninguna sirve, el error envuelve
// ErrUnreadable.
func readCover(ctx context.Context, file string) ([]byte, string, error) {
	streams, err := pictureStreams(ctx, file)
	if err != nil || len(streams) == 0 {
		return nil, "", err
	}
	var reason error
	for _, index := range streams {
		img, err := runTool(ctx, coverTimeout, maxCoverBytes, "ffmpeg", coverArgs(file, index)...)
		if errors.Is(err, ErrUnreadable) {
			reason = err
			continue
		}
		if err != nil {
			return nil, "", err
		}
		if ext := imageExt(img); ext != "" {
			return img, ext, nil
		}
		reason = fmt.Errorf("%w: la imagen incrustada no es JPEG ni PNG", ErrUnreadable)
	}
	return nil, "", reason
}

// pictureStreams devuelve los índices de los streams que ffprobe marca como imagen
// incrustada (attached_pic). Un vídeo de verdad no cuenta.
func pictureStreams(ctx context.Context, file string) ([]int, error) {
	args := []string{
		"-v", "error",
		"-select_streams", "v",
		"-show_entries", "stream=index:stream_disposition=attached_pic",
		"-of", "json",
	}
	out, err := runTool(ctx, probeTimeout, maxProbeOutput, "ffprobe", append(args, ffmpegInput(file)...)...)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Streams []struct {
			Index       int `json:"index"`
			Disposition struct {
				AttachedPic int `json:"attached_pic"`
			} `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("%w: salida de ffprobe ilegible: %w", ErrUnreadable, err)
	}
	var index []int
	for _, s := range parsed.Streams {
		if s.Disposition.AttachedPic == 1 {
			index = append(index, s.Index)
		}
	}
	return index, nil
}

// coverArgs son los argumentos de ffmpeg que escriben el stream index de file en la
// salida estándar sin recodificar: así los bytes de la imagen son los del archivo.
func coverArgs(file string, index int) []string {
	args := []string{"-v", "error", "-nostdin"}
	args = append(args, ffmpegInput(file)...)
	return append(args, "-map", "0:"+strconv.Itoa(index), "-c", "copy", "-f", "image2pipe", "pipe:1")
}

// imageExt dice la extensión de data si es JPEG o PNG, mirando sus primeros bytes.
// Ni la extensión del archivo ni el códec que declara la fuente son de fiar.
func imageExt(data []byte) string {
	switch {
	case bytes.HasPrefix(data, jpegSignature):
		return "jpg"
	case bytes.HasPrefix(data, pngSignature):
		return "png"
	}
	return ""
}
