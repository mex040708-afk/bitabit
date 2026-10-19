// Package ingest importa la música que se copia a la carpeta de entrada.
package ingest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"
)

// audioExts son las extensiones que se tienen en cuenta. Qué es cada archivo lo
// decide después su códec, no la extensión.
var audioExts = map[string]bool{
	".flac": true,
	".wav":  true,
	".aif":  true,
	".aiff": true,
	".mp3":  true,
	".m4a":  true,
	".ogg":  true,
	".opus": true,
}

// Candidate es un archivo de audio de la entrada que ya terminó de copiarse.
type Candidate struct {
	// Path es relativa a la entrada y usa '/' en cualquier sistema.
	Path string
	Size int64
}

type observation struct {
	size    int64
	modTime time.Time
	since   time.Time // primer sondeo en que se vio con este tamaño y fecha
}

// Inbox vigila la carpeta de entrada por sondeo. No admite llamadas
// simultáneas desde varias goroutines.
type Inbox struct {
	fsys  fs.FS
	quiet time.Duration
	seen  map[string]observation
}

// NewInbox vigila dir. Un archivo está listo cuando lleva al menos quiet sin
// cambiar de tamaño ni de fecha de modificación: así no se importa a medias
// mientras se copia.
func NewInbox(dir string, quiet time.Duration) *Inbox {
	return &Inbox{fsys: os.DirFS(dir), quiet: quiet, seen: map[string]observation{}}
}

// Scan recorre la entrada y devuelve los archivos de audio listos, en orden
// alfabético. Ignora los ocultos y los enlaces simbólicos. Olvida los que ya no
// están, para que si vuelven a aparecer el plazo empiece de cero.
func (in *Inbox) Scan(now time.Time) ([]Candidate, error) {
	seen := make(map[string]observation, len(in.seen))
	var ready []Candidate
	err := fs.WalkDir(in.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// Una carpeta que se mueve mientras se recorre no es un fallo.
			if p != "." && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if p == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !audioExts[strings.ToLower(path.Ext(p))] {
			return nil
		}

		info, err := d.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		obs := observation{size: info.Size(), modTime: info.ModTime(), since: now}
		if prev, ok := in.seen[p]; ok && prev.size == obs.size && prev.modTime.Equal(obs.modTime) {
			obs.since = prev.since
		}
		seen[p] = obs
		if now.Sub(obs.since) >= in.quiet {
			ready = append(ready, Candidate{Path: p, Size: obs.size})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("recorrer la entrada: %w", err)
	}
	in.seen = seen
	return ready, nil
}
