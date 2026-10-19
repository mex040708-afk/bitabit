package stream_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/stream"
)

type stored struct {
	data []byte
	info stream.FileInfo
}

type mediaEnv struct {
	mux     *http.ServeMux
	signer  *stream.Signer
	logs    *bytes.Buffer
	library string
	files   map[string]stored // "1/orig" → archivo
}

// newMediaEnv monta una biblioteca de prueba con la pista 1 en FLAC y AAC, y la
// 2 solo en la base de datos (sin archivo).
func newMediaEnv(t *testing.T) *mediaEnv {
	t.Helper()
	e := &mediaEnv{signer: newSigner(t, keyK1), logs: &bytes.Buffer{}, library: t.TempDir(), files: map[string]stored{}}
	e.add(t, "orig/1.flac", "1/orig", "flac", bytes.Repeat([]byte("fLaC-audio-"), 1000))
	e.add(t, "aac/1.m4a", "1/aac256", "m4a", []byte("contenido aac"))
	e.files["2/orig"] = stored{info: stream.FileInfo{Ext: "flac"}}

	root, err := os.OpenRoot(e.library)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	lookup := func(_ context.Context, id int64, q stream.Quality) (stream.FileInfo, bool, error) {
		if id == 99 {
			return stream.FileInfo{}, false, errors.New("la base de datos no responde")
		}
		f, ok := e.files[itoa(id)+"/"+string(q)]
		return f.info, ok, nil
	}
	log := slog.New(slog.NewTextHandler(e.logs, nil))
	e.mux = http.NewServeMux()
	e.mux.Handle("GET /media/{file}", stream.NewHandler(e.signer, lookup, root, log))
	return e
}

func (e *mediaEnv) add(t *testing.T, rel, key, ext string, data []byte) {
	t.Helper()
	p := filepath.Join(e.library, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o640); err != nil {
		t.Fatal(err)
	}
	e.files[key] = stored{data: data, info: stream.FileInfo{Ext: ext, SHA256: sha256.Sum256(data)}}
}

func (e *mediaEnv) url(t *testing.T, id int64, q stream.Quality, ext string, now time.Time) string {
	t.Helper()
	u, err := e.signer.URL(stream.Link{TrackID: id, Quality: q, Ext: ext}, now)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (e *mediaEnv) do(method, target string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// tamper cambia el primer carácter de la firma: sigue bien formada, pero ya no
// es la buena. (El último no sirve: puede dejar bits de relleno que la hacen
// mal formada.)
func tamper(u string) string {
	i := strings.Index(u, "sig=") + len("sig=")
	c := byte('A')
	if u[i] == 'A' {
		c = 'B'
	}
	return u[:i] + string(c) + u[i+1:]
}

func TestMediaServesOriginal(t *testing.T) {
	e := newMediaEnv(t)
	want := e.files["1/orig"]
	rec := e.do("GET", e.url(t, 1, stream.QualityOriginal, "flac", time.Now()), nil)

	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), want.data) {
		t.Fatalf("código %d y %d bytes, want 200 y %d", rec.Code, rec.Body.Len(), len(want.data))
	}
	h := rec.Header()
	wantHeaders := map[string]string{
		"Content-Type":   "audio/flac",
		"Etag":           `"` + hex.EncodeToString(want.info.SHA256[:]) + `"`,
		"Cache-Control":  "private, max-age=43200, immutable",
		"Accept-Ranges":  "bytes",
		"Content-Length": itoa(int64(len(want.data))),
	}
	for k, v := range wantHeaders {
		if got := h.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestMediaServesAAC(t *testing.T) {
	e := newMediaEnv(t)
	rec := e.do("GET", e.url(t, 1, stream.QualityAAC256, "m4a", time.Now()), nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "audio/mp4" ||
		!bytes.Equal(rec.Body.Bytes(), e.files["1/aac256"].data) {
		t.Errorf("código %d, Content-Type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestMediaHeadRangeAndNotModified(t *testing.T) {
	e := newMediaEnv(t)
	u := e.url(t, 1, stream.QualityOriginal, "flac", time.Now())
	data := e.files["1/orig"].data

	head := e.do("HEAD", u, nil)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != itoa(int64(len(data))) {
		t.Errorf("HEAD: código %d, cuerpo %d bytes, Content-Length %q", head.Code, head.Body.Len(), head.Header().Get("Content-Length"))
	}

	part := e.do("GET", u, http.Header{"Range": {"bytes=100-199"}})
	if part.Code != http.StatusPartialContent || !bytes.Equal(part.Body.Bytes(), data[100:200]) ||
		part.Header().Get("Content-Range") != "bytes 100-199/"+itoa(int64(len(data))) {
		t.Errorf("Range: código %d, Content-Range %q", part.Code, part.Header().Get("Content-Range"))
	}

	etag := head.Header().Get("Etag")
	same := e.do("GET", u, http.Header{"If-None-Match": {etag}})
	if same.Code != http.StatusNotModified || same.Body.Len() != 0 {
		t.Errorf("If-None-Match: código %d", same.Code)
	}
}

func TestMediaRejects(t *testing.T) {
	e := newMediaEnv(t)
	now := time.Now()
	good := e.url(t, 1, stream.QualityOriginal, "flac", now)
	tests := []struct {
		desc   string
		method string
		target string
		code   int
	}{
		{"sin firma", "GET", strings.Split(good, "&sig=")[0], http.StatusBadRequest},
		{"firma cambiada", "GET", tamper(good), http.StatusForbidden},
		{"caducado", "GET", e.url(t, 1, stream.QualityOriginal, "flac", now.Add(-13*time.Hour)), http.StatusForbidden},
		{"pista que no existe", "GET", e.url(t, 7, stream.QualityOriginal, "flac", now), http.StatusNotFound},
		{"extensión que no es la guardada", "GET", e.url(t, 1, stream.QualityOriginal, "mp3", now), http.StatusNotFound},
		{"falta el archivo", "GET", e.url(t, 2, stream.QualityOriginal, "flac", now), http.StatusNotFound},
		{"falla la base de datos", "GET", e.url(t, 99, stream.QualityOriginal, "flac", now), http.StatusInternalServerError},
		{"método no permitido", "POST", good, http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			rec := e.do(tt.method, tt.target, nil)
			if rec.Code != tt.code {
				t.Errorf("código %d, want %d", rec.Code, tt.code)
			}
			if strings.Contains(rec.Body.String(), "base de datos") {
				t.Errorf("la respuesta enseña detalles internos: %q", rec.Body.String())
			}
		})
	}

	// Los registros tienen los errores, pero nunca la consulta con la firma.
	logs := e.logs.String()
	if !strings.Contains(logs, "falta el archivo") || !strings.Contains(logs, "no responde") {
		t.Errorf("faltan errores en el registro:\n%s", logs)
	}
	if strings.Contains(logs, "sig=") || strings.Contains(logs, "kid=") {
		t.Errorf("el registro tiene la firma:\n%s", logs)
	}
}

// Un enlace simbólico dentro de la biblioteca no puede sacar un archivo de fuera.
func TestMediaDoesNotFollowSymlinksOutOfLibrary(t *testing.T) {
	e := newMediaEnv(t)
	secret := filepath.Join(t.TempDir(), "secreto.txt")
	if err := os.WriteFile(secret, []byte("contenido secreto"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(e.library, "orig", "3.flac")); err != nil {
		t.Skipf("este sistema no deja crear enlaces simbólicos: %v", err)
	}
	e.files["3/orig"] = stored{info: stream.FileInfo{Ext: "flac"}}

	rec := e.do("GET", e.url(t, 3, stream.QualityOriginal, "flac", time.Now()), nil)
	if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "secreto") {
		t.Errorf("sirvió un archivo de fuera de la biblioteca: código %d", rec.Code)
	}
}
