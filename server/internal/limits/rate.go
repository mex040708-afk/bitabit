package limits

import (
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// IPLimiter es un cubo de fichas por IP: cada IP gasta una ficha por petición y
// recupera rate fichas por segundo hasta burst. La tabla está acotada para que
// un atacante con muchas IPs no la haga crecer sin fin.
type IPLimiter struct {
	rate       float64
	burst      float64
	maxEntries int
	now        func() time.Time

	mu      sync.Mutex
	buckets map[netip.Addr]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewIPLimiter crea el limitador. rate y burst deben ser positivos y maxEntries al
// menos 1.
func NewIPLimiter(rate float64, burst, maxEntries int) *IPLimiter {
	if rate <= 0 || burst < 1 || maxEntries < 1 {
		panic("limits: parámetros no válidos del limitador de ritmo")
	}
	return &IPLimiter{
		rate: rate, burst: float64(burst), maxEntries: maxEntries,
		now: time.Now, buckets: map[netip.Addr]*bucket{},
	}
}

// Allow gasta una ficha de ip. Si no queda ninguna, devuelve false y cuánto falta
// para la siguiente.
func (l *IPLimiter) Allow(ip netip.Addr) (ok bool, retryAfter time.Duration) {
	ip = ip.Unmap()
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[ip]
	if b == nil {
		if len(l.buckets) >= l.maxEntries {
			l.evict(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[ip] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}

// evict libera sitio: tira las IP que ya recuperaron todas sus fichas (no pierden
// nada) y, si no basta, la más antigua.
func (l *IPLimiter) evict(now time.Time) {
	var oldest netip.Addr
	var oldestAt time.Time
	for ip, b := range l.buckets {
		if min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate) >= l.burst {
			delete(l.buckets, ip)
			continue
		}
		if oldestAt.IsZero() || b.last.Before(oldestAt) {
			oldest, oldestAt = ip, b.last
		}
	}
	if len(l.buckets) >= l.maxEntries {
		delete(l.buckets, oldest)
	}
}

// RateLimit aplica l a las peticiones para las que applies dé true, con la IP que dé
// ipOf. Pasado el ritmo responde 429 con Retry-After. Lo demás pasa sin contar.
func RateLimit(l *IPLimiter, applies func(*http.Request) bool, ipOf func(*http.Request) netip.Addr) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if applies(r) {
				if ok, wait := l.Allow(ipOf(r)); !ok {
					w.Header().Set("Retry-After", RetryAfterSeconds(wait))
					w.Header().Set("Cache-Control", "no-store")
					http.Error(w, "demasiadas peticiones; reintenta en unos segundos", http.StatusTooManyRequests)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RemoteIP es la IP de la conexión (la real si el servidor lee el protocolo PROXY).
// Una dirección ilegible da la cero, que comparten todas esas peticiones.
func RemoteIP(r *http.Request) netip.Addr {
	addr, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Addr()
}
