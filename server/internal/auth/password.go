// Package auth guarda la contraseña de la única cuenta, sus sesiones y el bloqueo
// por IP (ADR-014). No conoce HTTP: los endpoints de /api/v1 lo usan.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// MinPasswordLength es el mínimo de caracteres de una contraseña (ADR-014).
const MinPasswordLength = 12

// maxPasswordBytes acota lo que se acepta: una contraseña no necesita más, y sin
// límite una entrada sin fin llenaría la memoria.
const maxPasswordBytes = 1024

// params son los parámetros de Argon2id. Cada hash los lleva escritos, así que
// cambiarlos más adelante no rompe las contraseñas ya guardadas.
type params struct {
	time    uint32 // pasadas sobre la memoria
	memory  uint32 // KiB
	threads uint8
}

// defaultParams son los de la RFC 9106 para memoria limitada: 64 MiB, 3 pasadas
// y 4 hilos.
var defaultParams = params{time: 3, memory: 64 * 1024, threads: 4}

const (
	saltBytes = 16
	keyBytes  = 32

	// Límites al leer un hash guardado. Son valores nuestros, pero uno absurdo no
	// debe reservar gigas de memoria al comprobar una contraseña.
	maxMemory  = 256 * 1024
	maxTime    = 10
	maxThreads = 16
)

var errBadHash = errors.New("hash de contraseña no válido")

// ValidatePassword dice si password sirve como contraseña nueva. Se cuenta en
// caracteres, no en bytes: una «ñ» es un carácter aunque ocupe dos bytes. Tiene
// que ser UTF-8 válido porque el cliente la envía así en JSON.
func ValidatePassword(password string) error {
	if !utf8.ValidString(password) {
		return errors.New("la contraseña no es UTF-8 válido")
	}
	if n := utf8.RuneCountInString(password); n < MinPasswordLength {
		return fmt.Errorf("la contraseña tiene %d caracteres y debe tener al menos %d", n, MinPasswordLength)
	}
	if len(password) > maxPasswordBytes {
		return fmt.Errorf("la contraseña ocupa más de %d bytes", maxPasswordBytes)
	}
	return nil
}

// HashPassword devuelve la contraseña como hash Argon2id en formato PHC, con una
// sal aleatoria nueva.
func HashPassword(password string) (string, error) {
	return hashWith(password, defaultParams)
}

func hashWith(password string, p params) (string, error) {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generar la sal: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, keyBytes)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memory, p.time, p.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword dice si password coincide con el hash PHC. Los parámetros salen
// del propio hash, así que sirve también para hashes hechos con otros parámetros.
func VerifyPassword(password, phc string) (bool, error) {
	p, salt, want, err := parsePHC(phc)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// parsePHC lee $argon2id$v=19$m=…,t=…,p=…$sal$clave. Rechaza todo lo que no sea
// exactamente ese formato, o unos parámetros fuera de los límites.
func parsePHC(s string) (params, []byte, []byte, error) {
	// Se parte en ["", "argon2id", "v=19", "m=…,t=…,p=…", sal, clave].
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return params{}, nil, nil, errBadHash
	}
	var p params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return params{}, nil, nil, errBadHash
	}
	// Sscanf acepta un sufijo sobrante; la comparación con la forma canónica no.
	if parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", p.memory, p.time, p.threads) {
		return params{}, nil, nil, errBadHash
	}
	if p.time < 1 || p.time > maxTime || p.threads < 1 || p.threads > maxThreads ||
		p.memory < 8*uint32(p.threads) || p.memory > maxMemory {
		return params{}, nil, nil, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return params{}, nil, nil, errBadHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return params{}, nil, nil, errBadHash
	}
	return p, salt, key, nil
}
