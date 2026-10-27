// Package api es el HTTP de la API propia v1 (api/openapi.json, ADR-014). Traduce
// cada petición a los paquetes auth, catalog y stream; no guarda nada por su cuenta.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mex040708-afk/bitabit/server/internal/auth"
	"github.com/mex040708-afk/bitabit/server/internal/stream"
)

// APIVersion es la versión del contrato que sirve este código (info.apiVersion).
const APIVersion = "1.1.1"

// requestTimeout acota cada petición: ninguna consulta espera más que esto.
const requestTimeout = 10 * time.Second

// Config son las dependencias de la API.
type Config struct {
	Pool          *pgxpool.Pool
	Auth          *auth.Service
	Signer        *stream.Signer
	Library       *os.Root // biblioteca/: de aquí salen las carátulas
	ServerVersion string   // para depurar: info.serverVersion
	Log           *slog.Logger
}

// server guarda las dependencias de los manejadores.
type server struct {
	pool    *pgxpool.Pool
	auth    *auth.Service
	signer  *stream.Signer
	library *os.Root
	version string
	log     *slog.Logger
}

// New devuelve el manejador de /api/v1. El servidor lo monta bajo /api/.
func New(c Config) http.Handler {
	log := c.Log
	if log == nil {
		log = slog.Default()
	}
	s := &server{
		pool:    c.Pool,
		auth:    c.Auth,
		signer:  c.Signer,
		library: c.Library,
		version: c.ServerVersion,
		log:     log,
	}
	mux := http.NewServeMux()
	mux.Handle("/api/v1/info", byMethod(map[string]http.HandlerFunc{
		http.MethodGet: s.info,
	}))
	mux.Handle("/api/v1/sessions", byMethod(map[string]http.HandlerFunc{
		http.MethodPost: noStore(s.login),
		http.MethodGet:  noStore(s.authed(s.listSessions)),
	}))
	mux.Handle("/api/v1/sessions/{sessionId}", byMethod(map[string]http.HandlerFunc{
		http.MethodDelete: s.authed(s.closeSession),
	}))
	mux.Handle("/api/v1/artists", byMethod(map[string]http.HandlerFunc{
		http.MethodGet: s.authed(s.artists),
	}))
	mux.Handle("/api/v1/artists/{artistId}", byMethod(map[string]http.HandlerFunc{
		http.MethodGet: s.authed(s.artist),
	}))
	mux.Handle("/api/v1/albums", byMethod(map[string]http.HandlerFunc{
		http.MethodGet: s.authed(s.albums),
	}))
	mux.Handle("/api/v1/albums/{albumId}", byMethod(map[string]http.HandlerFunc{
		http.MethodGet: s.authed(s.album),
	}))
	mux.Handle("/api/v1/albums/{albumId}/cover", byMethod(map[string]http.HandlerFunc{
		http.MethodGet: s.authed(s.cover),
	}))
	mux.Handle("/api/v1/tracks/{trackId}", byMethod(map[string]http.HandlerFunc{
		http.MethodGet: s.authed(s.track),
	}))
	mux.Handle("/api/v1/tracks/{trackId}/link", byMethod(map[string]http.HandlerFunc{
		http.MethodGet: noStore(s.authed(s.trackLink)),
	}))
	mux.Handle("/api/v1/search", byMethod(map[string]http.HandlerFunc{
		http.MethodGet: s.authed(s.search),
	}))
	// Lo que no es una ruta de la v1 responde 404 con el error del contrato, no con
	// la página de texto de la biblioteca estándar.
	mux.HandleFunc("/api/", s.notFound)
	return withTimeout(nosniff(mux))
}

// withTimeout acota el tiempo de cada petición, para que una consulta colgada no
// deje la conexión abierta para siempre.
func withTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// nosniff impide que el navegador o el reproductor adivinen otro tipo de contenido.
func nosniff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (s *server) notFound(w http.ResponseWriter, _ *http.Request) {
	s.writeError(w, http.StatusNotFound, codeNotFound, "No existe.")
}
