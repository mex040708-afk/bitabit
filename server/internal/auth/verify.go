package auth

import (
	"context"
	"fmt"
	"net/netip"
)

// maxConcurrentVerifies acota las comprobaciones de Argon2id a la vez. Cada una
// cuesta unos 100 ms y 64 MiB: sin tope, una ráfaga de peticiones con contraseña
// agota la memoria del servidor antes de que el bloqueo por IP cuente el primer
// fallo.
const maxConcurrentVerifies = 4

// verifySlots es el semáforo de esas comprobaciones, compartido por Login y
// CheckPassword.
var verifySlots = make(chan struct{}, maxConcurrentVerifies)

// verify comprueba password contra el hash guardado esperando turno. Al recibir el
// turno vuelve a mirar el bloqueo de ip: las peticiones que llegaron a la vez
// pasaron todas la primera comprobación, y sin esta segunda una ráfaga de miles
// de intentos esquivaría el límite de 10 fallos.
func (s *Service) verify(ctx context.Context, ip netip.Addr, password, stored string) (bool, error) {
	select {
	case verifySlots <- struct{}{}:
		defer func() { <-verifySlots }()
	case <-ctx.Done():
		return false, fmt.Errorf("esperar turno para comprobar la contraseña: %w", ctx.Err())
	}
	if wait, blocked := s.lock.blocked(ip, s.now()); blocked {
		return false, &BlockedError{RetryAfter: wait}
	}
	ok, err := VerifyPassword(password, stored)
	if err != nil {
		return false, fmt.Errorf("comprobar la contraseña: %w", err)
	}
	return ok, nil
}
