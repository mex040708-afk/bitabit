package subsonic

import (
	"net/http"
	"strconv"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/catalog"
	"github.com/mex040708-afk/bitabit/server/internal/db"
	"github.com/mex040708-afk/bitabit/server/internal/stream"
)

// stream y download no mandan audio ellos: redirigen a un enlace firmado de /media
// (ADR-031), que es quien lo sirve, con Range y sin conversiones. Así el audio sale
// por un solo camino, con sus mismas comprobaciones, y su URL firmada no pasa por
// ningún registro de esta capa.
func (s *server) stream(w http.ResponseWriter, r *http.Request) *response {
	return s.redirectToMedia(w, r, false)
}

func (s *server) download(w http.ResponseWriter, r *http.Request) *response {
	return s.redirectToMedia(w, r, true)
}

// scrobble no guarda nada: no hay historial de escucha. Responde bien para que el
// cliente no lo trate como un fallo.
func (s *server) scrobble(r *http.Request) *response {
	if !r.Form.Has("id") {
		return s.fail(codeMissingParameter, "Falta el parámetro id.")
	}
	return s.ok()
}

func (s *server) redirectToMedia(w http.ResponseWriter, r *http.Request, original bool) *response {
	id, bad := s.idParam(r, "pista")
	if bad != nil {
		return bad
	}
	maxBitRate := 0
	if r.Form.Has("maxBitRate") {
		n, err := strconv.Atoi(r.Form.Get("maxBitRate"))
		if err != nil || n < 0 {
			return s.fail(codeGeneric, "El parámetro maxBitRate no es válido.")
		}
		maxBitRate = n
	}
	track, found, err := catalog.GetTrack(r.Context(), s.pool, id)
	if err != nil {
		return s.internal(r, err)
	}
	if !found {
		return s.fail(codeNotFound, "No existe esa pista.")
	}
	file, ok := chooseFile(track, original || r.Form.Get("format") == "raw", maxBitRate)
	if !ok {
		return s.fail(codeNotFound, "Esa pista no tiene archivos.")
	}
	// La extensión del enlace tiene que ser la guardada, no una deducida del códec.
	stored, found, err := db.FileByTrack(r.Context(), s.pool, id, file.Quality)
	if err != nil {
		return s.internal(r, err)
	}
	if !found {
		return s.fail(codeNotFound, "Esa pista no tiene archivos.")
	}
	path, err := s.signer.URL(stream.Link{TrackID: id, Quality: stream.Quality(file.Quality), Ext: stored.Ext}, time.Now())
	if err != nil {
		return s.internal(r, err)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", path)
	w.WriteHeader(http.StatusFound)
	return nil
}

// chooseFile elige la versión: la original, salvo que el cliente limite la tasa de
// bits por debajo de la suya y exista el AAC 256, que se manda entonces. Nada se
// convierte al vuelo (Mandamientos del proyecto 1 y 2): solo se elige entre las
// versiones que ya existen. Con raw, siempre la original.
func chooseFile(t catalog.Track, raw bool, maxBitRate int) (catalog.TrackFile, bool) {
	var orig, aac *catalog.TrackFile
	for i := range t.Files {
		switch t.Files[i].Quality {
		case db.QualityOriginal:
			orig = &t.Files[i]
		case db.QualityAAC256:
			aac = &t.Files[i]
		}
	}
	switch {
	case orig != nil && (raw || maxBitRate == 0 || aac == nil || bitRate(*orig, t.DurationMS) <= int64(maxBitRate)):
		return *orig, true
	case aac != nil:
		return *aac, true
	case orig != nil:
		return *orig, true
	}
	return catalog.TrackFile{}, false
}

// bitRate es la tasa media en kbit/s de una versión.
func bitRate(f catalog.TrackFile, durationMS int64) int64 {
	if durationMS <= 0 {
		return 0
	}
	return f.Bytes * 8 / durationMS
}
