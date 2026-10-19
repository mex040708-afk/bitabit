package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Calidades de track_files: las mismas que q en los enlaces firmados.
const (
	QualityOriginal = "orig"
	QualityAAC256   = "aac256"
)

// Querier lo cumplen *pgxpool.Pool y pgx.Tx.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// NewTrack es una pista recién importada. Los números valen 0 si no constan.
type NewTrack struct {
	Title        string
	Artist       string
	Album        string
	AlbumArtist  string
	ReleaseDate  string
	TrackNumber  int
	TrackTotal   int
	DiscNumber   int
	DiscTotal    int
	Duration     time.Duration
	SourceSHA256 [32]byte
}

// TrackFile es una versión guardada de una pista.
type TrackFile struct {
	Quality    string
	Ext        string
	Codec      string
	Lossless   bool
	SampleRate int
	Bits       int // 0 si tiene pérdida
	Channels   int
	Bytes      int64
	SHA256     [32]byte
	AudioMD5   []byte // 16 bytes, o nil si no consta
}

// TrackIDBySource busca la pista importada desde un archivo con ese SHA-256.
func TrackIDBySource(ctx context.Context, q Querier, sum [32]byte) (id int64, found bool, err error) {
	err = q.QueryRow(ctx, "SELECT id FROM tracks WHERE source_sha256 = $1", sum[:]).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("buscar la pista por su fuente: %w", err)
	}
	return id, true, nil
}

// InsertTrack guarda una pista con sus versiones y devuelve su ID. Va en una
// transacción para que nunca quede una pista sin sus versiones.
func InsertTrack(ctx context.Context, tx pgx.Tx, t NewTrack, files []TrackFile) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO tracks
		(title, artist, album, album_artist, release_date,
		 track_number, track_total, disc_number, disc_total, duration_ms, source_sha256)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id`,
		t.Title, t.Artist, t.Album, t.AlbumArtist, t.ReleaseDate,
		nullIfZero(t.TrackNumber), nullIfZero(t.TrackTotal), nullIfZero(t.DiscNumber), nullIfZero(t.DiscTotal),
		durationMS(t.Duration), t.SourceSHA256[:],
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("guardar la pista: %w", err)
	}
	for _, f := range files {
		if _, err := tx.Exec(ctx, `INSERT INTO track_files
			(track_id, quality, ext, codec, lossless, sample_rate, bits, channels, bytes, sha256, audio_md5)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			id, f.Quality, f.Ext, f.Codec, f.Lossless, f.SampleRate, nullIfZero(f.Bits), f.Channels,
			f.Bytes, f.SHA256[:], f.AudioMD5,
		); err != nil {
			return 0, fmt.Errorf("guardar la versión %s de la pista %d: %w", f.Quality, id, err)
		}
	}
	return id, nil
}

// ExistingTrackIDs dice cuáles de ids tienen pista.
func ExistingTrackIDs(ctx context.Context, q Querier, ids []int64) (map[int64]bool, error) {
	rows, err := q.Query(ctx, "SELECT id FROM tracks WHERE id = ANY($1)", ids)
	if err != nil {
		return nil, fmt.Errorf("buscar pistas por ID: %w", err)
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("buscar pistas por ID: %w", err)
	}
	exists := make(map[int64]bool, len(found))
	for _, id := range found {
		exists[id] = true
	}
	return exists, nil
}

// nullIfZero guarda NULL donde el 0 significa «no consta».
func nullIfZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

// durationMS redondea hacia arriba lo que dure menos de 1 ms, que la tabla no
// admite 0.
func durationMS(d time.Duration) int64 {
	return max(1, d.Milliseconds())
}
