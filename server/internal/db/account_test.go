package db

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// La 0003 aplica en la base de datos las reglas de ADR-014: que el código falle
// no debe dejar guardar una contraseña en claro, un token entero ni una IP.
func TestAccountMigrationRules(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	const hash = "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ$aGFzaA"
	token := make([]byte, 32)

	insertAccount := func(id int, hash string) error {
		_, err := pool.Exec(ctx, "INSERT INTO account (id, password_hash, changed_at) VALUES ($1, $2, now())", id, hash)
		return err
	}
	insertSession := func(token []byte, device string) error {
		_, err := pool.Exec(ctx, `INSERT INTO sessions (token_sha256, device, created_at, last_used_at)
			VALUES ($1, $2, now(), now())`, token, device)
		return err
	}

	if n := count(t, pool, "account"); n != 0 {
		t.Errorf("account tiene %d filas antes de poner la contraseña, want 0", n)
	}
	if err := insertAccount(2, hash); err == nil {
		t.Error("account aceptó el id 2")
	}
	if err := insertAccount(1, "texto plano"); err == nil {
		t.Error("account aceptó una contraseña que no es Argon2id")
	}
	if err := insertAccount(1, hash); err != nil {
		t.Fatalf("account rechazó una cuenta válida: %v", err)
	}

	if err := insertSession(token[:31], "Pixel"); err == nil {
		t.Error("sessions aceptó un token de 31 bytes")
	}
	if err := insertSession(token, ""); err == nil {
		t.Error("sessions aceptó un dispositivo vacío")
	}
	if err := insertSession(token, strings.Repeat("d", 65)); err == nil {
		t.Error("sessions aceptó un dispositivo de 65 caracteres")
	}
	// El límite se cuenta en caracteres, no en bytes: cada ñ ocupa dos bytes.
	if err := insertSession(token, strings.Repeat("ñ", 64)); err != nil {
		t.Errorf("sessions rechazó un dispositivo de 64 caracteres: %v", err)
	}
	if err := insertSession(token, "Pixel"); err == nil {
		t.Error("sessions aceptó dos sesiones con el mismo token")
	}

	rows, err := pool.Query(ctx, `SELECT column_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'sessions' ORDER BY ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	columns, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	// Ninguna columna guarda una IP ni el agente de usuario (mandamiento 10).
	if want := []string{"id", "token_sha256", "device", "created_at", "last_used_at"}; !slices.Equal(columns, want) {
		t.Errorf("columnas de sessions %v, want %v", columns, want)
	}
}
