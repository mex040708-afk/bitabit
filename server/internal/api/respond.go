package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Códigos de error del contrato (Error.error).
const (
	codeBadRequest    = "bad_request"
	codeUnauthorized  = "unauthorized"
	codeWrongPassword = "wrong_password"
	codeBlocked       = "blocked"
	codeNotFound      = "not_found"
	codeInternal      = "internal"
)

// maxBody es lo máximo que aceptan los cuerpos de la API (ADR-014).
const maxBody = 4 << 10

// Valores de Cache-Control del contrato.
const (
	cacheRevalidate = "private, no-cache"
	cacheNoStore    = "no-store"
)

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

type wrongPasswordBody struct {
	errorBody
	AttemptsLeft int `json:"attemptsLeft"`
}

type blockedBody struct {
	errorBody
	RetryAfter int `json:"retryAfter"`
}

func (s *server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Con la cabecera ya enviada no hay respuesta que corregir: solo queda avisar.
		s.log.Warn("escribir la respuesta", "error", err)
	}
}

func (s *server) writeError(w http.ResponseWriter, status int, code, message string) {
	s.writeJSON(w, status, errorBody{Error: code, Message: message})
}

func (s *server) badRequest(w http.ResponseWriter, message string) {
	s.writeError(w, http.StatusBadRequest, codeBadRequest, message)
}

// internal responde 500 sin detalles y deja el error en el registro.
func (s *server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("error interno", "ruta", r.URL.Path, "error", err)
	s.writeError(w, http.StatusInternalServerError, codeInternal, "Error interno.")
}

// decodeBody lee el cuerpo JSON de la petición. Rechaza los cuerpos de más de 4 KiB,
// los campos que no conoce el contrato y cualquier cosa detrás del objeto.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("sobra contenido después del objeto")
	}
	return nil
}

// bearerToken saca el token de «Authorization: Bearer {token}».
func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return strings.TrimSpace(token), true
}

// retrySeconds convierte lo que falta de un bloqueo en segundos enteros, redondeando
// hacia arriba y sin bajar de 1: así Retry-After nunca dice «ya».
func retrySeconds(d time.Duration) int {
	return max(1, int((d+time.Second-1)/time.Second))
}

// nonNil hace que una lista vacía salga como [] y no como null.
func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// byMethod atiende cada método con su manejador. Un método que la ruta no tiene
// responde 405 con el error del contrato y la cabecera Allow.
func byMethod(handlers map[string]http.HandlerFunc) http.HandlerFunc {
	allow := make([]string, 0, len(handlers))
	for m := range handlers {
		allow = append(allow, m)
	}
	slices.Sort(allow)
	allowed := strings.Join(allow, ", ")
	return func(w http.ResponseWriter, r *http.Request) {
		if h, ok := handlers[r.Method]; ok {
			h(w, r)
			return
		}
		w.Header().Set("Allow", allowed)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(errorBody{Error: codeBadRequest, Message: "Método no permitido."})
	}
}

// noStore marca la respuesta como no guardable en ninguna caché: sesiones, enlaces
// e inicio de sesión llevan datos que no deben quedarse en el dispositivo.
func noStore(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", cacheNoStore)
		h(w, r)
	}
}
