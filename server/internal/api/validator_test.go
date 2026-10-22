package api

import "testing"

// El validador del contrato tiene que rechazar lo que el contrato rechaza: si no,
// TestResponsesMatchTheContract pasaría con cualquier respuesta.
func TestValidatorReportsBrokenValues(t *testing.T) {
	c := loadContract(t)
	errorSchema := map[string]any{"$ref": "#/components/schemas/Error"}
	linkSchema := map[string]any{"$ref": "#/components/schemas/Link"}

	cases := []struct {
		name   string
		schema map[string]any
		value  any
	}{
		{"código de error inventado", errorSchema, map[string]any{"error": "inventado", "message": "x"}},
		{"mensaje que no es texto", errorSchema, map[string]any{"error": "not_found", "message": 3.0}},
		{"falta un campo obligatorio", errorSchema, map[string]any{"error": "not_found"}},
		{"ruta de enlace que no es de /media", linkSchema, map[string]any{
			"path": "/otra/1.flac?q=orig", "quality": "orig", "expiresAt": "2026-10-22T08:00:00Z"}},
		{"fecha que no es RFC 3339", linkSchema, map[string]any{
			"path": "/media/1.flac?q=orig", "quality": "orig", "expiresAt": "mañana"}},
		{"calidad fuera del enum", linkSchema, map[string]any{
			"path": "/media/1.flac?q=orig", "quality": "hires", "expiresAt": "2026-10-22T08:00:00Z"}},
		{"id que no es entero", map[string]any{"$ref": "#/components/schemas/Id"}, 1.5},
		{"id cero", map[string]any{"$ref": "#/components/schemas/Id"}, 0.0},
	}
	for _, tt := range cases {
		if got := c.validate(tt.schema, tt.value, "$"); len(got) == 0 {
			t.Errorf("%s: el validador lo acepta", tt.name)
		}
	}

	valid := map[string]any{"error": "not_found", "message": "No existe."}
	if got := c.validate(errorSchema, valid, "$"); len(got) != 0 {
		t.Errorf("un error válido sale con problemas: %v", got)
	}
}
