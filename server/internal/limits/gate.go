// Package limits acota la carga del servidor (ADR-040): colas cortas con tiempo
// máximo que responden 503 cuando no hay hueco, y un límite de ritmo por IP. Solo
// librería estándar.
package limits

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ErrBusy significa que no hubo hueco en el plazo de espera.
var ErrBusy = errors.New("sin hueco: el servidor está ocupado")

// Gate deja pasar como mucho n a la vez. Quien llega con todo ocupado espera, pero
// no más de lo que diga Acquire: no hay colas sin límite.
type Gate struct {
	slots chan struct{}
}

// NewGate crea una compuerta de n plazas. n debe ser al menos 1.
func NewGate(n int) *Gate {
	if n < 1 {
		panic(fmt.Sprintf("limits: compuerta de %d plazas", n))
	}
	return &Gate{slots: make(chan struct{}, n)}
}

// Acquire espera una plaza hasta wait. Devuelve la función que la libera, que hay que
// llamar una vez. Con el plazo agotado devuelve ErrBusy; con el contexto cancelado,
// el error del contexto.
func (g *Gate) Acquire(ctx context.Context, wait time.Duration) (release func(), err error) {
	select { // sin espera si hay hueco
	case g.slots <- struct{}{}:
		return g.release, nil
	default:
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case g.slots <- struct{}{}:
		return g.release, nil
	case <-timer.C:
		return nil, ErrBusy
	case <-ctx.Done():
		return nil, fmt.Errorf("esperar hueco: %w", ctx.Err())
	}
}

func (g *Gate) release() { <-g.slots }

// InUse dice cuántas plazas hay ocupadas.
func (g *Gate) InUse() int { return len(g.slots) }

// Cap es el número de plazas.
func (g *Gate) Cap() int { return cap(g.slots) }

// RetryAfterSeconds da el valor de la cabecera Retry-After: segundos enteros, al
// menos 1.
func RetryAfterSeconds(d time.Duration) string {
	return strconv.Itoa(max(1, int((d+time.Second-1)/time.Second)))
}

// Middleware pasa a next solo las peticiones que consigan plaza en wait; el resto
// recibe 503 con Retry-After. La plaza se mantiene hasta que next acaba, así que una
// descarga de audio ocupa la suya mientras dure.
func Middleware(g *Gate, wait, retryAfter time.Duration) func(http.Handler) http.Handler {
	retry := RetryAfterSeconds(retryAfter)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			release, err := g.Acquire(r.Context(), wait)
			if err != nil {
				if r.Context().Err() == nil {
					Busy(w, retry)
				}
				return
			}
			defer release()
			next.ServeHTTP(w, r)
		})
	}
}

// Busy responde el 503 de «ocupado, reintenta». Los clientes lo distinguen de un
// fallo por la cabecera Retry-After.
func Busy(w http.ResponseWriter, retryAfterSeconds string) {
	w.Header().Set("Retry-After", retryAfterSeconds)
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "servidor ocupado; reintenta en unos segundos", http.StatusServiceUnavailable)
}
