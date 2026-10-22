package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mex040708-afk/bitabit/server/internal/catalog"
	"github.com/mex040708-afk/bitabit/server/internal/db"
	"github.com/mex040708-afk/bitabit/server/internal/library"
)

// errNotFound lo devuelven las lecturas cuando el objeto no existe o no tiene lo
// pedido. El contrato responde igual en los dos casos: 404.
var errNotFound = errors.New("no existe")

// reply escribe una respuesta ya leída. Así las lecturas del catálogo comparten el
// camino del ETag y del 304, sea cual sea el formato de la respuesta.
type reply func(w http.ResponseWriter)

type artistListBody struct {
	Items []catalog.ArtistSummary `json:"items"`
}

type albumListBody struct {
	Items []catalog.AlbumSummary `json:"items"`
}

// jsonReply responde con body en JSON.
func (s *server) jsonReply(body any) reply {
	return func(w http.ResponseWriter) {
		s.writeJSON(w, http.StatusOK, body)
	}
}

// serveCatalog responde una lectura del catálogo con su ETag. La versión y los datos
// se leen en la misma transacción de solo lectura: el ETag nunca describe datos de
// otra versión, y un 304 no lee nada más.
func (s *server) serveCatalog(w http.ResponseWriter, r *http.Request, read func(ctx context.Context, q db.Querier) (reply, error)) {
	ctx := r.Context()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		s.internal(w, r, fmt.Errorf("abrir la lectura: %w", err))
		return
	}
	defer s.endSnapshot(tx)

	version, err := catalog.Version(ctx, tx)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	etag := etagOf(version)
	if notModified(r, etag) {
		setCatalogHeaders(w, etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	rep, err := read(ctx, tx)
	switch {
	case errors.Is(err, errNotFound):
		s.writeError(w, http.StatusNotFound, codeNotFound, "No existe.")
	case err != nil:
		s.internal(w, r, err)
	default:
		setCatalogHeaders(w, etag)
		rep(w)
	}
}

// endSnapshot cierra la lectura. Es de solo lectura: no hay nada que confirmar, y un
// fallo al cerrar solo se registra.
func (s *server) endSnapshot(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		s.log.Warn("cerrar la lectura", "error", err)
	}
}

// etagOf es el ETag de una versión de la biblioteca: cambia con cada importación.
func etagOf(version int64) string {
	return fmt.Sprintf(`"v%d"`, version)
}

// notModified dice si If-None-Match trae el ETag de la versión actual.
func notModified(r *http.Request, etag string) bool {
	for _, tag := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		if strings.TrimPrefix(strings.TrimSpace(tag), "W/") == etag {
			return true
		}
	}
	return false
}

func setCatalogHeaders(w http.ResponseWriter, etag string) {
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", cacheRevalidate)
}

func (s *server) artists(w http.ResponseWriter, r *http.Request, _ int64) {
	s.serveCatalog(w, r, func(ctx context.Context, q db.Querier) (reply, error) {
		items, err := catalog.Artists(ctx, q)
		if err != nil {
			return nil, err
		}
		return s.jsonReply(artistListBody{Items: nonNil(items)}), nil
	})
}

func (s *server) artist(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r, "artistId")
	if !ok {
		s.badRequest(w, "El id del artista no es válido.")
		return
	}
	s.serveCatalog(w, r, func(ctx context.Context, q db.Querier) (reply, error) {
		a, found, err := catalog.GetArtist(ctx, q, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errNotFound
		}
		a.Albums = nonNil(a.Albums)
		return s.jsonReply(a), nil
	})
}

func (s *server) albums(w http.ResponseWriter, r *http.Request, _ int64) {
	order := catalog.SortName
	if vals, present := r.URL.Query()["sort"]; present {
		order = vals[0]
	}
	if order != catalog.SortName && order != catalog.SortAdded {
		s.badRequest(w, "El orden debe ser name o added.")
		return
	}
	s.serveCatalog(w, r, func(ctx context.Context, q db.Querier) (reply, error) {
		items, err := catalog.Albums(ctx, q, order)
		if err != nil {
			return nil, err
		}
		return s.jsonReply(albumListBody{Items: nonNil(items)}), nil
	})
}

func (s *server) album(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r, "albumId")
	if !ok {
		s.badRequest(w, "El id del álbum no es válido.")
		return
	}
	s.serveCatalog(w, r, func(ctx context.Context, q db.Querier) (reply, error) {
		a, found, err := catalog.GetAlbum(ctx, q, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errNotFound
		}
		a.Tracks = nonNil(a.Tracks)
		return s.jsonReply(a), nil
	})
}

func (s *server) track(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r, "trackId")
	if !ok {
		s.badRequest(w, "El id de la pista no es válido.")
		return
	}
	s.serveCatalog(w, r, func(ctx context.Context, q db.Querier) (reply, error) {
		t, found, err := catalog.GetTrack(ctx, q, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errNotFound
		}
		return s.jsonReply(t), nil
	})
}

// cover manda la carátula del álbum tal cual. Responde 404 si el álbum no existe o
// no tiene carátula (ADR-014). El archivo sale de biblioteca/, nunca de una ruta que
// venga de la petición. Con size=256 manda la miniatura, o la carátula original si
// el álbum no la tiene (ADR-017).
func (s *server) cover(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r, "albumId")
	if !ok {
		s.badRequest(w, "El id del álbum no es válido.")
		return
	}
	thumb, ok := coverSize(r)
	if !ok {
		s.badRequest(w, "size solo puede valer "+strconv.Itoa(library.ThumbSize)+".")
		return
	}
	s.serveCatalog(w, r, func(ctx context.Context, q db.Querier) (reply, error) {
		ext, has, err := catalog.CoverExt(ctx, q, id)
		if err != nil {
			return nil, err
		}
		if !has {
			return nil, errNotFound
		}
		contentType, err := coverType(ext)
		if err != nil {
			return nil, fmt.Errorf("carátula del álbum %d: %w", id, err)
		}
		var f *os.File
		if thumb {
			f, err = s.library.Open(library.ThumbFile(id))
			if err == nil {
				contentType = "image/jpeg"
			} else if !errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("abrir la miniatura del álbum %d: %w", id, err)
			}
		}
		if f == nil {
			f, err = s.library.Open(library.CoverFile(id, ext))
			if err != nil {
				return nil, fmt.Errorf("abrir la carátula del álbum %d: %w", id, err)
			}
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("leer la carátula del álbum %d: %w", id, err)
		}
		return func(w http.ResponseWriter) {
			defer f.Close()
			w.Header().Set("Content-Type", contentType)
			http.ServeContent(w, r, "", info.ModTime(), f)
		}, nil
	})
}

// coverSize lee el parámetro size de la carátula: ausente es el original y 256 la
// miniatura. Cualquier otro valor no es válido.
func coverSize(r *http.Request) (thumb, ok bool) {
	vals, present := r.URL.Query()["size"]
	if !present {
		return false, true
	}
	return true, len(vals) == 1 && vals[0] == strconv.Itoa(library.ThumbSize)
}

// coverType es el tipo de imagen de una extensión de carátula. La base solo admite
// jpg y png, así que cualquier otra cosa es un dato roto.
func coverType(ext string) (string, error) {
	switch ext {
	case "jpg":
		return "image/jpeg", nil
	case "png":
		return "image/png", nil
	}
	return "", fmt.Errorf("extensión de carátula %q", ext)
}
