package db

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestInsertAndFindTrack(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	source := [32]byte{1, 2, 3}

	if _, found, err := TrackIDBySource(ctx, pool, source); err != nil || found {
		t.Fatalf("antes de guardar: found=%v, err=%v", found, err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	md5 := bytes.Repeat([]byte{7}, 16)
	id, err := InsertTrack(ctx, tx, NewTrack{
		Title: "Tono", Artist: "Artista", TrackNumber: 3, TrackTotal: 12,
		Duration: 2500 * time.Millisecond, SourceSHA256: source,
	}, []TrackFile{
		{Quality: QualityOriginal, Ext: "flac", Codec: "flac", Lossless: true, SampleRate: 96000, Bits: 24,
			Channels: 2, Bytes: 1000, SHA256: [32]byte{9}, AudioMD5: md5},
		{Quality: QualityAAC256, Ext: "m4a", Codec: "aac", SampleRate: 48000, Channels: 2, Bytes: 500,
			SHA256: [32]byte{8}},
	})
	if err != nil {
		t.Fatalf("InsertTrack: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	got, found, err := TrackIDBySource(ctx, pool, source)
	if err != nil || !found || got != id {
		t.Errorf("TrackIDBySource = %d, %v, %v; want %d", got, found, err, id)
	}

	// Los ceros que significan «no consta» se guardan como NULL.
	var discNumber, aacBits *int16
	var duration int
	var aacMD5 []byte
	if err := pool.QueryRow(ctx, "SELECT disc_number, duration_ms FROM tracks WHERE id = $1", id).
		Scan(&discNumber, &duration); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT bits, audio_md5 FROM track_files WHERE track_id = $1 AND quality = 'aac256'", id).
		Scan(&aacBits, &aacMD5); err != nil {
		t.Fatal(err)
	}
	if discNumber != nil || aacBits != nil || aacMD5 != nil || duration != 2500 {
		t.Errorf("disc_number=%v bits=%v audio_md5=%v duration_ms=%d", discNumber, aacBits, aacMD5, duration)
	}

	exists, err := ExistingTrackIDs(ctx, pool, []int64{id, id + 1000})
	if err != nil {
		t.Fatal(err)
	}
	if !exists[id] || exists[id+1000] {
		t.Errorf("ExistingTrackIDs = %v", exists)
	}
}

// Si falla una versión, no queda ni la pista.
func TestInsertTrackIsAtomic(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = InsertTrack(ctx, tx, NewTrack{Title: "Tono", Duration: time.Second, SourceSHA256: [32]byte{1}},
		[]TrackFile{{Quality: "hires", Ext: "flac", Codec: "flac", Lossless: true, SampleRate: 1, Bits: 16,
			Channels: 1, Bytes: 1}})
	if err == nil {
		t.Fatal("InsertTrack acepta una calidad desconocida")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM tracks").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("quedan %d pistas", n)
	}
}
