package db

import (
	"context"
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// albumOf devuelve el artista y el título del álbum de una pista.
func albumOf(t *testing.T, pool *pgxpool.Pool, trackID int64) (albumID int64, artist, title string) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `SELECT al.id, ar.name, al.title
		FROM tracks t JOIN albums al ON al.id = t.album_id JOIN artists ar ON ar.id = al.artist_id
		WHERE t.id = $1`, trackID).Scan(&albumID, &artist, &title)
	if err != nil {
		t.Fatalf("álbum de la pista %d: %v", trackID, err)
	}
	return albumID, artist, title
}

func libraryVersion(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var v int64
	if err := pool.QueryRow(context.Background(), "SELECT version FROM library").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func count(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// La 0002 reparte en artistas y álbumes las pistas importadas en la fase 1.
func TestCatalogMigrationGroupsExistingTracks(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	first, err := fs.ReadFile(migrationFiles, "migrations/0001_tracks.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, pool, fstest.MapFS{"migrations/0001_tracks.sql": {Data: first}}); err != nil {
		t.Fatalf("aplicar solo la 0001: %v", err)
	}
	tracks := []struct{ title, artist, albumArtist, album string }{
		{"Uno", "Grupo", "Grupo", "Disco"},
		{"Dos", "Grupo", "", "Disco"},
		{"Tres", "Grupo feat. Otro", "Grupo", "Disco"},
		{"Cuatro", "Grupo", "", ""},
		{"Cinco", "", "", ""},
	}
	ids := make([]int64, len(tracks))
	for i, tr := range tracks {
		err := pool.QueryRow(ctx, `INSERT INTO tracks (title, artist, album_artist, album, duration_ms, source_sha256)
			VALUES ($1, $2, $3, $4, 1000, $5) RETURNING id`,
			tr.title, tr.artist, tr.albumArtist, tr.album, fmt.Appendf(make([]byte, 0, 32), "%032d", i)).Scan(&ids[i])
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	disc, artist, title := albumOf(t, pool, ids[0])
	if artist != "Grupo" || title != "Disco" {
		t.Errorf("pista 1 en %q/%q", artist, title)
	}
	for _, i := range []int{1, 2} {
		if got, _, _ := albumOf(t, pool, ids[i]); got != disc {
			t.Errorf("la pista %q no está en el álbum de la primera", tracks[i].title)
		}
	}
	if _, artist, title := albumOf(t, pool, ids[3]); artist != "Grupo" || title != "" {
		t.Errorf("pista sin álbum en %q/%q", artist, title)
	}
	if _, artist, title := albumOf(t, pool, ids[4]); artist != "" || title != "" {
		t.Errorf("pista sin etiquetas en %q/%q", artist, title)
	}
	if a, b := count(t, pool, "artists"), count(t, pool, "albums"); a != 2 || b != 3 {
		t.Errorf("%d artistas y %d álbumes, want 2 y 3", a, b)
	}
	if v := libraryVersion(t, pool); v != 1 {
		t.Errorf("versión de la biblioteca %d, want 1", v)
	}
}

func TestInsertTrackGroupsIntoAlbums(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	insert := func(n byte, tr NewTrack) int64 {
		t.Helper()
		tr.Title = "Tono"
		tr.Duration = time.Second
		tr.SourceSHA256 = [32]byte{n}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		id, err := InsertTrack(ctx, tx, tr, []TrackFile{{Quality: QualityOriginal, Ext: "flac", Codec: "flac",
			Lossless: true, SampleRate: 44100, Bits: 16, Channels: 2, Bytes: 1, SHA256: [32]byte{n}}})
		if err != nil {
			t.Fatalf("InsertTrack: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return id
	}

	before := libraryVersion(t, pool)
	first := insert(1, NewTrack{Artist: "Grupo", AlbumArtist: "Grupo", Album: "Disco"})
	disc, _, _ := albumOf(t, pool, first)
	sameAlbum := map[string]NewTrack{
		"sin album_artist":          {Artist: "Grupo", Album: "Disco"},
		"con otro artista de pista": {Artist: "Grupo feat. Otro", AlbumArtist: "Grupo", Album: "Disco"},
	}
	n := byte(2)
	for desc, tr := range sameAlbum {
		if got, _, _ := albumOf(t, pool, insert(n, tr)); got != disc {
			t.Errorf("%s: no está en el álbum de la primera", desc)
		}
		n++
	}

	others := []struct {
		desc          string
		track         NewTrack
		artist, title string
	}{
		// Límite conocido (Base de datos): sin album_artist, el «feat.» parte el álbum.
		{"feat. sin album_artist", NewTrack{Artist: "Grupo feat. Otro", Album: "Disco"}, "Grupo feat. Otro", "Disco"},
		{"mayúsculas distintas", NewTrack{Artist: "grupo", Album: "Disco"}, "grupo", "Disco"},
		{"sin álbum", NewTrack{Artist: "Grupo"}, "Grupo", ""},
		{"sin etiquetas", NewTrack{}, "", ""},
	}
	for _, tt := range others {
		got, artist, title := albumOf(t, pool, insert(n, tt.track))
		if got == disc || artist != tt.artist || title != tt.title {
			t.Errorf("%s: álbum %d %q/%q, want otro que %d, %q/%q", tt.desc, got, artist, title, disc, tt.artist, tt.title)
		}
		n++
	}

	inserted := int64(n - 1)
	if v := libraryVersion(t, pool); v != before+inserted {
		t.Errorf("versión de la biblioteca %d, want %d (una más por pista)", v, before+inserted)
	}
}
