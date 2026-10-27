package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"sync"
	"time"
)

const (
	// verifiedTTL es lo que se recuerda una contraseña ya comprobada. Cada petición
	// de /rest con u y p paga un Argon2id (unos 100 ms y 64 MiB, medido): un cliente
	// que pide 50 carátulas a la vez agotaría la memoria sin esto (ADR-031).
	verifiedTTL = 5 * time.Minute
	// maxVerified acota la memoria: con una sola cuenta rara vez habrá más de una.
	maxVerified = 8
)

// verifiedCache recuerda contraseñas que ya pasaron Argon2id. Guarda solo un HMAC
// del hash guardado y la contraseña, con una clave que nace con el proceso: cambiar
// la contraseña cambia el hash y deja inservibles las entradas, y un volcado de la
// memoria no da la contraseña. Los fallos nunca se guardan.
type verifiedCache struct {
	mu      sync.Mutex
	key     [32]byte
	entries map[[32]byte]time.Time // caducidad de cada entrada
}

func newVerifiedCache() *verifiedCache {
	c := &verifiedCache{entries: map[[32]byte]time.Time{}}
	if _, err := rand.Read(c.key[:]); err != nil {
		// Sin azar no hay clave: la caché queda desactivada (key a cero no se usa).
		c.entries = nil
	}
	return c
}

func (c *verifiedCache) tag(storedHash, password string) (tag [32]byte) {
	m := hmac.New(sha256.New, c.key[:])
	m.Write([]byte(storedHash))
	m.Write([]byte{0})
	m.Write([]byte(password))
	copy(tag[:], m.Sum(nil))
	return tag
}

// has dice si password se comprobó contra storedHash hace menos de verifiedTTL.
func (c *verifiedCache) has(storedHash, password string, now time.Time) bool {
	if c.entries == nil {
		return false
	}
	tag := c.tag(storedHash, password)
	c.mu.Lock()
	defer c.mu.Unlock()
	exp, ok := c.entries[tag]
	if ok && !now.Before(exp) {
		delete(c.entries, tag)
		return false
	}
	return ok
}

// add recuerda una contraseña buena.
func (c *verifiedCache) add(storedHash, password string, now time.Time) {
	if c.entries == nil {
		return
	}
	tag := c.tag(storedHash, password)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= maxVerified {
		for k, exp := range c.entries { // se tira la que caduca antes
			if !now.Before(exp) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= maxVerified {
			clear(c.entries)
		}
	}
	c.entries[tag] = now.Add(verifiedTTL)
}
