package auth

import (
	"encoding/base64"
	"strings"
	"testing"
)

// Con parámetros mínimos los tests son rápidos. El hash real se prueba una vez.
var fastParams = params{time: 1, memory: 8, threads: 1}

const testPassword = "contraseña de prueba"

func TestHashAndVerify(t *testing.T) {
	hash, err := hashWith(testPassword, fastParams)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		password string
		want     bool
	}{
		{testPassword, true},
		{testPassword + " ", false},
		{"Contraseña de prueba", false},
		{"", false},
	} {
		ok, err := VerifyPassword(tt.password, hash)
		if err != nil || ok != tt.want {
			t.Errorf("VerifyPassword(%q) = %v, %v; want %v", tt.password, ok, err, tt.want)
		}
	}
}

func TestHashUsesFreshSalt(t *testing.T) {
	a, err := hashWith(testPassword, fastParams)
	if err != nil {
		t.Fatal(err)
	}
	b, err := hashWith(testPassword, fastParams)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("dos hashes de la misma contraseña son iguales: la sal no cambia")
	}
}

// La contraseña guardada lleva sus parámetros: si se cambian después, las viejas
// siguen valiendo.
func TestVerifyUsesParametersInHash(t *testing.T) {
	hash, err := hashWith(testPassword, params{time: 2, memory: 16, threads: 2})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword(testPassword, hash)
	if err != nil || !ok {
		t.Errorf("VerifyPassword con parámetros distintos a los actuales = %v, %v; want true", ok, err)
	}
}

func TestHashWithDefaultParameters(t *testing.T) {
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "$argon2id$v=19$m=65536,t=3,p=4$"
	if !strings.HasPrefix(hash, prefix) {
		t.Errorf("hash = %q, want prefijo %q", hash, prefix)
	}
	if ok, err := VerifyPassword(testPassword, hash); err != nil || !ok {
		t.Errorf("VerifyPassword = %v, %v; want true", ok, err)
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	good, err := hashWith(testPassword, fastParams)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(good, "$") // ["", "argon2id", "v=19", "m=8,t=1,p=1", sal, clave]
	emptyKey := base64.RawStdEncoding.EncodeToString(make([]byte, 8))
	bad := map[string]string{
		"vacío":                        "",
		"texto plano":                  testPassword,
		"otro algoritmo":               strings.Replace(good, "argon2id", "argon2i", 1),
		"otra versión":                 strings.Replace(good, "v=19", "v=16", 1),
		"parámetro de más":             strings.Replace(good, "m=8,t=1,p=1", "m=8,t=1,p=1,x=2", 1),
		"sufijo tras la memoria":       strings.Replace(good, "m=8,t=1,p=1", "m=8,t=1,p=1x", 1),
		"paralelismo cero":             strings.Replace(good, "m=8,t=1,p=1", "m=8,t=1,p=0", 1),
		"memoria menor que 8·p":        strings.Replace(good, "m=8,t=1,p=1", "m=4,t=1,p=1", 1),
		"memoria excesiva":             strings.Replace(good, "m=8,t=1,p=1", "m=4294967295,t=1,p=1", 1),
		"demasiadas pasadas":           strings.Replace(good, "m=8,t=1,p=1", "m=8,t=11,p=1", 1),
		"sal con caracteres no base64": strings.Replace(good, "$"+parts[4]+"$", "$!!!$", 1),
		"clave demasiado corta":        strings.Replace(good, "$"+parts[5], "$"+emptyKey, 1),
		"campo de más":                 good + "$extra",
	}
	for desc, h := range bad {
		if _, err := VerifyPassword(testPassword, h); err == nil {
			t.Errorf("%s: VerifyPassword acepta el hash %q", desc, h)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	ok := []string{
		strings.Repeat("a", MinPasswordLength),
		strings.Repeat("ñ", MinPasswordLength), // doce caracteres aunque ocupen 24 bytes
		strings.Repeat("a", maxPasswordBytes),
	}
	for _, p := range ok {
		if err := ValidatePassword(p); err != nil {
			t.Errorf("ValidatePassword(%d caracteres) = %v; want nil", len([]rune(p)), err)
		}
	}
	bad := []string{
		"",
		strings.Repeat("a", MinPasswordLength-1),
		strings.Repeat("ñ", MinPasswordLength-1), // once caracteres, veintidós bytes
		strings.Repeat("a", maxPasswordBytes+1),
		"\xff\xfe" + strings.Repeat("a", MinPasswordLength), // UTF-8 no válido
	}
	for _, p := range bad {
		if err := ValidatePassword(p); err == nil {
			t.Errorf("ValidatePassword(%d bytes) acepta una contraseña no válida", len(p))
		}
	}
}
