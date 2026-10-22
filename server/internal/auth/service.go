package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNoPassword: la cuenta todavía no tiene contraseña (bitabit password).
	ErrNoPassword = errors.New("la cuenta no tiene contraseña")
	// ErrInvalidDevice: el nombre del dispositivo no tiene de 1 a 64 caracteres.
	ErrInvalidDevice = errors.New("el nombre del dispositivo tiene de 1 a 64 caracteres")

	// errPasswordChanged: la contraseña cambió mientras se comprobaba la de este
	// inicio de sesión.
	errPasswordChanged = errors.New("la contraseña cambió mientras se iniciaba sesión")
)

// WrongPasswordError es una contraseña incorrecta. AttemptsLeft dice cuántos
// fallos quedan antes de que la IP se bloquee.
type WrongPasswordError struct {
	AttemptsLeft int
}

func (e *WrongPasswordError) Error() string { return "contraseña incorrecta" }

// BlockedError significa que la IP está bloqueada. RetryAfter es lo que falta
// para que se desbloquee.
type BlockedError struct {
	RetryAfter time.Duration
}

func (e *BlockedError) Error() string { return "IP bloqueada" }

// Service comprueba la contraseña, abre sesiones y valida los tokens. Es seguro
// para uso concurrente.
type Service struct {
	pool *pgxpool.Pool
	lock *lockout
	now  func() time.Time // los tests lo cambian para no esperar 24 horas
}

// NewService trabaja con la base de datos de pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, lock: newLockout(), now: time.Now}
}

// Login comprueba la contraseña que envía ip y, si es correcta, abre una sesión
// para device. Devuelve el token, que el cliente guarda: la base de datos solo
// guarda su SHA-256.
func (s *Service) Login(ctx context.Context, ip netip.Addr, password, device string) (string, error) {
	// 1.2.3.4 y ::ffff:1.2.3.4 son la misma IP, y no deben tener contadores aparte.
	ip = ip.Unmap()
	now := s.now()
	if wait, blocked := s.lock.blocked(ip, now); blocked {
		return "", &BlockedError{RetryAfter: wait}
	}
	if !validDevice(device) {
		return "", ErrInvalidDevice
	}
	stored, err := s.storedHash(ctx)
	if err != nil {
		return "", err
	}
	ok, err := VerifyPassword(password, stored)
	if err != nil {
		return "", fmt.Errorf("comprobar la contraseña: %w", err)
	}
	if !ok {
		return "", &WrongPasswordError{AttemptsLeft: s.lock.failed(ip, now)}
	}
	s.lock.succeeded(ip)

	token, sum, err := NewToken()
	if err != nil {
		return "", err
	}
	if err := openSession(ctx, s.pool, stored, sum, device); err != nil {
		return "", err
	}
	return token, nil
}

// Authenticate devuelve el id de la sesión del token, o ErrUnauthorized si no
// existe o si lleva 180 días sin usarse. Anota el último uso como mucho una vez
// por hora, para no escribir en cada petición.
func (s *Service) Authenticate(ctx context.Context, token string) (int64, error) {
	sum, err := tokenSum(token)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.pool.QueryRow(ctx, `SELECT id FROM sessions
		WHERE token_sha256 = $1 AND last_used_at > now() - interval '180 days'`, sum).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrUnauthorized
	}
	if err != nil {
		return 0, fmt.Errorf("buscar la sesión: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE sessions SET last_used_at = now()
		WHERE id = $1 AND last_used_at < now() - interval '1 hour'`, id); err != nil {
		return 0, fmt.Errorf("anotar el último uso: %w", err)
	}
	return id, nil
}

// SetPassword pone la contraseña de la cuenta, o la cambia, y cierra todas las
// sesiones en la misma transacción. Devuelve cuántas sesiones cerró.
func SetPassword(ctx context.Context, pool *pgxpool.Pool, password string) (closed int64, err error) {
	if err := ValidatePassword(password); err != nil {
		return 0, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return 0, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("cambiar la contraseña: empezar la transacción: %w", err)
	}
	defer func() {
		if rerr := tx.Rollback(ctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("cambiar la contraseña: deshacer: %w", rerr))
		}
	}()

	if _, err := tx.Exec(ctx, `INSERT INTO account (id, password_hash, changed_at) VALUES (1, $1, now())
		ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash, changed_at = EXCLUDED.changed_at`, hash); err != nil {
		return 0, fmt.Errorf("cambiar la contraseña: guardarla: %w", err)
	}
	tag, err := tx.Exec(ctx, "DELETE FROM sessions")
	if err != nil {
		return 0, fmt.Errorf("cambiar la contraseña: cerrar las sesiones: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("cambiar la contraseña: confirmar: %w", err)
	}
	return tag.RowsAffected(), nil
}

// storedHash lee el hash de la contraseña. Sin cuenta, nadie puede entrar.
func (s *Service) storedHash(ctx context.Context) (string, error) {
	var hash string
	err := s.pool.QueryRow(ctx, "SELECT password_hash FROM account WHERE id = 1").Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoPassword
	}
	if err != nil {
		return "", fmt.Errorf("leer la contraseña: %w", err)
	}
	return hash, nil
}

// openSession guarda la sesión nueva. Bloquea la fila de la cuenta con FOR SHARE
// y comprueba que el hash sigue siendo el que se acaba de verificar. Así, un
// cambio de contraseña a la vez espera a que esta sesión exista y la cierra con
// las demás. Sin esa comprobación, una sesión abierta con la contraseña vieja
// podría sobrevivir al cambio.
func openSession(ctx context.Context, pool *pgxpool.Pool, verified string, sum []byte, device string) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("abrir la sesión: empezar la transacción: %w", err)
	}
	defer func() {
		if rerr := tx.Rollback(ctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("abrir la sesión: deshacer: %w", rerr))
		}
	}()

	var current string
	if err := tx.QueryRow(ctx, "SELECT password_hash FROM account WHERE id = 1 FOR SHARE").Scan(&current); err != nil {
		return fmt.Errorf("abrir la sesión: leer la cuenta: %w", err)
	}
	if current != verified {
		return errPasswordChanged
	}
	// Las sesiones sin uso en 180 días ya no valen, así que se borran aquí.
	if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE last_used_at <= now() - interval '180 days'"); err != nil {
		return fmt.Errorf("abrir la sesión: borrar las caducadas: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sessions (token_sha256, device, created_at, last_used_at)
		VALUES ($1, $2, now(), now())`, sum, device); err != nil {
		return fmt.Errorf("abrir la sesión: guardarla: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("abrir la sesión: confirmar: %w", err)
	}
	return nil
}

// validDevice dice si device sirve como nombre de dispositivo. Son los mismos
// límites que la base de datos, contados en caracteres.
func validDevice(device string) bool {
	n := utf8.RuneCountInString(device)
	return utf8.ValidString(device) && n >= 1 && n <= 64
}
