package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// insertTestTrack importa una pista de prueba con sus etiquetas y devuelve su ID.
func insertTestTrack(t *testing.T, pool *pgxpool.Pool, n byte, tr NewTrack) int64 {
	t.Helper()
	ctx := context.Background()
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

// La primera carátula guardada gana: un segundo intento no la cambia.
func TestSetAlbumCoverOnlyWhenMissing(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	id := insertTestTrack(t, pool, 1, NewTrack{Artist: "Grupo", Album: "Disco"})
	album, hasCover, err := AlbumOfTrack(ctx, pool, id)
	if err != nil {
		t.Fatal(err)
	}
	if hasCover {
		t.Fatal("un álbum recién importado tiene carátula")
	}
	if err := SetAlbumCover(ctx, pool, album, "jpg"); err != nil {
		t.Fatalf("SetAlbumCover: %v", err)
	}
	if err := SetAlbumCover(ctx, pool, album, "png"); err == nil {
		t.Error("SetAlbumCover pisó una carátula que ya había")
	}
	if _, hasCover, err := AlbumOfTrack(ctx, pool, id); err != nil || !hasCover {
		t.Errorf("después de guardarla: hasCover %v, error %v", hasCover, err)
	}
	var ext string
	if err := pool.QueryRow(ctx, "SELECT cover_ext FROM albums WHERE id = $1", album).Scan(&ext); err != nil {
		t.Fatal(err)
	}
	if ext != "jpg" {
		t.Errorf("cover_ext %q, want jpg", ext)
	}
}

// AlbumCovers solo devuelve los álbumes pedidos que existen y tienen carátula.
func TestAlbumCovers(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	withCover, _, err := AlbumOfTrack(ctx, pool, insertTestTrack(t, pool, 1, NewTrack{Artist: "Grupo", Album: "Uno"}))
	if err != nil {
		t.Fatal(err)
	}
	without, _, err := AlbumOfTrack(ctx, pool, insertTestTrack(t, pool, 2, NewTrack{Artist: "Grupo", Album: "Dos"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := SetAlbumCover(ctx, pool, withCover, "png"); err != nil {
		t.Fatal(err)
	}
	got, err := AlbumCovers(ctx, pool, []int64{withCover, without, 999999})
	if err != nil {
		t.Fatalf("AlbumCovers: %v", err)
	}
	if len(got) != 1 || got[withCover] != "png" {
		t.Errorf("AlbumCovers = %v, want {%d: png}", got, withCover)
	}
	if got, err := AlbumCovers(ctx, pool, nil); err != nil || len(got) != 0 {
		t.Errorf("AlbumCovers(nil) = %v, %v; want vacío, sin error", got, err)
	}
}

// Un formato que no es jpg ni png, o un álbum que no existe, da error y no deja nada.
func TestSetAlbumCoverRejectsBadInput(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	id := insertTestTrack(t, pool, 1, NewTrack{Artist: "Grupo", Album: "Disco"})
	album, _, err := AlbumOfTrack(ctx, pool, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetAlbumCover(ctx, pool, album, "gif"); err == nil {
		t.Error("guardó una carátula gif")
	}
	if err := SetAlbumCover(ctx, pool, 999999, "jpg"); err == nil {
		t.Error("guardó una carátula en un álbum que no existe")
	}
	if _, hasCover, err := AlbumOfTrack(ctx, pool, id); err != nil || hasCover {
		t.Errorf("el álbum tiene carátula tras los errores: hasCover %v, error %v", hasCover, err)
	}
}
