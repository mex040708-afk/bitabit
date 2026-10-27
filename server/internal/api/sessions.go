package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mex040708-afk/bitabit/server/internal/auth"
	"github.com/mex040708-afk/bitabit/server/internal/limits"
)

// idPattern es el formato de los IDs del contrato: enteros de 1 a 19 cifras, sin
// ceros a la izquierda.
var idPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

// pathID lee un ID de la ruta. Si no tiene el formato del contrato, ok es false.
func pathID(r *http.Request, name string) (id int64, ok bool) {
	raw := r.PathValue(name)
	if !idPattern.MatchString(raw) {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil
}

// authed exige un token de sesión válido y pasa a next el id de su sesión. Sin un
// token que valga, responde 401 con la cabecera WWW-Authenticate.
func (s *server) authed(next func(w http.ResponseWriter, r *http.Request, session int64)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		var session int64
		var err error
		if ok {
			session, err = s.auth.Authenticate(r.Context(), token)
		}
		switch {
		case !ok || errors.Is(err, auth.ErrUnauthorized):
			w.Header().Set("WWW-Authenticate", "Bearer")
			s.writeError(w, http.StatusUnauthorized, codeUnauthorized, "Falta el token o ya no vale.")
		case err != nil:
			s.internal(w, r, err)
		default:
			next(w, r, session)
		}
	}
}

type infoBody struct {
	Service       string `json:"service"`
	APIVersion    string `json:"apiVersion"`
	ServerVersion string `json:"serverVersion"`
}

func (s *server) info(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, infoBody{Service: "bitabit", APIVersion: APIVersion, ServerVersion: s.version})
}

type loginRequest struct {
	Password string `json:"password"`
	Device   string `json:"device"`
}

type sessionBody struct {
	ID         int64     `json:"id"`
	Device     string    `json:"device"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
	Current    bool      `json:"current"`
}

type loginBody struct {
	Token   string      `json:"token"`
	Session sessionBody `json:"session"`
}

type sessionListBody struct {
	Items []sessionBody `json:"items"`
}

// toSession pasa una sesión a su forma JSON, con las fechas en UTC.
func toSession(sess auth.Session, current bool) sessionBody {
	return sessionBody{
		ID:         sess.ID,
		Device:     sess.Device,
		CreatedAt:  sess.CreatedAt.UTC(),
		LastUsedAt: sess.LastUsedAt.UTC(),
		Current:    current,
	}
}

// login abre una sesión con la contraseña. Los fallos responden como dice el
// contrato: 401 con los intentos que quedan, y 429 cuando la IP se bloquea.
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeBody(w, r, &req); err != nil {
		s.badRequest(w, "El cuerpo no tiene el formato del contrato.")
		return
	}
	if n := utf8.RuneCountInString(req.Password); n < 1 || n > 256 {
		s.badRequest(w, "La contraseña tiene de 1 a 256 caracteres.")
		return
	}
	token, sess, err := s.auth.Login(r.Context(), clientIP(r), req.Password, strings.TrimSpace(req.Device))
	var wrong *auth.WrongPasswordError
	var blocked *auth.BlockedError
	switch {
	case err == nil:
		s.writeJSON(w, http.StatusCreated, loginBody{Token: token, Session: toSession(sess, true)})
	case errors.As(err, &wrong):
		s.writeJSON(w, http.StatusUnauthorized, wrongPasswordBody{
			errorBody:    errorBody{Error: codeWrongPassword, Message: "Contraseña equivocada."},
			AttemptsLeft: wrong.AttemptsLeft,
		})
	case errors.As(err, &blocked):
		secs := retrySeconds(blocked.RetryAfter)
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		s.writeJSON(w, http.StatusTooManyRequests, blockedBody{
			errorBody:  errorBody{Error: codeBlocked, Message: "Demasiados fallos desde esta IP."},
			RetryAfter: secs,
		})
	case errors.Is(err, auth.ErrBusy):
		limits.Busy(w, limits.RetryAfterSeconds(auth.VerifyRetryAfter))
	case errors.Is(err, auth.ErrInvalidDevice):
		s.badRequest(w, "El nombre del dispositivo tiene de 1 a 64 caracteres.")
	default:
		s.internal(w, r, fmt.Errorf("iniciar sesión: %w", err))
	}
}

// clientIP es la IP de la conexión. No se lee ninguna cabecera de relé: eso se
// decide al desplegar (ADR-014).
func clientIP(r *http.Request) netip.Addr {
	addr, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Addr()
}

func (s *server) listSessions(w http.ResponseWriter, r *http.Request, current int64) {
	list, err := s.auth.Sessions(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	items := make([]sessionBody, 0, len(list))
	for _, sess := range list {
		items = append(items, toSession(sess, sess.ID == current))
	}
	s.writeJSON(w, http.StatusOK, sessionListBody{Items: items})
}

// closeSession cierra una sesión. Cerrar la propia es cerrar sesión: su token deja
// de valer en la siguiente petición.
func (s *server) closeSession(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r, "sessionId")
	if !ok {
		s.badRequest(w, "El id de la sesión no es válido.")
		return
	}
	closed, err := s.auth.CloseSession(r.Context(), id)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !closed {
		s.writeError(w, http.StatusNotFound, codeNotFound, "No existe.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
