package auth

import (
	"net/netip"
	"testing"
	"time"
)

var (
	testIP  = netip.MustParseAddr("192.0.2.7")
	otherIP = netip.MustParseAddr("2001:db8::1")
	start   = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
)

func TestLockoutBlocksIPAfterTenFailures(t *testing.T) {
	l := newLockout()
	for i := 1; i < maxFailures; i++ {
		if left := l.failed(testIP, start); left != maxFailures-i {
			t.Fatalf("fallo %d: quedan %d intentos, want %d", i, left, maxFailures-i)
		}
		if _, blocked := l.blocked(testIP, start); blocked {
			t.Fatalf("bloqueada tras %d fallos", i)
		}
	}
	// El décimo fallo no deja intentos, pero todavía responde como un fallo.
	if left := l.failed(testIP, start); left != 0 {
		t.Fatalf("al décimo fallo quedan %d intentos, want 0", left)
	}
	if wait, blocked := l.blocked(testIP, start); !blocked || wait != blockFor {
		t.Errorf("blocked = %v, %v; want %v, true", wait, blocked, blockFor)
	}
	if wait, blocked := l.blocked(testIP, start.Add(time.Hour)); !blocked || wait != blockFor-time.Hour {
		t.Errorf("una hora después: %v, %v; want %v, true", wait, blocked, blockFor-time.Hour)
	}
	if _, blocked := l.blocked(otherIP, start); blocked {
		t.Error("el bloqueo de una IP afecta a otra")
	}
}

func TestLockoutEndsAfterBlockAndStartsOver(t *testing.T) {
	l := newLockout()
	for range maxFailures {
		l.failed(testIP, start)
	}
	if _, blocked := l.blocked(testIP, start.Add(blockFor)); blocked {
		t.Fatal("sigue bloqueada al vencer las 24 horas")
	}
	if left := l.failed(testIP, start.Add(blockFor)); left != maxFailures-1 {
		t.Errorf("tras el bloqueo quedan %d intentos, want %d", left, maxFailures-1)
	}
}

func TestLockoutSuccessResetsCounter(t *testing.T) {
	l := newLockout()
	for range maxFailures - 1 {
		l.failed(testIP, start)
	}
	l.succeeded(testIP)
	if left := l.failed(testIP, start); left != maxFailures-1 {
		t.Errorf("tras acertar quedan %d intentos, want %d", left, maxFailures-1)
	}
}

func TestLockoutEvictsOldestUnblockedWhenFull(t *testing.T) {
	l := newLockout()
	for range maxFailures {
		l.failed(testIP, start) // bloqueada: nunca se borra
	}
	// Llena la tabla con IPs de un fallo cada una, una por segundo.
	for i := 0; len(l.ips) < maxTrackedIPs; i++ {
		ip := netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)})
		l.failed(ip, start.Add(time.Duration(i)*time.Second))
	}
	oldest := netip.AddrFrom4([4]byte{10, 0, 0, 0})

	l.failed(netip.AddrFrom4([4]byte{192, 0, 2, 1}), start.Add(time.Hour))

	if len(l.ips) != maxTrackedIPs {
		t.Errorf("la tabla tiene %d IPs, want %d", len(l.ips), maxTrackedIPs)
	}
	if _, ok := l.ips[oldest]; ok {
		t.Error("no borró la IP con el fallo más antiguo")
	}
	if _, blocked := l.blocked(testIP, start); !blocked {
		t.Error("borró una IP bloqueada al llenarse la tabla")
	}
}
