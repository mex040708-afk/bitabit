// Package subsonic es la capa OpenSubsonic de /rest/ (ADR-030): deja que un
// cliente Subsonic existente, el del iPhone, hable con el servidor. Lee los mismos
// datos que /api/v1 y no los duplica. El contrato es api/opensubsonic.md.
package subsonic

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mex040708-afk/bitabit/server/internal/auth"
)

const (
	// APIVersion es la versión de Subsonic que se anuncia; la bandera openSubsonic
	// dice que además se entiende OpenSubsonic.
	APIVersion = "1.16.1"
	serverType = "bitabit"

	// Códigos de error de la especificación. Las respuestas con error salen con
	// HTTP 200, como espera cualquier cliente Subsonic.
	codeGeneric          = 0
	codeMissingParameter = 10
	codeWrongCredentials = 40
	codeMechanism        = 42
	codeConflicting      = 43
	codeInvalidAPIKey    = 44

	maxFormBytes   = 16 << 10
	requestTimeout = 30 * time.Second
)

// Authenticator es lo que la capa necesita de auth.Service.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (int64, error)
	CheckPassword(ctx context.Context, ip netip.Addr, password string) error
}

// Config son las dependencias de la capa.
type Config struct {
	Library       *os.Root      // biblioteca/, de donde salen las carátulas
	Pool          *pgxpool.Pool // el catálogo; sin él solo funcionan las funciones sin datos
	Auth          Authenticator
	ServerVersion string
	Log           *slog.Logger
}

type server struct {
	library *os.Root
	pool    *pgxpool.Pool
	auth    Authenticator
	version string
	log     *slog.Logger
}

// New devuelve el manejador de /rest/. Se monta con mux.Handle("/rest/", …).
func New(c Config) http.Handler {
	s := &server{library: c.Library, pool: c.Pool, auth: c.Auth, version: c.ServerVersion, log: c.Log}
	mux := http.NewServeMux()
	endpoints := []struct {
		name   string
		handle func(http.ResponseWriter, *http.Request) *response
		auth   bool
	}{
		{"ping", plain(func(*http.Request) *response { return s.ok() }), true},
		{"getLicense", plain(s.getLicense), true},
		// Sin autenticación, como dice la especificación.
		{"getOpenSubsonicExtensions", plain(s.getExtensions), false},
		{"getMusicFolders", plain(s.getMusicFolders), true},
		{"getArtists", plain(s.getArtists), true},
		{"getArtist", plain(s.getArtist), true},
		{"getAlbum", plain(s.getAlbum), true},
		{"getSong", plain(s.getSong), true},
		{"getAlbumList2", plain(s.getAlbumList2), true},
		{"search3", plain(s.search3), true},
		{"getCoverArt", s.getCoverArt, true},
	}
	for _, e := range endpoints {
		mux.HandleFunc("/rest/"+e.name, s.endpoint(e.handle, e.auth))
		mux.HandleFunc("/rest/"+e.name+".view", s.endpoint(e.handle, e.auth))
	}
	mux.HandleFunc("/rest/", s.endpoint(plain(func(*http.Request) *response {
		return s.fail(codeGeneric, "Función no implementada.")
	}), true))
	return mux
}

// plain adapta una función que solo devuelve datos.
func plain(f func(*http.Request) *response) func(http.ResponseWriter, *http.Request) *response {
	return func(_ http.ResponseWriter, r *http.Request) *response { return f(r) }
}

// endpoint lee los parámetros, autentica si hace falta y escribe la respuesta en
// el formato pedido. Los parámetros llevan las credenciales: no se registran.
func (s *server) endpoint(handle func(http.ResponseWriter, *http.Request) *response, needsAuth bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		r = r.WithContext(ctx)
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		var resp *response
		if err := r.ParseForm(); err != nil {
			resp = s.fail(codeGeneric, "Parámetros no válidos.")
		} else if needsAuth {
			resp = s.authenticate(r)
		}
		if resp == nil {
			// Un manejador que escribe él mismo la respuesta (una imagen) devuelve nil.
			if resp = handle(w, r); resp == nil {
				return
			}
		}
		s.write(w, r, resp)
	}
}

// authenticate devuelve nil si la petición trae credenciales válidas, o el error
// que hay que responder (ADR-031).
func (s *server) authenticate(r *http.Request) *response {
	q := r.Form
	hasKey, hasPassword, hasToken := q.Has("apiKey"), q.Has("p"), q.Has("t") || q.Has("s")
	mechanisms := 0
	for _, used := range []bool{hasKey, hasPassword, hasToken} {
		if used {
			mechanisms++
		}
	}
	switch {
	case mechanisms == 0:
		return s.fail(codeMissingParameter, "Faltan las credenciales: usa apiKey o u y p.")
	case mechanisms > 1 || (hasKey && q.Has("u")):
		return s.fail(codeConflicting, "Hay más de un mecanismo de autenticación.")
	case hasToken:
		return s.fail(codeMechanism, "El token con salt no se admite: usa apiKey o la contraseña (u y p).")
	case hasKey:
		_, err := s.auth.Authenticate(r.Context(), q.Get("apiKey"))
		switch {
		case errors.Is(err, auth.ErrUnauthorized):
			return s.fail(codeInvalidAPIKey, "La apiKey no vale.")
		case err != nil:
			return s.internal(r, err)
		}
		return nil
	}
	password, err := decodePassword(q.Get("p"))
	if err != nil {
		return s.fail(codeWrongCredentials, "Usuario o contraseña incorrectos.")
	}
	addr, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return s.internal(r, err)
	}
	err = s.auth.CheckPassword(r.Context(), addr.Addr(), password)
	var blocked *auth.BlockedError
	var wrong *auth.WrongPasswordError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &blocked):
		return s.fail(codeWrongCredentials, "Demasiados intentos: esta IP está bloqueada.")
	case errors.As(err, &wrong):
		return s.fail(codeWrongCredentials, "Usuario o contraseña incorrectos.")
	}
	return s.internal(r, err)
}

// decodePassword quita el prefijo enc: de la forma antigua, que manda la contraseña
// en hexadecimal.
func decodePassword(p string) (string, error) {
	hexed, ok := strings.CutPrefix(p, "enc:")
	if !ok {
		return p, nil
	}
	b, err := hex.DecodeString(hexed)
	return string(b), err
}

func (s *server) internal(r *http.Request, err error) *response {
	// Solo la ruta: la query lleva las credenciales.
	s.log.Error("error en /rest", "error", err, "método", r.Method, "ruta", r.URL.Path)
	return s.fail(codeGeneric, "Error interno.")
}

func (s *server) getLicense(*http.Request) *response {
	resp := s.ok()
	resp.License = &license{Valid: true}
	return resp
}

// getExtensions anuncia solo lo que está implementado.
func (s *server) getExtensions(*http.Request) *response {
	resp := s.ok()
	resp.Extensions = []extension{{Name: "apiKeyAuthentication", Versions: []int{1}}}
	return resp
}

// response es la respuesta de cualquier función: los mismos campos salen en JSON,
// dentro de "subsonic-response", y en XML.
type response struct {
	XMLName       xml.Name        `xml:"subsonic-response" json:"-"`
	Xmlns         string          `xml:"xmlns,attr" json:"-"`
	Status        string          `xml:"status,attr" json:"status"`
	Version       string          `xml:"version,attr" json:"version"`
	Type          string          `xml:"type,attr" json:"type"`
	ServerVersion string          `xml:"serverVersion,attr" json:"serverVersion"`
	OpenSubsonic  bool            `xml:"openSubsonic,attr" json:"openSubsonic"`
	Error         *apiError       `xml:"error,omitempty" json:"error,omitempty"`
	License       *license        `xml:"license,omitempty" json:"license,omitempty"`
	Extensions    []extension     `xml:"openSubsonicExtensions,omitempty" json:"openSubsonicExtensions,omitempty"`
	MusicFolders  *musicFolders   `xml:"musicFolders,omitempty" json:"musicFolders,omitempty"`
	Artists       *artistsID3     `xml:"artists,omitempty" json:"artists,omitempty"`
	Artist        *artistID3      `xml:"artist,omitempty" json:"artist,omitempty"`
	Album         *albumWithSongs `xml:"album,omitempty" json:"album,omitempty"`
	Song          *songID3        `xml:"song,omitempty" json:"song,omitempty"`
	AlbumList2    *albumList      `xml:"albumList2,omitempty" json:"albumList2,omitempty"`
	SearchResult3 *searchResult3  `xml:"searchResult3,omitempty" json:"searchResult3,omitempty"`
}

type apiError struct {
	Code    int    `xml:"code,attr" json:"code"`
	Message string `xml:"message,attr" json:"message"`
}

type license struct {
	Valid bool `xml:"valid,attr" json:"valid"`
}

type extension struct {
	Name     string `xml:"name,attr" json:"name"`
	Versions []int  `xml:"versions" json:"versions"`
}

func (s *server) ok() *response {
	return &response{
		Xmlns: "http://subsonic.org/restapi", Status: "ok", Version: APIVersion,
		Type: serverType, ServerVersion: s.version, OpenSubsonic: true,
	}
}

func (s *server) fail(code int, message string) *response {
	resp := s.ok()
	resp.Status = "failed"
	resp.Error = &apiError{Code: code, Message: message}
	return resp
}

// write responde en JSON con f=json y, si no, en XML, el valor por defecto.
func (s *server) write(w http.ResponseWriter, r *http.Request, resp *response) {
	w.Header().Set("Cache-Control", "no-store")
	var (
		body        []byte
		err         error
		contentType string
	)
	if r.Form.Get("f") == "json" {
		contentType = "application/json; charset=utf-8"
		body, err = json.Marshal(map[string]*response{"subsonic-response": resp})
	} else {
		contentType = "application/xml; charset=utf-8"
		body, err = xml.Marshal(resp)
		body = append([]byte(xml.Header), body...)
	}
	if err != nil {
		s.log.Error("escribir la respuesta de /rest", "error", err)
		http.Error(w, "error interno", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if _, err := w.Write(body); err != nil {
		s.log.Warn("enviar la respuesta de /rest", "error", err)
	}
}
