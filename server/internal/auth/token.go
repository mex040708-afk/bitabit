package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// ErrUnauthorized: el token no existe, está mal escrito o su sesión caducó.
var ErrUnauthorized = errors.New("token no válido")

// tokenBytes son los bytes aleatorios de un token: 256 bits, que no hay forma
// práctica de adivinar.
const tokenBytes = 32

// tokenLen es la longitud del token en base64url sin relleno.
const tokenLen = 43

// NewToken crea un token nuevo. El cliente lo recibe una sola vez; la base de
// datos guarda solo sum, su SHA-256, así que un volcado de la base no sirve para
// entrar.
func NewToken() (token string, sum []byte, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generar el token: %w", err)
	}
	h := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(raw), h[:], nil
}

// tokenSum devuelve el SHA-256 de un token que llega del cliente. Solo acepta la
// forma en que se entrega: 43 caracteres de base64url, sin bits sobrantes. Otra
// cosa no puede ser un token nuestro, y así no hay dos textos distintos para el
// mismo token.
func tokenSum(token string) ([]byte, error) {
	if len(token) != tokenLen {
		return nil, ErrUnauthorized
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != tokenBytes {
		return nil, ErrUnauthorized
	}
	h := sha256.Sum256(raw)
	return h[:], nil
}
