package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/proxyproto"
)

func TestWithoutSession(t *testing.T) {
	cases := []struct {
		method, target string
		want           bool
	}{
		{"POST", "/api/v1/sessions", true},
		{"GET", "/api/v1/sessions", false}, // lista de sesiones: lleva token
		{"GET", "/api/v1/info", true},
		{"GET", "/api/v1/albums", false},
		{"GET", "/rest/ping?u=x&p=clave", true},
		{"GET", "/rest/ping?apiKey=abc", false},
		{"POST", "/rest/getArtists", true}, // las credenciales irían en el cuerpo
		{"GET", "/media/1.flac?q=orig", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.target, nil)
		if got := withoutSession(r); got != c.want {
			t.Errorf("withoutSession(%s %s) = %v, want %v", c.method, c.target, got, c.want)
		}
	}
}

// Pasado el ritmo, la IP sin sesión recibe 429 con Retry-After; una petición con
// apiKey no cuenta, y otra IP tiene su propio cubo.
func TestAPIHandlerRateLimitsRequestsWithoutSession(t *testing.T) {
	l := newLoadLimits()
	h := l.apiHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	do := func(remote, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for i := 0; i < unauthenticatedBurst; i++ {
		if rec := do("192.0.2.1:1000", "/rest/ping?u=x&p=y"); rec.Code != http.StatusOK {
			t.Fatalf("petición %d dentro de la ráfaga: %d", i+1, rec.Code)
		}
	}
	rec := do("192.0.2.1:1000", "/rest/ping?u=x&p=y")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("pasada la ráfaga: %d, Retry-After %q; want 429", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := do("192.0.2.1:1000", "/rest/ping?apiKey=abc"); rec.Code != http.StatusOK {
		t.Errorf("una petición con apiKey no cuenta: %d", rec.Code)
	}
	if rec := do("192.0.2.2:1000", "/rest/ping?u=x&p=y"); rec.Code != http.StatusOK {
		t.Errorf("otra IP tiene su propio cubo: %d", rec.Code)
	}
}

// Con todas las plazas de audio ocupadas, la siguiente espera gateWait y recibe 503;
// el resto de la API sigue atendiendo, porque tiene su propio tope.
func TestMediaAndOtherGatesAreSeparate(t *testing.T) {
	l := newLoadLimits()
	hold := make(chan struct{})
	var started sync.WaitGroup
	started.Add(maxMediaRequests)
	media := l.mediaHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started.Done()
		<-hold
	}))
	var wg sync.WaitGroup
	for i := 0; i < maxMediaRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			media.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/media/1.flac", nil))
		}()
	}
	started.Wait()

	begin := time.Now()
	rec := httptest.NewRecorder()
	media.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/1.flac", nil))
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "5" {
		t.Errorf("audio lleno: %d, Retry-After %q; want 503 y 5", rec.Code, rec.Header().Get("Retry-After"))
	}
	if took := time.Since(begin); took < gateWait-500*time.Millisecond {
		t.Errorf("esperó %v; la espera es %v", took, gateWait)
	}

	other := l.apiHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums", nil)
	req.RemoteAddr = "192.0.2.9:1"
	other.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("con el audio lleno la API debía atender: %d", rec.Code)
	}
	close(hold)
	wg.Wait()
}

// De punta a punta con un servidor HTTP real detrás de una conexión de confianza que
// envía PROXY: cada cliente real tiene su cubo de ritmo, y una conexión que no es de
// confianza no puede falsear su IP con una cabecera PROXY.
func TestRateLimitUsesTheRealIPBehindAProxy(t *testing.T) {
	handler := newLoadLimits().apiHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.RemoteAddr)
	}))
	serveWith := func(trusted string) string {
		nets, err := proxyproto.ParseTrusted(trusted)
		if err != nil {
			t.Fatal(err)
		}
		base, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
		go srv.Serve(proxyproto.NewListener(base, nets))
		t.Cleanup(func() { srv.Close() })
		return base.Addr().String()
	}
	request := func(addr, proxyLine string) (status int, body string) {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(5 * time.Second))
		fmt.Fprintf(c, "%sGET /api/v1/info HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n", proxyLine)
		raw, _ := io.ReadAll(c)
		text := string(raw)
		if !strings.HasPrefix(text, "HTTP/1.1 ") {
			return 0, text
		}
		var code int
		fmt.Sscanf(text[len("HTTP/1.1 "):], "%d", &code)
		_, body, _ = strings.Cut(text, "\r\n\r\n")
		return code, body
	}

	trusted := serveWith("127.0.0.1")
	for i := 0; i < unauthenticatedBurst; i++ {
		if code, body := request(trusted, "PROXY TCP4 203.0.113.7 192.0.2.1 5000 443\r\n"); code != 200 || !strings.HasPrefix(body, "203.0.113.7:") {
			t.Fatalf("petición %d: %d %q; want 200 con la IP real", i+1, code, body)
		}
	}
	if code, _ := request(trusted, "PROXY TCP4 203.0.113.7 192.0.2.1 5000 443\r\n"); code != http.StatusTooManyRequests {
		t.Errorf("el cliente que se pasó del ritmo recibió %d, want 429", code)
	}
	if code, body := request(trusted, "PROXY TCP4 203.0.113.8 192.0.2.1 5000 443\r\n"); code != 200 || !strings.HasPrefix(body, "203.0.113.8:") {
		t.Errorf("otro cliente tras el mismo proxy debe tener su cubo: %d %q", code, body)
	}

	// El bucle local no es de confianza aquí: la cabecera no cuenta, el servidor la
	// toma por una petición mal formada, y la IP que se limita es la de la conexión.
	untrusted := serveWith("10.0.0.0/8")
	code, body := request(untrusted, "PROXY TCP4 203.0.113.7 192.0.2.1 5000 443\r\n")
	if code == 200 && strings.HasPrefix(body, "203.0.113.7:") {
		t.Errorf("una conexión no confiable falseó su IP con PROXY: %d %q", code, body)
	}
	if code != http.StatusBadRequest {
		t.Errorf("una cabecera PROXY de una IP no confiable debe dar 400, dio %d", code)
	}
}
