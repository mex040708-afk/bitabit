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
	for range maxConcurrentVerifies {
		verifySlots <- struct{}{}
	}
	t.Cleanup(func() {
		for range maxConcurrentVerifies {
			<-verifySlots
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.verify(ctx, testIP, "x", "no-es-un-hash"); !errors.Is(err, context.Canceled) {
		t.Fatalf("verify sin turno y con el contexto cancelado = %v; want context.Canceled", err)
	}
}
