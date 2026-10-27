package main

import (
	"net/http"
	"strings"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/limits"
)

// Límites de carga (ADR-040). Se cuentan peticiones en curso, no conexiones: una
// descarga de audio ocupa su plaza mientras dure.
const (
	// maxMediaRequests: 6 dispositivos como mucho, con unas 4 conexiones cada uno al
	// saltar o precargar.
	maxMediaRequests = 24
	// maxOtherRequests: lo demás (/api y /rest), aparte para que un pico de la API no
	// corte la música ni al revés.
	maxOtherRequests = 64
	// gateWait es lo que espera una petición con todo ocupado antes de recibir 503.
	gateWait = 3 * time.Second
	// gateRetryAfter es el Retry-After de ese 503.
	gateRetryAfter = 5 * time.Second

	// Ritmo por IP de las peticiones sin sesión: 5 por segundo, ráfaga de 20.
	unauthenticatedRate  = 5.0
	unauthenticatedBurst = 20
	// maxTrackedIPs acota la tabla del limitador de ritmo.
	maxTrackedIPs = 10_000
)

// loadLimits son los topes y el limitador de un servidor.
type loadLimits struct {
	media, other *limits.Gate
	rate         *limits.IPLimiter
}

func newLoadLimits() *loadLimits {
	return &loadLimits{
		media: limits.NewGate(maxMediaRequests),
		other: limits.NewGate(maxOtherRequests),
		rate:  limits.NewIPLimiter(unauthenticatedRate, unauthenticatedBurst, maxTrackedIPs),
	}
}

// mediaHandler pone el tope de audio.
func (l *loadLimits) mediaHandler(h http.Handler) http.Handler {
	return limits.Middleware(l.media, gateWait, gateRetryAfter)(h)
}

// apiHandler pone el límite de ritmo a lo que no lleva sesión y, después, el tope
// del resto. El ritmo va primero: rechazar es barato y no gasta plaza.
func (l *loadLimits) apiHandler(h http.Handler) http.Handler {
	gated := limits.Middleware(l.other, gateWait, gateRetryAfter)(h)
	return limits.RateLimit(l.rate, withoutSession, limits.RemoteIP)(gated)
}

// withoutSession dice si la petición no trae una sesión: el inicio de sesión, la
// información pública y lo de /rest que se autentica con la contraseña en vez de
// con apiKey. Son las que un atacante sin credenciales puede repetir.
func withoutSession(r *http.Request) bool {
	switch {
	case r.URL.Path == "/api/v1/sessions" && r.Method == http.MethodPost:
		return true
	case r.URL.Path == "/api/v1/info":
		return true
	case strings.HasPrefix(r.URL.Path, "/rest/"):
		return !r.URL.Query().Has("apiKey")
	}
	return false
}
