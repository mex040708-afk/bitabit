package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// contract es api/openapi.json leído como JSON genérico. Los tests comprueban que
// cada respuesta del servidor es una de las que el contrato declara.
type contract struct {
	doc map[string]any
}

func loadContract(t *testing.T) contract {
	t.Helper()
	data, err := os.ReadFile("../../../api/openapi.json")
	if err != nil {
		t.Fatalf("leer el contrato: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("el contrato no es JSON: %v", err)
	}
	return contract{doc: doc}
}

// resolve sigue una referencia interna del contrato, como #/components/schemas/Error.
func (c contract) resolve(ref string) map[string]any {
	node := any(c.doc)
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		node = asMap(node)[part]
	}
	return asMap(node)
}

// deref devuelve el objeto al que apunta m, si es una referencia; si no, m.
func (c contract) deref(m map[string]any) map[string]any {
	if ref, ok := m["$ref"].(string); ok {
		return c.resolve(ref)
	}
	return m
}

// checkResponse comprueba que la respuesta es una de las que el contrato declara
// para esa operación: mismo código, cabeceras prometidas y, si hay cuerpo, JSON que
// cumple el esquema.
func (c contract) checkResponse(t *testing.T, method, tmpl string, rec *httptest.ResponseRecorder) {
	t.Helper()
	op := asMap(asMap(asMap(c.doc["paths"])[tmpl])[strings.ToLower(method)])
	if op == nil {
		t.Fatalf("el contrato no tiene %s %s", method, tmpl)
	}
	resp := asMap(asMap(op["responses"])[strconv.Itoa(rec.Code)])
	if resp == nil {
		t.Errorf("%s %s: el contrato no declara la respuesta %d", method, tmpl, rec.Code)
		return
	}
	resp = c.deref(resp)

	for name, raw := range asMap(resp["headers"]) {
		hdr := c.deref(asMap(raw))
		value := rec.Header().Get(name)
		if value == "" {
			t.Errorf("%s %s %d: falta la cabecera %s", method, tmpl, rec.Code, name)
			continue
		}
		schema := asMap(hdr["schema"])
		if want, ok := schema["const"]; ok && value != want {
			t.Errorf("%s %s %d: %s = %q, el contrato pide %q", method, tmpl, rec.Code, name, value, want)
		}
		if schema["type"] == "integer" {
			n, err := strconv.Atoi(value)
			if min, ok := schema["minimum"].(float64); err != nil || (ok && float64(n) < min) {
				t.Errorf("%s %s %d: %s = %q, no es un entero válido", method, tmpl, rec.Code, name, value)
			}
		}
	}

	content := asMap(resp["content"])
	if rec.Body.Len() == 0 {
		if len(content) > 0 && rec.Code != http.StatusNotModified && rec.Code != http.StatusNoContent {
			t.Errorf("%s %s %d: sin cuerpo, pero el contrato declara uno", method, tmpl, rec.Code)
		}
		return
	}
	if len(content) == 0 {
		t.Errorf("%s %s %d: hay cuerpo, pero el contrato no declara ninguno", method, tmpl, rec.Code)
		return
	}
	ct := strings.TrimSpace(strings.Split(rec.Header().Get("Content-Type"), ";")[0])
	media, ok := content[ct].(map[string]any)
	if !ok {
		t.Errorf("%s %s %d: Content-Type %q no está en el contrato", method, tmpl, rec.Code, ct)
		return
	}
	if strings.HasPrefix(ct, "image/") {
		return // la imagen es tal cual; el contrato no describe sus bytes
	}
	var value any
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Errorf("%s %s %d: el cuerpo no es JSON: %v", method, tmpl, rec.Code, err)
		return
	}
	for _, problem := range c.validate(asMap(media["schema"]), value, "$") {
		t.Errorf("%s %s %d: %s", method, tmpl, rec.Code, problem)
	}
}

// validate comprueba value contra un esquema del contrato. Cubre lo que el contrato
// usa: $ref, allOf, type, const, enum, required, properties, items, límites de
// tamaño y de valor, pattern y date-time. Devuelve un problema por línea.
func (c contract) validate(schema map[string]any, value any, at string) []string {
	if ref, ok := schema["$ref"].(string); ok {
		return c.validate(c.resolve(ref), value, at)
	}
	var problems []string
	for _, sub := range asList(schema["allOf"]) {
		problems = append(problems, c.validate(asMap(sub), value, at)...)
	}
	if typ, ok := schema["type"]; ok && !typeMatches(typ, value) {
		return append(problems, fmt.Sprintf("%s: tipo %T, el contrato pide %v", at, value, typ))
	}
	if want, ok := schema["const"]; ok && !reflect.DeepEqual(want, value) {
		problems = append(problems, fmt.Sprintf("%s: %v, el contrato pide %v", at, value, want))
	}
	if enum := asList(schema["enum"]); enum != nil && !containsValue(enum, value) {
		problems = append(problems, fmt.Sprintf("%s: %v no está en %v", at, value, enum))
	}
	switch v := value.(type) {
	case string:
		n := float64(utf8.RuneCountInString(v))
		if min, ok := schema["minLength"].(float64); ok && n < min {
			problems = append(problems, fmt.Sprintf("%s: %d caracteres, mínimo %v", at, int(n), min))
		}
		if max, ok := schema["maxLength"].(float64); ok && n > max {
			problems = append(problems, fmt.Sprintf("%s: %d caracteres, máximo %v", at, int(n), max))
		}
		if p, ok := schema["pattern"].(string); ok && !regexp.MustCompile(p).MatchString(v) {
			problems = append(problems, fmt.Sprintf("%s: %q no cumple %s", at, v, p))
		}
		if schema["format"] == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %q no es una fecha RFC 3339", at, v))
			}
		}
	case float64:
		if min, ok := schema["minimum"].(float64); ok && v < min {
			problems = append(problems, fmt.Sprintf("%s: %v, mínimo %v", at, v, min))
		}
		if max, ok := schema["maximum"].(float64); ok && v > max {
			problems = append(problems, fmt.Sprintf("%s: %v, máximo %v", at, v, max))
		}
	case []any:
		n := float64(len(v))
		if min, ok := schema["minItems"].(float64); ok && n < min {
			problems = append(problems, fmt.Sprintf("%s: %d elementos, mínimo %v", at, len(v), min))
		}
		if max, ok := schema["maxItems"].(float64); ok && n > max {
			problems = append(problems, fmt.Sprintf("%s: %d elementos, máximo %v", at, len(v), max))
		}
		if items := asMap(schema["items"]); items != nil {
			for i, item := range v {
				problems = append(problems, c.validate(items, item, fmt.Sprintf("%s[%d]", at, i))...)
			}
		}
	case map[string]any:
		for _, name := range asStrings(schema["required"]) {
			if _, ok := v[name]; !ok {
				problems = append(problems, fmt.Sprintf("%s: falta %q", at, name))
			}
		}
		for name, sub := range asMap(schema["properties"]) {
			if field, ok := v[name]; ok {
				problems = append(problems, c.validate(asMap(sub), field, at+"."+name)...)
			}
		}
	}
	return problems
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func asStrings(v any) []string {
	var out []string
	for _, s := range asList(v) {
		out = append(out, s.(string))
	}
	return out
}

// typeMatches dice si value cumple el «type» de JSON Schema, que puede ser una lista.
func typeMatches(typ any, value any) bool {
	names := []string{}
	switch t := typ.(type) {
	case string:
		names = append(names, t)
	case []any:
		for _, n := range t {
			names = append(names, n.(string))
		}
	}
	for _, n := range names {
		switch n {
		case "null":
			if value == nil {
				return true
			}
		case "string":
			if _, ok := value.(string); ok {
				return true
			}
		case "boolean":
			if _, ok := value.(bool); ok {
				return true
			}
		case "integer":
			if f, ok := value.(float64); ok && f == math.Trunc(f) {
				return true
			}
		case "number":
			if _, ok := value.(float64); ok {
				return true
			}
		case "array":
			if _, ok := value.([]any); ok {
				return true
			}
		case "object":
			if _, ok := value.(map[string]any); ok {
				return true
			}
		}
	}
	return false
}

func containsValue(list []any, value any) bool {
	for _, v := range list {
		if reflect.DeepEqual(v, value) {
			return true
		}
	}
	return false
}

// TestResponsesMatchTheContract recorre las operaciones del contrato y comprueba cada
// respuesta del servidor contra lo que declara: el código, las cabeceras y el cuerpo.
func TestResponsesMatchTheContract(t *testing.T) {
	f := newFixture(t)
	c := loadContract(t)
	auth := map[string]string{"Authorization": "Bearer " + f.token}
	withAuth := func(extra map[string]string) map[string]string {
		out := map[string]string{"Authorization": "Bearer " + f.token}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	scenarios := []struct {
		name       string
		method     string
		tmpl       string // ruta en el contrato
		url        string // ruta real de la petición
		header     map[string]string
		body       string
		status     int
		revalidate bool // repetir con If-None-Match y esperar 304
	}{
		{"info", http.MethodGet, "/api/v1/info", "/api/v1/info", nil, "", 200, false},
		{"login", http.MethodPost, "/api/v1/sessions", "/api/v1/sessions", nil,
			fmt.Sprintf(`{"password":%q,"device":"Contrato"}`, testPassword), 201, false},
		{"login sin campos", http.MethodPost, "/api/v1/sessions", "/api/v1/sessions", nil, `{}`, 400, false},
		{"login con contraseña mala", http.MethodPost, "/api/v1/sessions", "/api/v1/sessions", nil,
			`{"password":"no es","device":"Contrato"}`, 401, false},
		{"sesiones", http.MethodGet, "/api/v1/sessions", "/api/v1/sessions", auth, "", 200, false},
		{"sesiones sin token", http.MethodGet, "/api/v1/sessions", "/api/v1/sessions", nil, "", 401, false},
		{"sesión con id malo", http.MethodDelete, "/api/v1/sessions/{sessionId}", "/api/v1/sessions/abc", auth, "", 400, false},
		{"sesión que no existe", http.MethodDelete, "/api/v1/sessions/{sessionId}", "/api/v1/sessions/999999", auth, "", 404, false},
		{"artistas", http.MethodGet, "/api/v1/artists", "/api/v1/artists", auth, "", 200, true},
		{"artistas sin token", http.MethodGet, "/api/v1/artists", "/api/v1/artists", nil, "", 401, false},
		{"artista", http.MethodGet, "/api/v1/artists/{artistId}", fmt.Sprintf("/api/v1/artists/%d", f.artist), auth, "", 200, true},
		{"artista con id malo", http.MethodGet, "/api/v1/artists/{artistId}", "/api/v1/artists/0", auth, "", 400, false},
		{"artista que no existe", http.MethodGet, "/api/v1/artists/{artistId}", "/api/v1/artists/999999", auth, "", 404, false},
		{"álbumes por nombre", http.MethodGet, "/api/v1/albums", "/api/v1/albums", auth, "", 200, true},
		{"álbumes por importación", http.MethodGet, "/api/v1/albums", "/api/v1/albums?sort=added", auth, "", 200, false},
		{"álbumes con orden malo", http.MethodGet, "/api/v1/albums", "/api/v1/albums?sort=nombre", auth, "", 400, false},
		{"álbum", http.MethodGet, "/api/v1/albums/{albumId}", fmt.Sprintf("/api/v1/albums/%d", f.albumA), auth, "", 200, true},
		{"álbum con id malo", http.MethodGet, "/api/v1/albums/{albumId}", "/api/v1/albums/x", auth, "", 400, false},
		{"álbum que no existe", http.MethodGet, "/api/v1/albums/{albumId}", "/api/v1/albums/999999", auth, "", 404, false},
		{"carátula", http.MethodGet, "/api/v1/albums/{albumId}/cover", fmt.Sprintf("/api/v1/albums/%d/cover", f.albumA), auth, "", 200, true},
		{"carátula de un álbum sin ella", http.MethodGet, "/api/v1/albums/{albumId}/cover", fmt.Sprintf("/api/v1/albums/%d/cover", f.albumB), auth, "", 404, false},
		{"pista", http.MethodGet, "/api/v1/tracks/{trackId}", fmt.Sprintf("/api/v1/tracks/%d", f.trackA), auth, "", 200, true},
		{"pista que no existe", http.MethodGet, "/api/v1/tracks/{trackId}", "/api/v1/tracks/999999", auth, "", 404, false},
		{"enlace", http.MethodGet, "/api/v1/tracks/{trackId}/link", fmt.Sprintf("/api/v1/tracks/%d/link?quality=orig", f.trackA), auth, "", 200, false},
		{"enlace sin calidad", http.MethodGet, "/api/v1/tracks/{trackId}/link", fmt.Sprintf("/api/v1/tracks/%d/link", f.trackA), auth, "", 400, false},
		{"enlace a una versión que no hay", http.MethodGet, "/api/v1/tracks/{trackId}/link", fmt.Sprintf("/api/v1/tracks/%d/link?quality=aac256", f.trackB), auth, "", 404, false},
		{"búsqueda", http.MethodGet, "/api/v1/search", "/api/v1/search?q=grupo", auth, "", 200, true},
		{"búsqueda sin texto", http.MethodGet, "/api/v1/search", "/api/v1/search", auth, "", 400, false},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			rec := f.req(sc.method, sc.url, sc.body, sc.header)
			if rec.Code != sc.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, sc.status, rec.Body.String())
			}
			c.checkResponse(t, sc.method, sc.tmpl, rec)
			if !sc.revalidate {
				return
			}
			etag := rec.Header().Get("ETag")
			again := f.req(sc.method, sc.url, sc.body, withAuth(map[string]string{"If-None-Match": etag}))
			if again.Code != http.StatusNotModified {
				t.Fatalf("con If-None-Match %s: status %d, want 304", etag, again.Code)
			}
			c.checkResponse(t, sc.method, sc.tmpl, again)
		})
	}

	t.Run("cerrar sesión", func(t *testing.T) {
		out := f.login(testPassword, "Para cerrar")
		rec := f.req(http.MethodDelete, fmt.Sprintf("/api/v1/sessions/%d", out.Session.ID), "", map[string]string{"Authorization": "Bearer " + f.token})
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d, want 204", rec.Code)
		}
		c.checkResponse(t, http.MethodDelete, "/api/v1/sessions/{sessionId}", rec)
	})

	t.Run("bloqueo", func(t *testing.T) {
		body := `{"password":"no es","device":"Contrato"}`
		var rec *httptest.ResponseRecorder
		for i := 0; i < maxFailures; i++ {
			rec = f.req(http.MethodPost, "/api/v1/sessions", body, nil)
		}
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("el décimo fallo responde %d, want 429", rec.Code)
		}
		c.checkResponse(t, http.MethodPost, "/api/v1/sessions", rec)
	})
}
