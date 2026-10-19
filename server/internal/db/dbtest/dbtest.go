// Package dbtest da a los tests una base de datos PostgreSQL aislada.
package dbtest

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool devuelve un pool que trabaja en un esquema temporal propio del test, que
// se borra al terminar. Salta el test si no está BITABIT_TEST_DATABASE_URL; en
// el servidor de pruebas vale "host=/var/run/postgresql dbname=bitabit".
func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("BITABIT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("hace falta BITABIT_TEST_DATABASE_URL con un PostgreSQL de pruebas")
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("conectar con PostgreSQL: %v", err)
	}
	schema := "test_" + strings.ToLower(rand.Text())
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatalf("crear el esquema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE"); err != nil {
			t.Errorf("borrar el esquema %s: %v", schema, err)
		}
		if err := admin.Close(ctx); err != nil {
			t.Errorf("cerrar la conexión: %v", err)
		}
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("crear el pool: %v", err)
	}
	// Las limpiezas van al revés: el pool se cierra antes del DROP.
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("conectar al esquema %s: %v", schema, err)
	}
	return pool
}
