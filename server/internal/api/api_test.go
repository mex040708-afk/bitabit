package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mex040708-afk/bitabit/server/internal/auth"
	"github.com/mex040708-afk/bitabit/server/internal/catalog"
	"github.com/mex040708-afk/bitabit/server/internal/db"
	"github.com/mex040708-afk/bitabit/server/internal/db/dbtest"
	"github.com/mex040708-afk/bitabit/server/internal/stream"
)

const testPassword = "contraseña de prueba"

// maxFailures son los fallos seguidos que bloquean una IP (ADR-014). El test lo repite
// porque el contrato lo fija, no el paquete auth.
const maxFailures = 10

// coverBytes es una carátula de mentira: el servidor no la abre, solo la sirve.
var coverBytes = []byte("\xff\xd8\xff\xe0carátula de prueba")

// fixture es un servidor de la API sobre una base migrada y con una biblioteca de
// prueba: un artista con dos álbumes, uno con carátula y AAC, y otro sin nada.
type fixture struct {
	t      *testing.T
	h      http.Handler
	pool   *pgxpool.Pool
	auth   *auth.Service
	signer *stream.Signer
	token  string // sesión abierta del test

	artist int64
	albumA int64 // con carátula; su primera pista tiene FLAC y AAC
	albumB int64 // sin carátula; solo FLAC
	trackA int64
	trackB int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := auth.SetPassword(ctx, pool, testPassword); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	signer, err := stream.NewSigner([]stream.Key{{ID: "k1", Secret: bytes.Repeat([]byte{7}, 32)}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	authSvc := auth.NewService(pool)
	f := &fixture{
		t:      t,
		pool:   pool,
		auth:   authSvc,
		signer: signer,
		h: New(Config{
			Pool:          pool,
			Auth:          authSvc,
			Signer:        signer,
			Library:       root,
			ServerVersion: "prueba",
			Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		}),
	}
	f.seed(dir)
	f.token = f.login(testPassword, "Prueba").Token
	return f
}

// seed guarda la biblioteca de prueba por el camino real de importación.
func (f *fixture) seed(dir string) {
	f.t.Helper()
	ctx := context.Background()
	flac := func(n byte) db.TrackFile {
		return db.TrackFile{Quality: db.QualityOriginal, Ext: "flac", Codec: "flac", Lossless: true,
			SampleRate: 44100, Bits: 16, Channels: 2, Bytes: 1000, SHA256: [32]byte{n}}
	}
	aac := db.TrackFile{Quality: db.QualityAAC256, Ext: "m4a", Codec: "aac",
		SampleRate: 44100, Channels: 2, Bytes: 500, SHA256: [32]byte{200}}
	f.trackA = f.insert(1, db.NewTrack{Title: "Uno", Artist: "Grupo", Album: "Disco", ReleaseDate: "1977-05-25",
		TrackNumber: 1, DiscNumber: 1, Duration: 2 * time.Minute}, flac(1), aac)
	f.trackB = f.insert(2, db.NewTrack{Title: "Dos", Artist: "Grupo", Album: "Sin carátula",
		TrackNumber: 1, DiscNumber: 1, Duration: time.Minute}, flac(2))

	if err := f.pool.QueryRow(ctx, "SELECT album_id FROM tracks WHERE id = $1", f.trackA).Scan(&f.albumA); err != nil {
		f.t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, "SELECT album_id FROM tracks WHERE id = $1", f.trackB).Scan(&f.albumB); err != nil {
		f.t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, "SELECT artist_id FROM albums WHERE id = $1", f.albumA).Scan(&f.artist); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, "UPDATE albums SET cover_ext = 'jpg' WHERE id = $1", f.albumA); err != nil {
		f.t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "covers"), 0o755); err != nil {
		f.t.Fatal(err)
	}
	cover := filepath.Join(dir, "covers", fmt.Sprintf("%d.jpg", f.albumA))
	if err := os.WriteFile(cover, coverBytes, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// insert guarda una pista con sus versiones y devuelve su id.
func (f *fixture) insert(n byte, tr db.NewTrack, files ...db.TrackFile) int64 {
	f.t.Helper()
	ctx := context.Background()
	tr.SourceSHA256 = [32]byte{n}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	id, err := db.InsertTrack(ctx, tx, tr, files)
	if err != nil {
		f.t.Fatalf("InsertTrack: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		f.t.Fatal(err)
	}
	return id
}

// req hace una petición a la API y devuelve la respuesta.
func (f *fixture) req(method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range header {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	return rec
}

// bearer son las cabeceras de la sesión del test.
func (f *fixture) bearer() map[string]string {
	return map[string]string{"Authorization": "Bearer " + f.token}
}

// login abre una sesión y devuelve la respuesta ya decodificada.
func (f *fixture) login(password, device string) loginBody {
	f.t.Helper()
	body := fmt.Sprintf(`{"password":%q,"device":%q}`, password, device)
	rec := f.req(http.MethodPost, "/api/v1/sessions", body, nil)
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	var out loginBody
	decode(f.t, rec, &out)
	return out
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("respuesta no es JSON: %v\n%s", err, rec.Body.String())
	}
}

// errorOf decodifica un error de la API y comprueba que trae código y mensaje.
func errorOf(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var e errorBody
	decode(t, rec, &e)
	if e.Error == "" || e.Message == "" {
		t.Errorf("error sin código o sin mensaje: %s", rec.Body.String())
	}
	return e
}

func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status %d, want %d: %s", rec.Code, want, rec.Body.String())
	}
}

func TestInfoNeedsNoSession(t *testing.T) {
	f := newFixture(t)
	rec := f.req(http.MethodGet, "/api/v1/info", "", nil)
	expectStatus(t, rec, http.StatusOK)
	var info infoBody
	decode(t, rec, &info)
	if info.Service != "bitabit" || info.APIVersion != APIVersion || info.ServerVersion != "prueba" {
		t.Errorf("info = %+v", info)
	}
}

func TestLoginOpensASessionThatListsAsCurrent(t *testing.T) {
	f := newFixture(t)
	out := f.login(testPassword, "  Pixel 7a  ")
	if out.Session.Device != "Pixel 7a" || !out.Session.Current {
		t.Errorf("sesión %+v: el nombre debe ir sin espacios y marcarse como actual", out.Session)
	}
	rec := f.req(http.MethodGet, "/api/v1/sessions", "", map[string]string{"Authorization": "Bearer " + out.Token})
	expectStatus(t, rec, http.StatusOK)
	var list sessionListBody
	decode(t, rec, &list)
	if len(list.Items) != 2 {
		t.Fatalf("%d sesiones, want 2", len(list.Items))
	}
	for _, s := range list.Items {
		if s.Current != (s.ID == out.Session.ID) {
			t.Errorf("sesión %d: current = %v", s.ID, s.Current)
		}
	}
}

func TestBadLoginBodiesAreRejected(t *testing.T) {
	f := newFixture(t)
	valid := fmt.Sprintf(`{"password":%q,"device":"Prueba"}`, testPassword)
	cases := map[string]string{
		"campo desconocido":      fmt.Sprintf(`{"password":%q,"device":"Prueba","ip":"x"}`, testPassword),
		"contenido sobrante":     valid + "{}",
		"cuerpo de más de 4 KiB": fmt.Sprintf(`{"password":%q,"device":%q}`, testPassword, strings.Repeat("a", 5000)),
		"dispositivo en blanco":  fmt.Sprintf(`{"password":%q,"device":"   "}`, testPassword),
		"contraseña vacía":       `{"password":"","device":"Prueba"}`,
		"JSON roto":              `{"password":`,
	}
	for name, body := range cases {
		rec := f.req(http.MethodPost, "/api/v1/sessions", body, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
			continue
		}
		if e := errorOf(t, rec); e.Error != codeBadRequest {
			t.Errorf("%s: error %q, want bad_request", name, e.Error)
		}
	}
}

// Los nueve primeros fallos responden 401 con los intentos que quedan.
func TestWrongPasswordReportsAttemptsLeft(t *testing.T) {
	f := newFixture(t)
	body := `{"password":"contraseña equivocada","device":"Prueba"}`
	for want := maxFailures - 1; want >= maxFailures-2; want-- {
		rec := f.req(http.MethodPost, "/api/v1/sessions", body, nil)
		expectStatus(t, rec, http.StatusUnauthorized)
		var e wrongPasswordBody
		decode(t, rec, &e)
		if e.Error != codeWrongPassword || e.AttemptsLeft != want {
			t.Errorf("%+v, want wrong_password con %d intentos", e, want)
		}
	}
}

// El décimo fallo ya responde 429 con Retry-After, y la IP sigue bloqueada aunque
// después acierte la contraseña (ADR-014).
func TestTenthFailureBlocksTheIPWithRetryAfter(t *testing.T) {
	f := newFixture(t)
	body := `{"password":"contraseña equivocada","device":"Prueba"}`
	for i := 1; i < maxFailures; i++ {
		expectStatus(t, f.req(http.MethodPost, "/api/v1/sessions", body, nil), http.StatusUnauthorized)
	}
	rec := f.req(http.MethodPost, "/api/v1/sessions", body, nil)
	expectStatus(t, rec, http.StatusTooManyRequests)
	if got := rec.Header().Get("Retry-After"); got != "86400" {
		t.Errorf("Retry-After = %q, want 86400", got)
	}
	var e blockedBody
	decode(t, rec, &e)
	if e.Error != codeBlocked || e.RetryAfter != 86400 {
		t.Errorf("%+v, want blocked con retryAfter 86400", e)
	}
	ok := fmt.Sprintf(`{"password":%q,"device":"Prueba"}`, testPassword)
	expectStatus(t, f.req(http.MethodPost, "/api/v1/sessions", ok, nil), http.StatusTooManyRequests)
}

func TestMissingOrBrokenTokensAreUnauthorized(t *testing.T) {
	f := newFixture(t)
	for _, header := range []map[string]string{
		nil,
		{"Authorization": "Basic dXN1YXJpbzpjbGF2ZQ=="},
		{"Authorization": "Bearer corto"},
		{"Authorization": "Bearer " + strings.Repeat("A", 43)},
	} {
		rec := f.req(http.MethodGet, "/api/v1/artists", "", header)
		expectStatus(t, rec, http.StatusUnauthorized)
		if rec.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Errorf("sin WWW-Authenticate: %v", header)
		}
		if e := errorOf(t, rec); e.Error != codeUnauthorized {
			t.Errorf("error %q, want unauthorized", e.Error)
		}
	}
}

// Cerrar una sesión deja su token sin valor al momento; cerrar una que no existe, 404.
func TestClosingASessionEndsItsToken(t *testing.T) {
	f := newFixture(t)
	other := f.login(testPassword, "Otro")
	rec := f.req(http.MethodDelete, fmt.Sprintf("/api/v1/sessions/%d", other.Session.ID), "", f.bearer())
	expectStatus(t, rec, http.StatusNoContent)
	expectStatus(t, f.req(http.MethodGet, "/api/v1/artists", "", map[string]string{"Authorization": "Bearer " + other.Token}), http.StatusUnauthorized)

	expectStatus(t, f.req(http.MethodDelete, "/api/v1/sessions/999999", "", f.bearer()), http.StatusNotFound)
	expectStatus(t, f.req(http.MethodDelete, "/api/v1/sessions/abc", "", f.bearer()), http.StatusBadRequest)
}

func TestUnknownRoutesAndMethodsAnswerWithJSON(t *testing.T) {
	f := newFixture(t)
	rec := f.req(http.MethodGet, "/api/v1/no-existe", "", nil)
	expectStatus(t, rec, http.StatusNotFound)
	if e := errorOf(t, rec); e.Error != codeNotFound {
		t.Errorf("error %q, want not_found", e.Error)
	}

	rec = f.req(http.MethodPost, "/api/v1/artists", "", f.bearer())
	expectStatus(t, rec, http.StatusMethodNotAllowed)
	if rec.Header().Get("Allow") != "GET" {
		t.Errorf("Allow = %q, want GET", rec.Header().Get("Allow"))
	}
	if e := errorOf(t, rec); e.Error != codeBadRequest {
		t.Errorf("error %q, want bad_request", e.Error)
	}
}

// El ETag de una biblioteca no cambia hasta que se importa algo: con el mismo ETag
// la respuesta es 304 sin cuerpo; tras una importación, vuelve a salir el contenido.
func TestCatalogETagAnswers304UntilTheLibraryChanges(t *testing.T) {
	f := newFixture(t)
	first := f.req(http.MethodGet, "/api/v1/artists", "", f.bearer())
	expectStatus(t, first, http.StatusOK)
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != cacheRevalidate {
		t.Fatalf("cabeceras de caché: ETag %q, Cache-Control %q", etag, first.Header().Get("Cache-Control"))
	}

	same := f.req(http.MethodGet, "/api/v1/artists", "", map[string]string{"Authorization": "Bearer " + f.token, "If-None-Match": etag})
	expectStatus(t, same, http.StatusNotModified)
	if same.Body.Len() != 0 {
		t.Errorf("304 con cuerpo de %d bytes", same.Body.Len())
	}

	f.insert(9, db.NewTrack{Title: "Nueva", Artist: "Otro", Album: "Nuevo", Duration: time.Minute},
		db.TrackFile{Quality: db.QualityOriginal, Ext: "flac", Codec: "flac", Lossless: true,
			SampleRate: 44100, Bits: 16, Channels: 2, Bytes: 1000, SHA256: [32]byte{9}})
	after := f.req(http.MethodGet, "/api/v1/artists", "", map[string]string{"Authorization": "Bearer " + f.token, "If-None-Match": etag})
	expectStatus(t, after, http.StatusOK)
	if after.Header().Get("ETag") == etag {
		t.Error("tras importar, el ETag no cambió")
	}
}

func TestCoverIsServedAsStoredOrNotFound(t *testing.T) {
	f := newFixture(t)
	rec := f.req(http.MethodGet, fmt.Sprintf("/api/v1/albums/%d/cover", f.albumA), "", f.bearer())
	expectStatus(t, rec, http.StatusOK)
	if rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("Content-Type %q, want image/jpeg", rec.Header().Get("Content-Type"))
	}
	if !bytes.Equal(rec.Body.Bytes(), coverBytes) {
		t.Errorf("la carátula no es la guardada")
	}
	etag := rec.Header().Get("ETag")
	expectStatus(t, f.req(http.MethodGet, fmt.Sprintf("/api/v1/albums/%d/cover", f.albumA), "",
		map[string]string{"Authorization": "Bearer " + f.token, "If-None-Match": etag}), http.StatusNotModified)

	missing := f.req(http.MethodGet, fmt.Sprintf("/api/v1/albums/%d/cover", f.albumB), "", f.bearer())
	expectStatus(t, missing, http.StatusNotFound)
}

// El enlace es relativo y el propio servidor de audio lo acepta.
func TestTrackLinkIsAPathTheMediaEndpointAccepts(t *testing.T) {
	f := newFixture(t)
	rec := f.req(http.MethodGet, fmt.Sprintf("/api/v1/tracks/%d/link?quality=orig", f.trackA), "", f.bearer())
	expectStatus(t, rec, http.StatusOK)
	if rec.Header().Get("Cache-Control") != cacheNoStore {
		t.Errorf("Cache-Control %q, want no-store", rec.Header().Get("Cache-Control"))
	}
	var link linkBody
	decode(t, rec, &link)
	u, err := url.Parse(link.Path)
	if err != nil || !strings.HasPrefix(u.Path, "/media/") {
		t.Fatalf("path %q no es relativo a /media/", link.Path)
	}
	name := strings.TrimPrefix(u.Path, "/media/")
	if _, err := f.signer.Verify(name, u.RawQuery, time.Now()); err != nil {
		t.Errorf("la firma del enlace no vale: %v", err)
	}
	if left := time.Until(link.ExpiresAt); left < 11*time.Hour || left > 12*time.Hour+time.Minute {
		t.Errorf("el enlace caduca en %v, want unas 12 horas", left)
	}
}

func TestTrackLinkRefusesVersionsTheTrackDoesNotHave(t *testing.T) {
	f := newFixture(t)
	// La pista B solo tiene FLAC: no hay versión AAC.
	expectStatus(t, f.req(http.MethodGet, fmt.Sprintf("/api/v1/tracks/%d/link?quality=aac256", f.trackB), "", f.bearer()), http.StatusNotFound)
	expectStatus(t, f.req(http.MethodGet, fmt.Sprintf("/api/v1/tracks/%d/link", f.trackA), "", f.bearer()), http.StatusBadRequest)
	expectStatus(t, f.req(http.MethodGet, fmt.Sprintf("/api/v1/tracks/%d/link?quality=hires", f.trackA), "", f.bearer()), http.StatusBadRequest)
	expectStatus(t, f.req(http.MethodGet, "/api/v1/tracks/999999/link?quality=orig", "", f.bearer()), http.StatusNotFound)
}

func TestSearchChecksItsParameters(t *testing.T) {
	f := newFixture(t)
	for _, target := range []string{
		"/api/v1/search",
		"/api/v1/search?q=",
		"/api/v1/search?q=%20%20",
		"/api/v1/search?q=" + strings.Repeat("a", 101),
		"/api/v1/search?q=a&limit=0",
		"/api/v1/search?q=a&limit=51",
		"/api/v1/search?q=a&limit=abc",
	} {
		expectStatus(t, f.req(http.MethodGet, target, "", f.bearer()), http.StatusBadRequest)
	}
	rec := f.req(http.MethodGet, "/api/v1/search?q=%20grupo%20&limit=1", "", f.bearer())
	expectStatus(t, rec, http.StatusOK)
	var res catalog.SearchResult
	decode(t, rec, &res)
	if len(res.Artists) != 1 || res.Artists[0].Name != "Grupo" {
		t.Errorf("artistas %+v, want [Grupo]", res.Artists)
	}
	if res.Albums == nil || res.Tracks == nil {
		t.Error("las listas vacías salen como null, no como []")
	}
}
