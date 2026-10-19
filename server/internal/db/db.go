// Package db conecta con PostgreSQL y aplica las migraciones.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// Un solo usuario: pocas conexiones bastan (ADR-007).
	maxConns       = 4
	connectTimeout = 5 * time.Second
)

// Open conecta con PostgreSQL y comprueba que responde. En el servidor, dsn es
// "host=/var/run/postgresql dbname=bitabit": entra por socket con
// autenticación peer, sin contraseña.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("leer la cadena de conexión: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	return connect(ctx, cfg)
}

func connect(ctx context.Context, cfg *pgxpool.Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("crear el pool de PostgreSQL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("conectar con PostgreSQL: %w", err)
	}
	return pool, nil
}
