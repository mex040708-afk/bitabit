package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mex040708-afk/bitabit/server/internal/auth"
	"github.com/mex040708-afk/bitabit/server/internal/db"
)

const (
	passwordTimeout = 30 * time.Second
	// maxPasswordInput es lo más que se lee de la entrada. Va por encima del límite
	// de auth para que una entrada demasiado larga se rechace con su motivo.
	maxPasswordInput = 4096
	passwordUsage    = "uso: bitabit password (la contraseña se lee de la entrada estándar, no como argumento)"
)

// changePassword pone la contraseña de la cuenta. La nueva se lee de in y nunca
// va como argumento: quedaría en el historial de la shell y en la lista de
// procesos.
func changePassword(args []string, in io.Reader, out io.Writer) error {
	if len(args) != 0 {
		return errors.New(passwordUsage)
	}
	password, err := readPassword(in)
	if err != nil {
		return err
	}
	dsn, err := requiredEnv("BITABIT_DATABASE_URL")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), passwordTimeout)
	defer cancel()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	return setPassword(ctx, pool, password, out)
}

// setPassword aplica las migraciones que falten, cambia la contraseña y dice
// cuántas sesiones cerró. La salida nunca repite la contraseña.
func setPassword(ctx context.Context, pool *pgxpool.Pool, password string, out io.Writer) error {
	// Sin migrar, la tabla account no existe si el servidor nunca ha arrancado con
	// esta versión. Migrar aquí es lo mismo que hace el servidor al arrancar.
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	closed, err := auth.SetPassword(ctx, pool, password)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Contraseña guardada. Sesiones cerradas: %d.\n", closed)
	return err
}

// readPassword lee la primera línea de in, sin su fin de línea, y comprueba que
// sirve como contraseña. Los espacios de los extremos cuentan: es lo que se escribe.
func readPassword(in io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(in, maxPasswordInput)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("leer la contraseña: %w", err)
	}
	password := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if err := auth.ValidatePassword(password); err != nil {
		return "", err
	}
	return password, nil
}
