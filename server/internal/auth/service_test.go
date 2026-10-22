package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mex040708-afk/bitabit/server/internal/db"
	"github.com/mex040708-afk/bitabit/server/internal/db/dbtest"
)

// testService da un servicio con la contraseña ya puesta, sobre un esquema
// temporal. El reloj del servicio solo avanza cuando el test lo mueve.
func testService(t *testing.T) (*Service, *pgxpool.Pool, *time.Time) {
	t.Helper()
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := SetPassword(ctx, pool, testPassword); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	clock := start
	s := NewService(pool)
	s.now = func() time.Time { return clock }
	return s, pool, &clock
}

// login abre una sesión desde testIP y devuelve su token.
func login(t *testing.T, s *Service, device string) string {
	t.Helper()
	token, _, err := s.Login(context.Background(), testIP, testPassword, device)
	if err != nil {
		t.Fatalf("Login(%q): %v", device, err)
	}
	return token
}

// sessionID devuelve el id de la sesión de un token.
func sessionID(t *testing.T, pool *pgxpool.Pool, token string) int64 {
	t.Helper()
	sum, err := tokenSum(token)
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := pool.QueryRow(context.Background(), "SELECT id FROM sessions WHERE token_sha256 = $1", sum).Scan(&id); err != nil {
		t.Fatalf("sesión del token: %v", err)
	}
	return id
}

// setLastUsed deja la sesión con su último uso hace ago, un intervalo de SQL.
func setLastUsed(t *testing.T, pool *pgxpool.Pool, id int64, ago string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), "UPDATE sessions SET last_used_at = now() - $2::interval WHERE id = $1", id, ago); err != nil {
		t.Fatal(err)
	}
}

// usedBefore dice si el último uso de la sesión es anterior a hace ago. Se
// compara con el reloj de la base de datos, el mismo que escribe last_used_at.
func usedBefore(t *testing.T, pool *pgxpool.Pool, id int64, ago string) bool {
	t.Helper()
	var before bool
	if err := pool.QueryRow(context.Background(), "SELECT last_used_at < now() - $2::interval FROM sessions WHERE id = $1", id, ago).Scan(&before); err != nil {
		t.Fatal(err)
	}
	return before
}

func TestLoginStoresOnlyTheTokenHash(t *testing.T) {
	s, pool, _ := testService(t)
	ctx := context.Background()
	token := login(t, s, "Pixel 7a")

	sum, err := tokenSum(token)
	if err != nil {
		t.Fatal(err)
	}
	var device string
	if err := pool.QueryRow(ctx, "SELECT device FROM sessions WHERE token_sha256 = $1", sum).Scan(&device); err != nil {
		t.Fatalf("no hay sesión con la suma del token: %v", err)
	}
	if device != "Pixel 7a" {
		t.Errorf("dispositivo %q, want %q", device, "Pixel 7a")
	}

	// Ninguna fila guarda el token en claro, solo su suma.
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	var stored int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE token_sha256 = $1", raw).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Error("la base de datos guarda el token en claro")
	}
}

func TestLoginReturnsTheOpenedSession(t *testing.T) {
	s, pool, _ := testService(t)
	token, sess, err := s.Login(context.Background(), testIP, testPassword, "Pixel 7a")
	if err != nil {
		t.Fatal(err)
	}
	if want := sessionID(t, pool, token); sess.ID != want {
		t.Errorf("id de la sesión %d, want %d", sess.ID, want)
	}
	if sess.Device != "Pixel 7a" || sess.CreatedAt.IsZero() || sess.LastUsedAt.IsZero() {
		t.Errorf("sesión incompleta: %+v", sess)
	}
}

// Sin contraseña, el intento cuenta como uno equivocado: la respuesta no revela
// si la cuenta existe.
func TestLoginWithoutPasswordCountsAsWrongPassword(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := NewService(pool)
	_, sess, err := s.Login(ctx, testIP, testPassword, "Pixel")
	var wrong *WrongPasswordError
	if !errors.As(err, &wrong) || wrong.AttemptsLeft != maxFailures-1 {
		t.Fatalf("Login sin contraseña: %v, want WrongPasswordError con %d intentos", err, maxFailures-1)
	}
	if (sess != Session{}) {
		t.Errorf("Login sin contraseña devolvió la sesión %+v", sess)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("hay %d sesiones sin contraseña en la cuenta, want 0", n)
	}
}

func TestLoginRejectsBadDeviceNames(t *testing.T) {
	// Sin base de datos a propósito: el nombre se comprueba antes de leer nada.
	s := NewService(nil)
	for _, device := range []string{"", strings.Repeat("d", 65), "\xff"} {
		if _, _, err := s.Login(context.Background(), testIP, testPassword, device); !errors.Is(err, ErrInvalidDevice) {
			t.Errorf("Login con el dispositivo %q: %v, want ErrInvalidDevice", device, err)
		}
	}
}

// Los nueve primeros fallos responden 401 con los intentos que quedan; el décimo,
// 429 con el bloqueo de 24 horas (ADR-014).
func TestLoginBlocksIPAfterTenFailures(t *testing.T) {
	s, _, clock := testService(t)
	ctx := context.Background()
	other := netip.MustParseAddr("198.51.100.20")

	for i := 1; i < maxFailures; i++ {
		_, _, err := s.Login(ctx, testIP, "contraseña equivocada", "Pixel")
		var wrong *WrongPasswordError
		if !errors.As(err, &wrong) || wrong.AttemptsLeft != maxFailures-i {
			t.Fatalf("fallo %d: %v, want WrongPasswordError con %d intentos", i, err, maxFailures-i)
		}
	}
	var blocked *BlockedError
	if _, _, err := s.Login(ctx, testIP, "contraseña equivocada", "Pixel"); !errors.As(err, &blocked) || blocked.RetryAfter != blockFor {
		t.Fatalf("décimo fallo: %v, want BlockedError de %v", err, blockFor)
	}
	// Bloqueada: ni la contraseña buena entra.
	if _, _, err := s.Login(ctx, testIP, testPassword, "Pixel"); !errors.As(err, &blocked) {
		t.Fatalf("con la contraseña buena: %v, want BlockedError", err)
	}
	if _, _, err := s.Login(ctx, other, testPassword, "Pixel"); err != nil {
		t.Errorf("otra IP: %v", err)
	}
	*clock = clock.Add(blockFor)
	if _, _, err := s.Login(ctx, testIP, testPassword, "Pixel"); err != nil {
		t.Errorf("tras 24 horas: %v", err)
	}
}

func TestAuthenticateRejectsUnknownTokens(t *testing.T) {
	s, _, _ := testService(t)
	ctx := context.Background()
	token := login(t, s, "Pixel")

	unknown, _, err := NewToken() // bien formado, pero nunca guardado
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", token[:42], strings.Repeat("A", 43), unknown} {
		if _, err := s.Authenticate(ctx, bad); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("Authenticate(%q) = %v, want ErrUnauthorized", bad, err)
		}
	}
}

// El último uso se anota como mucho una vez por hora.
func TestAuthenticateTouchesLastUseAtMostHourly(t *testing.T) {
	s, pool, _ := testService(t)
	ctx := context.Background()
	token := login(t, s, "Pixel")
	id := sessionID(t, pool, token)

	setLastUsed(t, pool, id, "30 minutes")
	if got, err := s.Authenticate(ctx, token); err != nil || got != id {
		t.Fatalf("Authenticate = %d, %v; want %d", got, err, id)
	}
	if !usedBefore(t, pool, id, "29 minutes") {
		t.Error("anotó el uso antes de que pasara una hora")
	}

	setLastUsed(t, pool, id, "2 hours")
	if _, err := s.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	if usedBefore(t, pool, id, "1 minute") {
		t.Error("no anotó el uso tras dos horas")
	}
}

func TestSessionsExpireAfter180DaysAndAreDeletedAtLogin(t *testing.T) {
	s, pool, _ := testService(t)
	ctx := context.Background()
	old := login(t, s, "Viejo")
	id := sessionID(t, pool, old)

	setLastUsed(t, pool, id, "179 days")
	if _, err := s.Authenticate(ctx, old); err != nil {
		t.Errorf("sesión usada hace 179 días: %v, want válida", err)
	}
	setLastUsed(t, pool, id, "181 days")
	if _, err := s.Authenticate(ctx, old); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("sesión usada hace 181 días: %v, want ErrUnauthorized", err)
	}

	login(t, s, "Nuevo") // iniciar sesión borra las caducadas
	var left int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE id = $1", id).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Error("el inicio de sesión no borró la sesión caducada")
	}
}

// La lista muestra solo las sesiones vigentes, de la usada más recientemente a la
// menos.
func TestSessionsListsOpenDevicesNewestFirst(t *testing.T) {
	s, pool, _ := testService(t)
	ctx := context.Background()
	login(t, s, "Viejo")
	login(t, s, "Nuevo")
	if _, err := pool.Exec(ctx, "UPDATE sessions SET last_used_at = now() - interval '1 day' WHERE device = 'Viejo'"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET last_used_at = now() - interval '181 days' WHERE device = 'Nuevo'"); err != nil {
		t.Fatal(err)
	}

	list, err := s.Sessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Device != "Viejo" {
		t.Errorf("sesiones %+v, want solo la de «Viejo»", list)
	}
}

func TestCloseSessionEndsItsToken(t *testing.T) {
	s, pool, _ := testService(t)
	ctx := context.Background()
	token := login(t, s, "Pixel")
	id := sessionID(t, pool, token)

	if ok, err := s.CloseSession(ctx, id); err != nil || !ok {
		t.Fatalf("CloseSession = %v, %v; want true", ok, err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("token de una sesión cerrada: %v, want ErrUnauthorized", err)
	}
	if ok, err := s.CloseSession(ctx, id); err != nil || ok {
		t.Errorf("cerrar otra vez = %v, %v; want false", ok, err)
	}
}

func TestSetPasswordClosesSessions(t *testing.T) {
	s, pool, _ := testService(t)
	ctx := context.Background()
	first := login(t, s, "Uno")
	second := login(t, s, "Dos")

	if _, err := SetPassword(ctx, pool, "corta"); err == nil {
		t.Error("SetPassword aceptó una contraseña de menos de 12 caracteres")
	}
	closed, err := SetPassword(ctx, pool, "otra contraseña nueva")
	if err != nil || closed != 2 {
		t.Fatalf("SetPassword cerró %d sesiones, %v; want 2", closed, err)
	}
	for _, token := range []string{first, second} {
		if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("token de una sesión cerrada: %v, want ErrUnauthorized", err)
		}
	}
	var wrong *WrongPasswordError
	if _, _, err := s.Login(ctx, testIP, testPassword, "Pixel"); !errors.As(err, &wrong) {
		t.Errorf("con la contraseña vieja: %v, want WrongPasswordError", err)
	}
	if _, _, err := s.Login(ctx, testIP, "otra contraseña nueva", "Pixel"); err != nil {
		t.Errorf("con la contraseña nueva: %v", err)
	}
}

// Si la contraseña cambia entre la comprobación y la sesión, la sesión no se abre:
// así no sobrevive al cambio.
func TestOpenSessionRefusesChangedPassword(t *testing.T) {
	_, pool, _ := testService(t)
	ctx := context.Background()
	var verified string
	if err := pool.QueryRow(ctx, "SELECT password_hash FROM account WHERE id = 1").Scan(&verified); err != nil {
		t.Fatal(err)
	}
	if _, err := SetPassword(ctx, pool, "otra contraseña nueva"); err != nil {
		t.Fatal(err)
	}

	_, err := openSession(ctx, pool, verified, make([]byte, 32), "Pixel")
	if !errors.Is(err, errPasswordChanged) {
		t.Fatalf("openSession con el hash viejo: %v, want errPasswordChanged", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("hay %d sesiones tras un inicio de sesión rechazado, want 0", n)
	}
}
