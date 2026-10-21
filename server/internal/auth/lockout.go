package auth

import (
	"net/netip"
	"sync"
	"time"
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

// lockout cuenta los fallos de contraseña por IP. Es seguro para uso concurrente.
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

// failed anota un fallo de ip y devuelve los intentos que le quedan. El fallo que
// llega al límite bloquea la IP, y su siguiente intento ya no llega a comprobar
// la contraseña.
func (l *lockout) failed(ip netip.Addr, now time.Time) int {
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
	}
	return max(maxFailures-f.count, 0)
}

// succeeded borra los fallos de ip: acertar pone su contador a cero.
func (l *lockout) succeeded(ip netip.Addr) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.ips, ip)
}

// evictOldest borra la IP sin bloquear cuyo último fallo es el más antiguo. Las
// bloqueadas no se borran, porque sería desbloquearlas. Hay que llamarla con el
// mutex tomado.
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
	if found {
		delete(l.ips, oldest)
	}
}
