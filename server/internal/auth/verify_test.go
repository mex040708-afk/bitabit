package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newBareService no necesita base de datos: verify solo usa el bloqueo y el reloj.
func newBareService() *Service {
	return &Service{lock: newLockout(), verified: newVerifiedCache(), now: func() time.Time { return start }}
}

// fillTurns ocupa todos los turnos y los suelta al acabar el test.
func fillTurns(t *testing.T) {
	t.Helper()
	var releases []func()
	for range maxConcurrentVerifies {
		release, err := verifyGate.Acquire(context.Background(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	t.Cleanup(func() {
		for _, release := range releases {
			release()
		}
	})
}

func TestOnlyTwoVerificationsAtATime(t *testing.T) {
	if maxConcurrentVerifies != 2 {
		t.Errorf("maxConcurrentVerifies = %d; el ADR-040 fija 2", maxConcurrentVerifies)
	}
}

func TestVerifyRechecksTheLockoutAfterWaitingForATurn(t *testing.T) {
	s := newBareService()
	for range maxFailures {
		s.lock.failed(testIP, start)
	}
	// El hash no llega a leerse: la IP ya está bloqueada.
	_, err := s.verify(context.Background(), testIP, "x", "no-es-un-hash")
	var blocked *BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("verify con la IP bloqueada = %v; want *BlockedError", err)
	}
}

func TestVerifyGivesUpWhenTheContextEndsWhileWaiting(t *testing.T) {
	s := newBareService()
	fillTurns(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.verify(ctx, testIP, "x", "no-es-un-hash"); !errors.Is(err, context.Canceled) {
		t.Fatalf("verify sin turno y con el contexto cancelado = %v; want context.Canceled", err)
	}
}

// Sin turno tras la espera, el error es ErrBusy (503), no un fallo de contraseña: no
// cuenta para el bloqueo de la IP.
func TestVerifyIsBusyAfterTheWaitAndDoesNotCountAsAFailure(t *testing.T) {
	s := newBareService()
	fillTurns(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	begin := time.Now()
	_, err := s.verify(ctx, testIP, "x", "no-es-un-hash")
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("verify sin turno = %v; want ErrBusy", err)
	}
	if took := time.Since(begin); took < verifyWait-time.Second || took > verifyWait+3*time.Second {
		t.Errorf("esperó %v; la espera del ADR-040 son %v", took, verifyWait)
	}
	if wait, blocked := s.lock.blocked(testIP, start); blocked {
		t.Errorf("un 503 no debe contar como fallo, pero la IP quedó bloqueada %v", wait)
	}
}
