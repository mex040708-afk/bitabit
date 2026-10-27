package subsonic

import (
	"net/http"
	"net/url"
	"testing"
)

func TestEveryResponseCarriesNosniffAndNoReferrer(t *testing.T) {
	h := newHandler(&fakeAuth{})
	forms := map[string]url.Values{
		"éxito":             {"apiKey": {"bueno"}},
		"sin credenciales":  {},
		"sin autenticación": {},
	}
	paths := map[string]string{"éxito": "/rest/ping", "sin credenciales": "/rest/ping", "sin autenticación": "/rest/getOpenSubsonicExtensions"}
	for name, form := range forms {
		rec := get(t, h, http.MethodGet, paths[name], form)
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q; want nosniff", name, got)
		}
		if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s: Referrer-Policy = %q; want no-referrer", name, got)
		}
	}
}
