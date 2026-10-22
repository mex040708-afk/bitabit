package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mex040708-afk/bitabit/server/internal/catalog"
	"github.com/mex040708-afk/bitabit/server/internal/db"
)

// queryInt lee un parámetro entero de la consulta. Sin el parámetro, vale def; con
// un valor que no es un entero entre lo y hi, ok es false.
func queryInt(r *http.Request, name string, def, lo, hi int) (n int, ok bool) {
	vals, present := r.URL.Query()[name]
	if !present {
		return def, true
	}
	n, err := strconv.Atoi(vals[0])
	return n, err == nil && n >= lo && n <= hi
}

// search busca artistas, álbumes y pistas. La consulta va sin los espacios de los
// extremos y tiene de 1 a 100 caracteres; limit va de 1 a 50 por tipo (ADR-014).
func (s *server) search(w http.ResponseWriter, r *http.Request, _ int64) {
	text := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(text); n < 1 || n > 100 {
		s.badRequest(w, "La búsqueda tiene de 1 a 100 caracteres.")
		return
	}
	limit, ok := queryInt(r, "limit", 20, 1, 50)
	if !ok {
		s.badRequest(w, "El límite va de 1 a 50.")
		return
	}
	s.serveCatalog(w, r, func(ctx context.Context, q db.Querier) (reply, error) {
		res, err := catalog.Search(ctx, q, text, limit)
		if err != nil {
			return nil, err
		}
		res.Artists = nonNil(res.Artists)
		res.Albums = nonNil(res.Albums)
		res.Tracks = nonNil(res.Tracks)
		return s.jsonReply(res), nil
	})
}
