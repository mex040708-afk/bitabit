package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/db"
	"github.com/mex040708-afk/bitabit/server/internal/stream"
)

// linkBody es un enlace firmado para escuchar una versión de una pista (ADR-014).
type linkBody struct {
	Path      string    `json:"path"`
	Quality   string    `json:"quality"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// trackLink firma un enlace nuevo para la versión pedida. La ruta es relativa: el
// cliente le pone delante el servidor con el que llega a él. Una versión que la
// pista no tiene responde 404.
func (s *server) trackLink(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r, "trackId")
	if !ok {
		s.badRequest(w, "El id de la pista no es válido.")
		return
	}
	quality := stream.Quality(r.URL.Query().Get("quality"))
	if quality != stream.QualityOriginal && quality != stream.QualityAAC256 {
		s.badRequest(w, "La calidad debe ser orig o aac256.")
		return
	}
	ctx := r.Context()
	f, found, err := db.FileByTrack(ctx, s.pool, id, string(quality))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !found {
		s.writeError(w, http.StatusNotFound, codeNotFound, "La pista no existe o no tiene esa versión.")
		return
	}
	now := time.Now()
	path, err := s.signer.URL(stream.Link{TrackID: id, Quality: quality, Ext: f.Ext}, now)
	if err != nil {
		s.internal(w, r, fmt.Errorf("firmar el enlace de la pista %d: %w", id, err))
		return
	}
	// Mismo instante que usa la firma: el enlace caduca justo cuando dice.
	expires := time.Unix(now.Add(stream.LinkTTL).Unix(), 0).UTC()
	s.writeJSON(w, http.StatusOK, linkBody{Path: path, Quality: string(quality), ExpiresAt: expires})
}
