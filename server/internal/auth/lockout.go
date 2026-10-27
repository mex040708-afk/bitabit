package auth

import (
	"net/netip"
	"sync"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/limits"
)

const (
	// maxFailures fallos seguidos bloquean una IP (ADR-014).
	maxFailures = 10
	// blockFor es lo que dura el bloqueo.
	blockFor = 24 * time.Hour
	// maxTrackedIPs acota la tabla. Sin límite, quien prueba desde muchas IPs
	// podría llenar la memoria.
	maxTrackedIPs = 10000
)

// failures son los fallos seguidos de una IP. Viven solo en memoria: reiniciar el
// servicio los borra, como dice ADR-014.
type failures struct {
	count        int
	lastFail     time.Time
	blockedUntil time.Time // cero si la IP no está bloqueada
}

// lockout cuenta los fallos de contraseña por IP (por /64 en IPv6, ver limits.Network). Es seguro para uso concurrente.
type lockout struct {
	mu  sync.Mutex
	ips map[netip.Addr]*failures
}

func newLockout() *lockout {
	return &lockout{ips: make(map[netip.Addr]*failures)}
}

// blocked dice si ip está bloqueada y cuánto falta para que se desbloquee. Al
// vencer el bloqueo borra la entrada: la IP vuelve a empezar de cero.
func (l *lockout) blocked(ip netip.Addr, now time.Time) (time.Duration, bool) {
	ip = limits.Network(ip)
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.ips[ip]
	if !ok || f.blockedUntil.IsZero() {
		return 0, false
	}
	if now.Before(f.blockedUntil) {
		return f.blockedUntil.Sub(now), true
	}
	delete(l.ips, ip)
	return 0, false
}

// failed anota un fallo de ip. Devuelve los intentos que le quedan. El fallo que
// llega al límite ya bloquea la IP: su propia respuesta es el bloqueo, como pide
// ADR-014 (los nueve primeros responden 401 y el décimo, 429).
func (l *lockout) failed(ip netip.Addr, now time.Time) (attemptsLeft int, blocked bool) {
	ip = limits.Network(ip)
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.ips[ip]
	if !ok {
		if len(l.ips) >= maxTrackedIPs {
			l.evictOldest()
		}
		f = &failures{}
		l.ips[ip] = f
	}
	f.count++
	f.lastFail = now
	if f.count >= maxFailures {
		f.blockedUntil = now.Add(blockFor)
		return 0, true
	}
	return maxFailures - f.count, false
}

// succeeded borra los fallos de ip: acertar pone su contador a cero.
func (l *lockout) succeeded(ip netip.Addr) {
	ip = limits.Network(ip)
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.ips, ip)
}

// evictOldest hace sitio en la tabla llena: borra la IP sin bloquear cuyo último fallo
// es el más antiguo. Si todas están bloqueadas, borra la que antes se desbloquearía de
// todas formas; sin esto, quien llenara la tabla de IPs bloqueadas la haría crecer sin
// fin. Hay que llamarla con el mutex tomado.
func (l *lockout) evictOldest() {
	var oldest netip.Addr
	var oldestAt time.Time
	found := false
	for ip, f := range l.ips {
		if !f.blockedUntil.IsZero() {
			continue
		}
		if !found || f.lastFail.Before(oldestAt) {
			oldest, oldestAt, found = ip, f.lastFail, true
		}
	}
	if !found {
		for ip, f := range l.ips {
			if oldestAt.IsZero() || f.blockedUntil.Before(oldestAt) {
				oldest, oldestAt, found = ip, f.blockedUntil, true
			}
		}
	}
	if found {
		delete(l.ips, oldest)
	}
}
