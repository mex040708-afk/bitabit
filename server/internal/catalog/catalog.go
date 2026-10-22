// Package catalog lee la biblioteca de la base de datos en la forma que pide el
// contrato de la API v1 (api/openapi.json). No sabe nada de HTTP: los endpoints
// lo usan y lo traducen.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/mex040708-afk/bitabit/server/internal/db"
)

// Órdenes de los álbumes (parámetro sort del contrato).
const (
	SortName  = "name"
	SortAdded = "added"
)

// ArtistRef es un artista dentro de otro objeto.
type ArtistRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// AlbumRef es un álbum dentro de otro objeto.
type AlbumRef struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// ArtistSummary es un artista de álbum y cuántos álbumes tiene.
type ArtistSummary struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	AlbumCount int    `json:"albumCount"`
}

// AlbumSummary es un álbum sin sus pistas. Year es nil si ninguna pista trae fecha.
type AlbumSummary struct {
	ID         int64     `json:"id"`
	Title      string    `json:"title"`
	Artist     ArtistRef `json:"artist"`
	Year       *int      `json:"year"`
	TrackCount int       `json:"trackCount"`
	DurationMS int64     `json:"durationMs"`
	AddedAt    time.Time `json:"addedAt"`
	HasCover   bool      `json:"hasCover"`
}

// Artist es un artista con sus álbumes.
type Artist struct {
	ID     int64          `json:"id"`
	Name   string         `json:"name"`
	Albums []AlbumSummary `json:"albums"`
}

// Album es un álbum con sus pistas.
type Album struct {
	AlbumSummary
	Tracks []Track `json:"tracks"`
}

// Track es una pista con las versiones que se pueden pedir.
type Track struct {
	ID          int64       `json:"id"`
	Title       string      `json:"title"`
	Artist      string      `json:"artist"`
	Album       AlbumRef    `json:"album"`
	AlbumArtist ArtistRef   `json:"albumArtist"`
	DiscNumber  *int        `json:"discNumber"`
	TrackNumber *int        `json:"trackNumber"`
	DurationMS  int64       `json:"durationMs"`
	Files       []TrackFile `json:"files"`
}

// TrackFile es una versión de una pista: orig o aac256.
type TrackFile struct {
	Quality    string `json:"quality"`
	Codec      string `json:"codec"`
	Lossless   bool   `json:"lossless"`
	Bits       *int   `json:"bits"`
	SampleRate int    `json:"sampleRate"`
	Channels   int    `json:"channels"`
	Bytes      int64  `json:"bytes"`
}

// SearchResult son los resultados de una búsqueda, cada lista de más a menos
// parecido.
type SearchResult struct {
	Artists []ArtistSummary `json:"artists"`
	Albums  []AlbumSummary  `json:"albums"`
	Tracks  []Track         `json:"tracks"`
}

// Version devuelve la versión de la biblioteca, que da el ETag de las respuestas.
// Se lee dentro de la misma transacción que los datos, para no servir datos viejos
// con un ETag nuevo.
func Version(ctx context.Context, q db.Querier) (int64, error) {
	var v int64
	if err := q.QueryRow(ctx, "SELECT version FROM library").Scan(&v); err != nil {
		return 0, fmt.Errorf("leer la versión de la biblioteca: %w", err)
	}
	return v, nil
}

// Artists son los artistas de álbum de la biblioteca, por nombre sin distinguir
// mayúsculas ni tildes.
func Artists(ctx context.Context, q db.Querier) ([]ArtistSummary, error) {
	rows, err := q.Query(ctx, `SELECT ar.id, ar.name, count(al.id)
		FROM artists ar JOIN albums al ON al.artist_id = ar.id
		GROUP BY ar.id, ar.name
		ORDER BY lower(immutable_unaccent(ar.name)), ar.name, ar.id`)
	if err != nil {
		return nil, fmt.Errorf("listar los artistas: %w", err)
	}
	artists, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ArtistSummary, error) {
		var a ArtistSummary
		err := row.Scan(&a.ID, &a.Name, &a.AlbumCount)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("listar los artistas: %w", err)
	}
	return artists, nil
}

// GetArtist devuelve un artista con sus álbumes, por año (los que no lo tienen, al
// final) y título. found es false si no existe.
func GetArtist(ctx context.Context, q db.Querier, id int64) (a Artist, found bool, err error) {
	err = q.QueryRow(ctx, "SELECT id, name FROM artists WHERE id = $1", id).Scan(&a.ID, &a.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artist{}, false, nil
	}
	if err != nil {
		return Artist{}, false, fmt.Errorf("leer el artista %d: %w", id, err)
	}
	a.Albums, err = loadAlbums(ctx, q, `WHERE al.artist_id = $1
		ORDER BY ta.year NULLS LAST, lower(immutable_unaccent(al.title)), al.id`, id)
	if err != nil {
		return Artist{}, false, err
	}
	return a, true, nil
}

// Albums son todos los álbumes, por título y artista (sort name) o por lo último
// importado primero (sort added).
func Albums(ctx context.Context, q db.Querier, sort string) ([]AlbumSummary, error) {
	order := "lower(immutable_unaccent(al.title)), lower(immutable_unaccent(ar.name)), al.id"
	if sort == SortAdded {
		order = "ta.added_at DESC, al.id DESC"
	}
	return loadAlbums(ctx, q, "ORDER BY "+order)
}

// GetAlbum devuelve un álbum con sus pistas, por disco y número (las que no lo tienen,
// al final) y título. found es false si no existe.
func GetAlbum(ctx context.Context, q db.Querier, id int64) (a Album, found bool, err error) {
	summaries, err := loadAlbums(ctx, q, "WHERE al.id = $1", id)
	if err != nil {
		return Album{}, false, err
	}
	if len(summaries) == 0 {
		return Album{}, false, nil
	}
	a.AlbumSummary = summaries[0]
	a.Tracks, err = loadTracks(ctx, q, `WHERE t.album_id = $1
		ORDER BY t.disc_number NULLS LAST, t.track_number NULLS LAST, lower(immutable_unaccent(t.title)), t.id`, id)
	if err != nil {
		return Album{}, false, err
	}
	return a, true, nil
}

// GetTrack devuelve una pista con sus versiones. found es false si no existe.
func GetTrack(ctx context.Context, q db.Querier, id int64) (t Track, found bool, err error) {
	tracks, err := loadTracks(ctx, q, "WHERE t.id = $1", id)
	if err != nil {
		return Track{}, false, err
	}
	if len(tracks) == 0 {
		return Track{}, false, nil
	}
	return tracks[0], true, nil
}

// CoverExt devuelve la extensión de la carátula del álbum, jpg o png. ok es false
// si el álbum no existe o no tiene carátula.
func CoverExt(ctx context.Context, q db.Querier, albumID int64) (ext string, ok bool, err error) {
	var e *string
	err = q.QueryRow(ctx, "SELECT cover_ext FROM albums WHERE id = $1", albumID).Scan(&e)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("leer la carátula del álbum %d: %w", albumID, err)
	}
	if e == nil {
		return "", false, nil
	}
	return *e, true, nil
}

// Search busca en nombres de artistas, títulos de álbumes y títulos de pistas. La
// búsqueda por palabras encuentra prefijos sin tildes; la de trigramas tolera
// erratas («beatels» encuentra «Beatles»). Cada lista va de más a menos parecida.
func Search(ctx context.Context, q db.Querier, text string, limit int) (SearchResult, error) {
	terms := searchTerms(text)
	var res SearchResult
	var err error
	if res.Artists, err = searchArtists(ctx, q, text, terms, limit); err != nil {
		return SearchResult{}, err
	}
	if res.Albums, err = loadAlbums(ctx, q, `WHERE al.search @@ to_tsquery('simple', immutable_unaccent($1))
			OR immutable_unaccent(al.title) OPERATOR(public.%) immutable_unaccent($2)
		ORDER BY greatest(public.similarity(immutable_unaccent(al.title), immutable_unaccent($2)),
			CASE WHEN al.search @@ to_tsquery('simple', immutable_unaccent($1)) THEN 1 ELSE 0 END) DESC, al.id
		LIMIT $3`, terms, text, limit); err != nil {
		return SearchResult{}, err
	}
	if res.Tracks, err = loadTracks(ctx, q, `WHERE t.search @@ to_tsquery('simple', immutable_unaccent($1))
			OR immutable_unaccent(t.title) OPERATOR(public.%) immutable_unaccent($2)
		ORDER BY greatest(public.similarity(immutable_unaccent(t.title), immutable_unaccent($2)),
			CASE WHEN t.search @@ to_tsquery('simple', immutable_unaccent($1)) THEN 1 ELSE 0 END) DESC, t.id
		LIMIT $3`, terms, text, limit); err != nil {
		return SearchResult{}, err
	}
	return res, nil
}

// searchTerms convierte el texto de búsqueda en una consulta to_tsquery con
// prefijos: «beyon nand» da «beyon:* & nand:*». Solo quedan letras y dígitos, así
// que el texto nunca rompe la sintaxis de la consulta.
func searchTerms(text string) string {
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for i, w := range words {
		words[i] = w + ":*"
	}
	return strings.Join(words, " & ")
}

// searchArtists busca artistas por palabras o por erratas, con sus álbumes contados.
func searchArtists(ctx context.Context, q db.Querier, text, terms string, limit int) ([]ArtistSummary, error) {
	rows, err := q.Query(ctx, `SELECT ar.id, ar.name, count(al.id)
		FROM artists ar JOIN albums al ON al.artist_id = ar.id
		WHERE ar.search @@ to_tsquery('simple', immutable_unaccent($1))
			OR immutable_unaccent(ar.name) OPERATOR(public.%) immutable_unaccent($2)
		GROUP BY ar.id, ar.name
		ORDER BY greatest(public.similarity(immutable_unaccent(ar.name), immutable_unaccent($2)),
			CASE WHEN ar.search @@ to_tsquery('simple', immutable_unaccent($1)) THEN 1 ELSE 0 END) DESC, ar.name, ar.id
		LIMIT $3`, terms, text, limit)
	if err != nil {
		return nil, fmt.Errorf("buscar artistas: %w", err)
	}
	artists, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ArtistSummary, error) {
		var a ArtistSummary
		err := row.Scan(&a.ID, &a.Name, &a.AlbumCount)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("buscar artistas: %w", err)
	}
	return artists, nil
}

// albumSummarySQL es la consulta de un álbum con sus datos calculados a partir de
// sus pistas: se calculan al consultar y no se guardan dos veces (Base de datos).
const albumSummarySQL = `SELECT al.id, al.title, ar.id, ar.name, ta.year, ta.track_count, ta.duration_ms,
		ta.added_at, al.cover_ext IS NOT NULL
	FROM albums al
	JOIN artists ar ON ar.id = al.artist_id
	JOIN (
		SELECT album_id,
			count(*) AS track_count,
			sum(duration_ms)::bigint AS duration_ms,
			max(imported_at) AS added_at,
			min(CASE WHEN release_date ~ '^[1-9][0-9]{3}' THEN substring(release_date FROM 1 FOR 4)::int END) AS year
		FROM tracks
		GROUP BY album_id
	) ta ON ta.album_id = al.id`

// loadAlbums lee los álbumes que cumple tail, que es texto fijo del código seguido
// de sus argumentos. Los valores de usuario van siempre como argumentos.
func loadAlbums(ctx context.Context, q db.Querier, tail string, args ...any) ([]AlbumSummary, error) {
	rows, err := q.Query(ctx, albumSummarySQL+" "+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("leer los álbumes: %w", err)
	}
	albums, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AlbumSummary, error) {
		var a AlbumSummary
		err := row.Scan(&a.ID, &a.Title, &a.Artist.ID, &a.Artist.Name, &a.Year, &a.TrackCount,
			&a.DurationMS, &a.AddedAt, &a.HasCover)
		a.AddedAt = a.AddedAt.UTC()
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("leer los álbumes: %w", err)
	}
	return albums, nil
}

// trackSQL es la consulta de una pista con su álbum y el artista del álbum.
const trackSQL = `SELECT t.id, t.title, t.artist, al.id, al.title, ar.id, ar.name,
		t.disc_number, t.track_number, t.duration_ms
	FROM tracks t
	JOIN albums al ON al.id = t.album_id
	JOIN artists ar ON ar.id = al.artist_id`

// loadTracks lee las pistas que cumple tail, cada una con sus versiones.
func loadTracks(ctx context.Context, q db.Querier, tail string, args ...any) ([]Track, error) {
	rows, err := q.Query(ctx, trackSQL+" "+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("leer las pistas: %w", err)
	}
	tracks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Track, error) {
		var t Track
		err := row.Scan(&t.ID, &t.Title, &t.Artist, &t.Album.ID, &t.Album.Title, &t.AlbumArtist.ID,
			&t.AlbumArtist.Name, &t.DiscNumber, &t.TrackNumber, &t.DurationMS)
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("leer las pistas: %w", err)
	}
	return withFiles(ctx, q, tracks)
}

// withFiles añade a cada pista sus versiones, con la original primero.
func withFiles(ctx context.Context, q db.Querier, tracks []Track) ([]Track, error) {
	if len(tracks) == 0 {
		return tracks, nil
	}
	ids := make([]int64, len(tracks))
	for i, t := range tracks {
		ids[i] = t.ID
	}
	rows, err := q.Query(ctx, `SELECT track_id, quality, codec, lossless, sample_rate, bits, channels, bytes
		FROM track_files WHERE track_id = ANY($1)
		ORDER BY track_id, CASE quality WHEN 'orig' THEN 0 ELSE 1 END`, ids)
	if err != nil {
		return nil, fmt.Errorf("leer las versiones de las pistas: %w", err)
	}
	files := make(map[int64][]TrackFile, len(tracks))
	defer rows.Close()
	for rows.Next() {
		var trackID int64
		var f TrackFile
		if err := rows.Scan(&trackID, &f.Quality, &f.Codec, &f.Lossless, &f.SampleRate, &f.Bits, &f.Channels, &f.Bytes); err != nil {
			return nil, fmt.Errorf("leer las versiones de las pistas: %w", err)
		}
		files[trackID] = append(files[trackID], f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("leer las versiones de las pistas: %w", err)
	}
	for i := range tracks {
		tracks[i].Files = files[tracks[i].ID]
		if tracks[i].Files == nil {
			tracks[i].Files = []TrackFile{}
		}
	}
	return tracks, nil
}
