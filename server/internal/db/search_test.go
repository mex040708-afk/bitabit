package db

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// La 0004 deja buscar sin distinguir tildes y tolerar erratas (ADR-007).
func TestSearchMigrationFindsAccentsAndTypos(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "INSERT INTO artists (name) VALUES ('Beatles'), ('Beyoncé')"); err != nil {
		t.Fatal(err)
	}

	var unaccented string
	if err := pool.QueryRow(ctx, "SELECT immutable_unaccent('Beyoncé Ñandú')").Scan(&unaccented); err != nil {
		t.Fatal(err)
	}
	if unaccented != "Beyonce Nandu" {
		t.Errorf("immutable_unaccent = %q, want %q", unaccented, "Beyonce Nandu")
	}

	// Por palabras y con prefijo: «beyonc» encuentra «Beyoncé».
	names := func(query string, args ...any) []string {
		t.Helper()
		rows, err := pool.Query(ctx, query, args...)
		if err != nil {
			t.Fatal(err)
		}
		found, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return found
	}
	if got := names(`SELECT name FROM artists WHERE search @@ to_tsquery('simple', immutable_unaccent($1))`, "beyonc:*"); len(got) != 1 || got[0] != "Beyoncé" {
		t.Errorf("búsqueda por palabra: %v, want [Beyoncé]", got)
	}

	// Por erratas: «beatels» encuentra «Beatles».
	if got := names(`SELECT name FROM artists WHERE immutable_unaccent(name) OPERATOR(public.%) immutable_unaccent($1)`, "beatels"); len(got) != 1 || got[0] != "Beatles" {
		t.Errorf("búsqueda con erratas: %v, want [Beatles]", got)
	}
}
