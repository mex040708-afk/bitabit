package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/limits"
)

const (
	// maxConcurrentVerifies acota las comprobaciones de Argon2id a la vez (ADR-040).
	// Cada una cuesta unos 100 ms, 64 MiB y 4 hilos: sin tope, una ráfaga de
	// peticiones con contraseña agota la memoria del servidor antes de que el bloqueo
	// por IP cuente el primer fallo.
	maxConcurrentVerifies = 2
	// verifyWait es lo que espera un turno quien llega con todo ocupado; pasado ese
	// plazo recibe ErrBusy y la API responde 503 con Retry-After.
	verifyWait = 5 * time.Second
	// VerifyRetryAfter es el Retry-After de ese 503.
	VerifyRetryAfter = 5 * time.Second
)

// ErrBusy: no hubo turno para comprobar la contraseña en el plazo. El cliente debe
// reintentar; no cuenta como fallo de contraseña.
var ErrBusy = errors.New("demasiadas comprobaciones de contraseña a la vez")

// verifyGate es la compuerta de esas comprobaciones, compartida por Login y
// CheckPassword.
var verifyGate = limits.NewGate(maxConcurrentVerifies)

// verify comprueba password contra el hash guardado esperando turno. Al recibir el
// turno vuelve a mirar el bloqueo de ip: las peticiones que llegaron a la vez
// pasaron todas la primera comprobación, y sin esta segunda una ráfaga de miles
// de intentos esquivaría el límite de 10 fallos.
func (s *Service) verify(ctx context.Context, ip netip.Addr, password, stored string) (bool, error) {
	release, err := verifyGate.Acquire(ctx, verifyWait)
	if errors.Is(err, limits.ErrBusy) {
		return false, ErrBusy
	}
	if err != nil {
		return false, fmt.Errorf("esperar turno para comprobar la contraseña: %w", err)
	}
	defer release()
	if wait, blocked := s.lock.blocked(ip, s.now()); blocked {
		return false, &BlockedError{RetryAfter: wait}
	}
	ok, err := VerifyPassword(password, stored)
	if err != nil {
		return false, fmt.Errorf("comprobar la contraseña: %w", err)
	}
	return ok, nil
}
