package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

const base64URLAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

func TestNewTokenIsRandomAndHashed(t *testing.T) {
	token, sum, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != tokenLen {
		t.Fatalf("token de %d caracteres, want %d", len(token), tokenLen)
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != tokenBytes {
		t.Fatalf("el token no decodifica a %d bytes: %v", tokenBytes, err)
	}
	want := sha256.Sum256(raw)
	if !bytes.Equal(sum, want[:]) {
		t.Error("la suma no es el SHA-256 de los bytes del token")
	}
	if got, err := tokenSum(token); err != nil || !bytes.Equal(got, sum) {
		t.Errorf("tokenSum(token) = %x, %v; want la suma de NewToken", got, err)
	}
	if other, _, err := NewToken(); err != nil || other == token {
		t.Errorf("dos tokens iguales (%v)", err)
	}
}

func TestTokenSumRejectsOtherText(t *testing.T) {
	token, _, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	// Los dos bits sobrantes del último carácter tienen que ser cero. Otro
	// carácter decodificaría los mismos bytes, así que no se acepta.
	last := strings.IndexByte(base64URLAlphabet, token[tokenLen-1])
	nonCanonical := token[:tokenLen-1] + string(base64URLAlphabet[last^1])

	bad := []string{
		"",
		token[:tokenLen-1],
		token + "A",
		"+" + token[1:], // «+» no está en base64url
		strings.Repeat("=", tokenLen),
		nonCanonical,
	}
	for _, s := range bad {
		if _, err := tokenSum(s); err == nil {
			t.Errorf("tokenSum(%q) acepta un texto que no es un token nuestro", s)
		}
	}
}
