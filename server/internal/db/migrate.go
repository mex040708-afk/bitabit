package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrateLockKey identifica el candado de PostgreSQL que impide que dos procesos
// migren a la vez. Son los bytes de "bitabit" seguidos de un 1.
const migrateLockKey int64 = 0x6269746162697401

var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

type migration struct {
	version  int
	name     string
	sql      string
	checksum string // SHA-256 del archivo, en hexadecimal
}

// Migrate aplica en una sola transacción las migraciones que falten. Falla si
// una ya aplicada ha cambiado (no se editan) o si la base de datos tiene una
// versión que este programa no conoce (el binario sería más viejo que la base).
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return migrate(ctx, pool, migrationFiles)
}

// migrate recibe las migraciones como parámetro para que los tests puedan
// probar con otras, por ejemplo una que falla.
func migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) (err error) {
	migrations, err := loadMigrations(fsys)
	if err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migrar: empezar la transacción: %w", err)
	}
	defer func() {
		if rerr := tx.Rollback(ctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("migrar: deshacer la transacción: %w", rerr))
		}
	}()

	// El candado se suelta solo al acabar la transacción.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrateLockKey); err != nil {
		return fmt.Errorf("migrar: tomar el candado: %w", err)
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    integer     PRIMARY KEY,
		name       text        NOT NULL,
		checksum   text        NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("migrar: crear schema_migrations: %w", err)
	}

	applied, err := appliedChecksums(ctx, tx)
	if err != nil {
		return err
	}
	for version, checksum := range applied {
		if version < 1 || version > len(migrations) {
			return fmt.Errorf("migrar: la base de datos tiene la migración %d y este programa solo conoce hasta la %d", version, len(migrations))
		}
		if m := migrations[version-1]; checksum != m.checksum {
			return fmt.Errorf("migrar: la migración %04d_%s cambió después de aplicarse", m.version, m.name)
		}
	}

	for _, m := range migrations {
		if _, done := applied[m.version]; done {
			continue
		}
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			return fmt.Errorf("migrar: aplicar %04d_%s: %w", m.version, m.name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)",
			m.version, m.name, m.checksum); err != nil {
			return fmt.Errorf("migrar: apuntar %04d_%s: %w", m.version, m.name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("migrar: confirmar: %w", err)
	}
	return nil
}

func appliedChecksums(ctx context.Context, tx pgx.Tx) (map[int]string, error) {
	rows, err := tx.Query(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("migrar: leer schema_migrations: %w", err)
	}
	applied := map[int]string{}
	var version int
	var checksum string
	if _, err := pgx.ForEachRow(rows, []any{&version, &checksum}, func() error {
		applied[version] = checksum
		return nil
	}); err != nil {
		return nil, fmt.Errorf("migrar: leer schema_migrations: %w", err)
	}
	return applied, nil
}

// loadMigrations lee migrations/*.sql de fsys. Exige números seguidos desde
// 0001, para que un hueco o un número repetido se vea al compilar los tests y
// no en el servidor.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	names, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("listar las migraciones: %w", err)
	}
	// fs.Glob las devuelve en orden alfabético, que con cuatro cifras es el numérico.
	var migrations []migration
	for i, name := range names {
		parts := migrationName.FindStringSubmatch(path.Base(name))
		if parts == nil {
			return nil, fmt.Errorf("migración con nombre no válido: %s (se espera NNNN_nombre.sql)", name)
		}
		version, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("migración %s: %w", name, err)
		}
		if version != i+1 {
			return nil, fmt.Errorf("migración %s: se esperaba la %04d", name, i+1)
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("leer la migración %s: %w", name, err)
		}
		sum := sha256.Sum256(data)
		migrations = append(migrations, migration{
			version:  version,
			name:     parts[2],
			sql:      string(data),
			checksum: hex.EncodeToString(sum[:]),
		})
	}
	if len(migrations) == 0 {
		return nil, errors.New("no hay migraciones")
	}
	return migrations, nil
}
