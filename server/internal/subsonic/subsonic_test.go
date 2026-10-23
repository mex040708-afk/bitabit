package subsonic

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/mex040708-afk/bitabit/server/internal/auth"
)

// fakeAuth acepta la clave "bueno" y la contraseña "contraseña correcta".
type fakeAuth struct {
	blocked bool
	checks  int
}

func (f *fakeAuth) Authenticate(_ context.Context, token string) (int64, error) {
	if token == "bueno" {
		return 1, nil
	}
	return 0, auth.ErrUnauthorized
}

func (f *fakeAuth) CheckPassword(_ context.Context, _ netip.Addr, password string) error {
	f.checks++
	switch {
	case f.blocked:
		return &auth.BlockedError{}
	case password != "contraseña correcta":
		return &auth.WrongPasswordError{AttemptsLeft: 3}
	}
	return nil
}

func get(t *testing.T, h http.Handler, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if method == http.MethodPost {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path+"?"+form.Encode(), nil)
	}
	req.RemoteAddr = "192.0.2.1:5000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newHandler(f *fakeAuth) http.Handler {
	return New(Config{Auth: f, ServerVersion: "prueba", Log: quietLog()})
}

// jsonBody lee el contenido de "subsonic-response".
func jsonBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("JSON no válido: %v\n%s", err, rec.Body)
	}
	return out["subsonic-response"]
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	body := jsonBody(t, rec)
	if body["status"] != "failed" {
		t.Fatalf("status = %v, want failed", body["status"])
	}
	return int(body["error"].(map[string]any)["code"].(float64))
}

func TestPingAcceptsAPIKeyAndPassword(t *testing.T) {
	h := newHandler(&fakeAuth{})
	forms := map[string]url.Values{
		"apiKey":            {"apiKey": {"bueno"}, "f": {"json"}},
		"contraseña":        {"u": {"x"}, "p": {"contraseña correcta"}, "f": {"json"}},
		"contraseña en hex": {"u": {"x"}, "p": {"enc:" + hex.EncodeToString([]byte("contraseña correcta"))}, "f": {"json"}},
	}
	for name, form := range forms {
		for _, path := range []string{"/rest/ping", "/rest/ping.view"} {
			rec := get(t, h, http.MethodGet, path, form)
			body := jsonBody(t, rec)
			if rec.Code != http.StatusOK || body["status"] != "ok" || body["version"] != APIVersion ||
				body["type"] != "bitabit" || body["serverVersion"] != "prueba" || body["openSubsonic"] != true {
				t.Errorf("%s %s: %d %v", name, path, rec.Code, body)
			}
		}
	}
	// POST de formulario, que usan algunos clientes para no poner la contraseña en la URL.
	rec := get(t, h, http.MethodPost, "/rest/ping", url.Values{"u": {"x"}, "p": {"contraseña correcta"}, "f": {"json"}})
	if jsonBody(t, rec)["status"] != "ok" {
		t.Errorf("POST: %s", rec.Body)
	}
}

func TestAuthenticationErrors(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		want int
	}{
		{"sin credenciales", url.Values{"u": {"x"}}, codeMissingParameter},
		{"contraseña mala", url.Values{"u": {"x"}, "p": {"mala"}}, codeWrongCredentials},
		{"hex roto", url.Values{"u": {"x"}, "p": {"enc:zz"}}, codeWrongCredentials},
		{"apiKey mala", url.Values{"apiKey": {"malo"}}, codeInvalidAPIKey},
		{"token con salt", url.Values{"u": {"x"}, "t": {"abc"}, "s": {"sal"}}, codeMechanism},
		{"apiKey y contraseña", url.Values{"apiKey": {"bueno"}, "p": {"contraseña correcta"}}, codeConflicting},
		{"apiKey y usuario", url.Values{"apiKey": {"bueno"}, "u": {"x"}}, codeConflicting},
	}
	for _, c := range cases {
		c.form.Set("f", "json")
		rec := get(t, newHandler(&fakeAuth{}), http.MethodGet, "/rest/ping", c.form)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: HTTP %d, want 200", c.name, rec.Code)
		}
		if got := errCode(t, rec); got != c.want {
			t.Errorf("%s: error %d, want %d", c.name, got, c.want)
		}
	}
	rec := get(t, newHandler(&fakeAuth{blocked: true}), http.MethodGet, "/rest/ping",
		url.Values{"u": {"x"}, "p": {"contraseña correcta"}, "f": {"json"}})
	if errCode(t, rec) != codeWrongCredentials {
		t.Errorf("una IP bloqueada debe dar el error %d", codeWrongCredentials)
	}
}

// El token con salt se rechaza sin llegar a comprobar la contraseña.
func TestSaltedTokenDoesNotCountAsAPasswordAttempt(t *testing.T) {
	f := &fakeAuth{}
	get(t, newHandler(f), http.MethodGet, "/rest/ping", url.Values{"u": {"x"}, "t": {"a"}, "s": {"b"}})
	if f.checks != 0 {
		t.Errorf("se comprobó la contraseña %d veces", f.checks)
	}
}

func TestExtensionsNeedNoAuthentication(t *testing.T) {
	rec := get(t, newHandler(&fakeAuth{}), http.MethodGet, "/rest/getOpenSubsonicExtensions", url.Values{"f": {"json"}})
	body := jsonBody(t, rec)
	list, _ := body["openSubsonicExtensions"].([]any)
	if body["status"] != "ok" || len(list) != 1 {
		t.Fatalf("respuesta inesperada: %v", body)
	}
	ext := list[0].(map[string]any)
	if ext["name"] != "apiKeyAuthentication" || len(ext["versions"].([]any)) != 1 {
		t.Errorf("extensión inesperada: %v", ext)
	}
}

func TestGetLicenseAndUnknownFunction(t *testing.T) {
	h := newHandler(&fakeAuth{})
	creds := url.Values{"apiKey": {"bueno"}, "f": {"json"}}
	body := jsonBody(t, get(t, h, http.MethodGet, "/rest/getLicense", creds))
	if license, _ := body["license"].(map[string]any); license["valid"] != true {
		t.Errorf("licencia inesperada: %v", body)
	}
	if got := errCode(t, get(t, h, http.MethodGet, "/rest/getSong", creds)); got != codeGeneric {
		t.Errorf("función sin implementar: error %d, want %d", got, codeGeneric)
	}
}

// Sin f, la respuesta es XML y lleva el espacio de nombres de Subsonic.
func TestDefaultFormatIsXML(t *testing.T) {
	rec := get(t, newHandler(&fakeAuth{}), http.MethodGet, "/rest/ping", url.Values{"apiKey": {"bueno"}})
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
		t.Errorf("Content-Type %q", ct)
	}
	var got struct {
		XMLName      xml.Name `xml:"http://subsonic.org/restapi subsonic-response"`
		Status       string   `xml:"status,attr"`
		Version      string   `xml:"version,attr"`
		OpenSubsonic bool     `xml:"openSubsonic,attr"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("XML no válido: %v\n%s", err, rec.Body)
	}
	if got.Status != "ok" || got.Version != APIVersion || !got.OpenSubsonic {
		t.Errorf("respuesta inesperada: %+v", got)
	}

	rec = get(t, newHandler(&fakeAuth{}), http.MethodGet, "/rest/getOpenSubsonicExtensions", nil)
	if !strings.Contains(rec.Body.String(), `<openSubsonicExtensions name="apiKeyAuthentication"><versions>1</versions>`) {
		t.Errorf("extensiones en XML: %s", rec.Body)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	rec := get(t, newHandler(&fakeAuth{}), http.MethodDelete, "/rest/ping", url.Values{"apiKey": {"bueno"}})
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE dio %d, want 405", rec.Code)
	}
}
