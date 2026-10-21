package db

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mex040708-afk/bitabit/server/internal/db/dbtest"
)

// testPool da un esquema temporal vacío, sin migrar.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return dbtest.Pool(t)
}

// migratedPool da un esquema temporal con todas las migraciones aplicadas.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := dbtest.Pool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return pool
}

func appliedVersions(t *testing.T, pool *pgxpool.Pool) []int {
	t.Helper()
	rows, err := pool.Query(context.Background(), "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	versions, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		t.Fatal(err)
	}
	return versions
}

// allVersions son las versiones de todas las migraciones del repositorio.
func allVersions(t *testing.T) []int {
	t.Helper()
	ms, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	versions := make([]int, len(ms))
	for i, m := range ms {
		versions[i] = m.version
	}
	return versions
}

func tableExists(t *testing.T, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestLoadMigrations(t *testing.T) {
	ms, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatalf("las migraciones del repositorio: %v", err)
	}
	if ms[0].version != 1 || ms[0].name != "tracks" || len(ms[0].checksum) != 64 {
		t.Errorf("primera migración: %+v", ms[0])
	}

	sql := &fstest.MapFile{Data: []byte("SELECT 1;")}
	bad := map[string]fstest.MapFS{
		"sin migraciones":   {},
		"hueco en la serie": {"migrations/0001_a.sql": sql, "migrations/0003_c.sql": sql},
		"no empieza en 1":   {"migrations/0002_b.sql": sql},
		"número corto":      {"migrations/1_a.sql": sql},
		"mayúsculas":        {"migrations/0001_Tracks.sql": sql},
		"sin nombre":        {"migrations/0001.sql": sql},
	}
	for desc, fsys := range bad {
		if _, err := loadMigrations(fsys); err == nil {
			t.Errorf("%s: loadMigrations no falla", desc)
		}
	}
}

func TestOpenRejectsBadConnections(t *testing.T) {
	ctx := context.Background()
	for desc, dsn := range map[string]string{
		"cadena mal formada": "port=no-es-un-numero",
		// El puerto 1 de localhost rechaza la conexión al momento.
		"servidor que no responde": "host=127.0.0.1 port=1 dbname=x user=x",
	} {
		if pool, err := Open(ctx, dsn); err == nil {
			pool.Close()
			t.Errorf("%s: Open no falla", desc)
		}
	}
}

func TestMigrateCreatesTables(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	for _, table := range []string{"tracks", "track_files", "artists", "albums", "library", "schema_migrations"} {
		if !tableExists(t, pool, table) {
			t.Errorf("falta la tabla %s", table)
		}
	}
	if got, want := appliedVersions(t, pool), allVersions(t); !slices.Equal(got, want) {
		t.Errorf("versiones aplicadas: %v, want %v", got, want)
	}
}

func TestMigrateTwiceAndConcurrently(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() { errs <- Migrate(ctx, pool) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Migrate a la vez: %v", err)
		}
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Errorf("Migrate otra vez: %v", err)
	}
	if got, want := appliedVersions(t, pool), allVersions(t); !slices.Equal(got, want) {
		t.Errorf("versiones aplicadas: %v, want %v", got, want)
	}
}

func TestMigrateRejectsEditedMigration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE schema_migrations SET checksum = 'otro' WHERE version = 1"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err == nil || !strings.Contains(err.Error(), "cambió") {
		t.Errorf("error %v, want que la migración cambió", err)
	}
}

func TestMigrateRejectsNewerDatabase(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	last := len(allVersions(t))
	if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, 'futura', 'x')", last+1); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("solo conoce hasta la %d", last)) {
		t.Errorf("error %v, want que la base es más nueva que el programa", err)
	}
}

// Si una migración falla, no queda aplicada ninguna: ni las anteriores.
func TestMigrateIsAllOrNothing(t *testing.T) {
	pool := testPool(t)
	fsys := fstest.MapFS{
		"migrations/0001_buena.sql": {Data: []byte("CREATE TABLE buena (id int); CREATE TABLE otra (id int);")},
		"migrations/0002_mala.sql":  {Data: []byte("CREATE TABLE mala (id int); SELECT * FROM no_existe;")},
	}
	if err := migrate(context.Background(), pool, fsys); err == nil {
		t.Fatal("migrate no falla con una migración rota")
	}
	for _, table := range []string{"buena", "otra", "mala", "schema_migrations"} {
		if tableExists(t, pool, table) {
			t.Errorf("la tabla %s existe y no debería", table)
		}
	}
}

func TestSchemaConstraints(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	sha := func(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }
	// Desde la 0002, toda pista va en un álbum.
	if _, err := pool.Exec(ctx, `WITH ar AS (INSERT INTO artists (name) VALUES ('Artista') RETURNING id)
		INSERT INTO albums (artist_id, title) SELECT id, 'Disco' FROM ar`); err != nil {
		t.Fatal(err)
	}
	insertTrack := func(title string, source []byte) (int64, error) {
		var id int64
		err := pool.QueryRow(ctx, `INSERT INTO tracks (album_id, title, duration_ms, source_sha256)
			VALUES ((SELECT min(id) FROM albums), $1, 1000, $2) RETURNING id`, title, source).Scan(&id)
		return id, err
	}
	const insertFile = `INSERT INTO track_files
		(track_id, quality, ext, codec, lossless, sample_rate, bits, channels, bytes, sha256, audio_md5)
		VALUES ($1, $2, $3, $4, $5, 44100, $6, 2, 1000, $7, $8)`
	md5 := make([]byte, 16)

	id, err := insertTrack("Tono", sha('a'))
	if err != nil {
		t.Fatalf("pista válida: %v", err)
	}
	if _, err := pool.Exec(ctx, insertFile, id, "orig", "flac", "flac", true, 24, sha('b'), md5); err != nil {
		t.Fatalf("original válido: %v", err)
	}
	if _, err := pool.Exec(ctx, insertFile, id, "aac256", "m4a", "aac", false, nil, sha('c'), nil); err != nil {
		t.Fatalf("AAC válido: %v", err)
	}
	lossyID, err := insertTrack("Con pérdida", sha('d'))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, insertFile, lossyID, "orig", "mp3", "mp3", false, nil, sha('e'), nil); err != nil {
		t.Fatalf("original con pérdida válido: %v", err)
	}

	const (
		unique  = "23505"
		check   = "23514"
		fk      = "23503"
		notNull = "23502"
	)
	tests := []struct {
		desc string
		sql  string
		args []any
		code string
	}{
		{"fuente repetida", `INSERT INTO tracks (album_id, title, duration_ms, source_sha256) VALUES ((SELECT min(id) FROM albums), 'X', 1, $1)`,
			[]any{sha('a')}, unique},
		{"título vacío", `INSERT INTO tracks (album_id, title, duration_ms, source_sha256) VALUES ((SELECT min(id) FROM albums), '', 1, $1)`,
			[]any{sha('f')}, check},
		{"número de pista 0", `INSERT INTO tracks (album_id, title, duration_ms, source_sha256, track_number) VALUES ((SELECT min(id) FROM albums), 'X', 1, $1, 0)`,
			[]any{sha('f')}, check},
		{"duración 0", `INSERT INTO tracks (album_id, title, duration_ms, source_sha256) VALUES ((SELECT min(id) FROM albums), 'X', 0, $1)`,
			[]any{sha('f')}, check},
		{"SHA-256 corto", `INSERT INTO tracks (album_id, title, duration_ms, source_sha256) VALUES ((SELECT min(id) FROM albums), 'X', 1, $1)`,
			[]any{sha('f')[:31]}, check},
		{"calidad desconocida", insertFile, []any{lossyID, "hires", "flac", "flac", true, 24, sha('g'), nil}, check},
		{"extensión en mayúsculas", insertFile, []any{lossyID, "aac256", "M4A", "aac", false, nil, sha('g'), nil}, check},
		{"aac256 en flac", insertFile, []any{lossyID, "aac256", "flac", "aac", false, nil, sha('g'), nil}, check},
		{"aac256 sin pérdida", insertFile, []any{lossyID, "aac256", "m4a", "alac", true, 24, sha('g'), nil}, check},
		{"sin pérdida y sin bits", insertFile, []any{id, "orig", "flac", "flac", true, nil, sha('g'), nil}, check},
		{"con pérdida y con bits", insertFile, []any{lossyID, "aac256", "m4a", "aac", false, 16, sha('g'), nil}, check},
		{"MD5 con pérdida", insertFile, []any{lossyID, "aac256", "m4a", "aac", false, nil, sha('g'), md5}, check},
		{"MD5 corto", insertFile, []any{id, "orig", "flac", "flac", true, 16, sha('g'), md5[:15]}, check},
		{"versión repetida", insertFile, []any{id, "orig", "flac", "flac", true, 16, sha('g'), nil}, unique},
		{"pista que no existe", insertFile, []any{int64(999999), "orig", "flac", "flac", true, 16, sha('g'), nil}, fk},
		{"pista sin álbum", `INSERT INTO tracks (title, duration_ms, source_sha256) VALUES ('X', 1, $1)`,
			[]any{sha('f')}, notNull},
		{"álbum que no existe", `INSERT INTO tracks (album_id, title, duration_ms, source_sha256) VALUES (999999, 'X', 1, $1)`,
			[]any{sha('f')}, fk},
		{"artista repetido", `INSERT INTO artists (name) VALUES ('Artista')`, nil, unique},
		{"álbum repetido", `INSERT INTO albums (artist_id, title) SELECT artist_id, title FROM albums`, nil, unique},
		{"artista de álbum que no existe", `INSERT INTO albums (artist_id, title) VALUES (999999, 'X')`, nil, fk},
		{"carátula en gif", `UPDATE albums SET cover_ext = 'gif'`, nil, check},
		{"segunda fila de library", `INSERT INTO library (id) VALUES (2)`, nil, check},
		{"versión 0", `UPDATE library SET version = 0`, nil, check},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			_, err := pool.Exec(ctx, tt.sql, tt.args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tt.code {
				t.Errorf("error %v, want SQLSTATE %s", err, tt.code)
			}
		})
	}

	// Borrar una pista borra sus versiones.
	if _, err := pool.Exec(ctx, "DELETE FROM tracks WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM track_files WHERE track_id = $1", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("quedan %d versiones de una pista borrada", n)
	}
}
