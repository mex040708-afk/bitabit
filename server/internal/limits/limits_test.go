package limits

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"
)

func TestGateWaitsThenGivesUp(t *testing.T) {
	g := NewGate(2)
	r1, err := g.Acquire(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := g.Acquire(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if g.InUse() != 2 || g.Cap() != 2 {
		t.Errorf("ocupadas %d de %d", g.InUse(), g.Cap())
	}
	start := time.Now()
	if _, err := g.Acquire(context.Background(), 50*time.Millisecond); !errors.Is(err, ErrBusy) {
		t.Fatalf("con todo ocupado: %v, want ErrBusy", err)
	}
	if time.Since(start) < 40*time.Millisecond {
		t.Error("no esperó el plazo")
	}
	r1()
	r3, err := g.Acquire(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("tras liberar debía haber hueco: %v", err)
	}
	r3()
	r2()
}

func TestGateHandsOverAFreedSlotToAWaiter(t *testing.T) {
	g := NewGate(1)
	release, _ := g.Acquire(context.Background(), time.Second)
	got := make(chan error, 1)
	go func() {
		r, err := g.Acquire(context.Background(), 2*time.Second)
		if err == nil {
			r()
		}
		got <- err
	}()
	time.Sleep(30 * time.Millisecond)
	release()
	if err := <-got; err != nil {
		t.Errorf("el que esperaba debía recibir la plaza: %v", err)
	}
}

func TestGateStopsWaitingWhenTheContextIsCancelled(t *testing.T) {
	g := NewGate(1)
	release, _ := g.Acquire(context.Background(), time.Second)
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Acquire(ctx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Errorf("error %v, want context.Canceled", err)
	}
}

func TestGateNeverExceedsItsCapacity(t *testing.T) {
	g := NewGate(3)
	var mu sync.Mutex
	inside, peak := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := g.Acquire(context.Background(), 5*time.Second)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			inside++
			peak = max(peak, inside)
			mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			inside--
			mu.Unlock()
			release()
		}()
	}
	wg.Wait()
	if peak > 3 {
		t.Errorf("hubo %d dentro a la vez, máximo 3", peak)
	}
}

func TestMiddlewareAnswers503WithRetryAfterWhenFull(t *testing.T) {
	g := NewGate(1)
	hold := make(chan struct{})
	entered := make(chan struct{})
	h := Middleware(g, 30*time.Millisecond, 7*time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-hold
	}))
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
		close(done)
	}()
	<-entered

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "7" {
		t.Errorf("lleno: %d, Retry-After %q; want 503 y 7", rec.Code, rec.Header().Get("Retry-After"))
	}
	close(hold)
	<-done

	rec = httptest.NewRecorder()
	free := Middleware(g, 30*time.Millisecond, 7*time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	free.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("con hueco: %d, want 200", rec.Code)
	}
	if g.InUse() != 0 {
		t.Errorf("quedan %d plazas ocupadas", g.InUse())
	}
}

func TestRetryAfterSecondsRoundsUpAndNeverZero(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "1", time.Millisecond: "1", time.Second: "1", 1500 * time.Millisecond: "2", 5 * time.Second: "5"} {
		if got := RetryAfterSeconds(d); got != want {
			t.Errorf("RetryAfterSeconds(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestIPLimiterBurstThenRefill(t *testing.T) {
	l := NewIPLimiter(2, 3, 10)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	ip := netip.MustParseAddr("192.0.2.1")
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow(ip); !ok {
			t.Fatalf("la ráfaga permite 3, falló la %d", i+1)
		}
	}
	ok, wait := l.Allow(ip)
	if ok || wait != 500*time.Millisecond {
		t.Fatalf("cuarta: ok=%v espera=%v; want false y 500ms", ok, wait)
	}
	if other, _ := l.Allow(netip.MustParseAddr("192.0.2.2")); !other {
		t.Error("otra IP tiene su propio cubo")
	}
	now = now.Add(500 * time.Millisecond) // una ficha
	if ok, _ := l.Allow(ip); !ok {
		t.Error("tras medio segundo debía haber una ficha")
	}
	if ok, _ := l.Allow(ip); ok {
		t.Error("solo había una ficha")
	}
	if mapped, _ := l.Allow(netip.MustParseAddr("::ffff:192.0.2.1")); mapped {
		t.Error("la IPv4 y su forma mapeada a IPv6 son la misma IP")
	}
}

func TestIPLimiterTableIsBounded(t *testing.T) {
	l := NewIPLimiter(1, 1, 8)
	for i := 0; i < 100; i++ {
		l.Allow(netip.AddrFrom4([4]byte{10, 0, byte(i / 256), byte(i)}))
	}
	if n := len(l.buckets); n > 8 {
		t.Errorf("la tabla tiene %d entradas, máximo 8", n)
	}
}

func TestRateLimitOnlyCountsMatchingRequests(t *testing.T) {
	l := NewIPLimiter(0.001, 2, 10)
	h := RateLimit(l, func(r *http.Request) bool { return r.URL.Path == "/limitado" }, RemoteIP)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	do := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "192.0.2.1:4000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for i := 0; i < 5; i++ {
		if rec := do("/libre"); rec.Code != http.StatusOK {
			t.Fatalf("lo que no aplica no se cuenta: %d", rec.Code)
		}
	}
	do("/limitado")
	do("/limitado")
	rec := do("/limitado")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("tercera: %d, Retry-After %q; want 429", rec.Code, rec.Header().Get("Retry-After"))
	}
}
