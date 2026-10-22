package catalog

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mex040708-afk/bitabit/server/internal/db"
	"github.com/mex040708-afk/bitabit/server/internal/db/dbtest"
)

// testPool da una base de datos migrada y vacía.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := dbtest.Pool(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return pool
}

// insert guarda una pista por el camino real de importación, con una versión FLAC
// si no se pasan versiones, y devuelve su id.
func insert(t *testing.T, pool *pgxpool.Pool, n byte, tr db.NewTrack, files ...db.TrackFile) int64 {
	t.Helper()
	ctx := context.Background()
	tr.SourceSHA256 = [32]byte{n}
	if tr.Title == "" {
		tr.Title = fmt.Sprintf("Pista %d", n)
	}
	if tr.Duration == 0 {
		tr.Duration = time.Minute
	}
	if len(files) == 0 {
		files = []db.TrackFile{{Quality: db.QualityOriginal, Ext: "flac", Codec: "flac", Lossless: true,
			SampleRate: 44100, Bits: 16, Channels: 2, Bytes: 1000, SHA256: [32]byte{n}}}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	id, err := db.InsertTrack(ctx, tx, tr, files)
	if err != nil {
		t.Fatalf("InsertTrack: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return id
}

func artistNames(list []ArtistSummary) []string {
	names := make([]string, len(list))
	for i, a := range list {
		names[i] = a.Name
	}
	return names
}

func albumTitles(list []AlbumSummary) []string {
	titles := make([]string, len(list))
	for i, a := range list {
		titles[i] = a.Title
	}
	return titles
}

func trackTitles(list []Track) []string {
	titles := make([]string, len(list))
	for i, tr := range list {
		titles[i] = tr.Title
	}
	return titles
}

func TestArtistsSortIgnoringCaseAndAccents(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	insert(t, pool, 1, db.NewTrack{Artist: "Zed", Album: "A"})
	insert(t, pool, 2, db.NewTrack{Artist: "beatles", Album: "B"})
	insert(t, pool, 3, db.NewTrack{Artist: "Émile", Album: "C"})

	got, err := Artists(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"beatles", "Émile", "Zed"}; !slices.Equal(artistNames(got), want) {
		t.Errorf("artistas %v, want %v", artistNames(got), want)
	}
	for _, a := range got {
		if a.AlbumCount != 1 {
			t.Errorf("%s tiene %d álbumes, want 1", a.Name, a.AlbumCount)
		}
	}
}

func TestAlbumsSortByNameOrByLatestImport(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	insert(t, pool, 1, db.NewTrack{Artist: "X", Album: "Beta"})
	insert(t, pool, 2, db.NewTrack{Artist: "Y", Album: "alfa"})
	insert(t, pool, 3, db.NewTrack{Artist: "X", Album: "Gamma"})

	byName, err := Albums(ctx, pool, SortName)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"alfa", "Beta", "Gamma"}; !slices.Equal(albumTitles(byName), want) {
		t.Errorf("por nombre %v, want %v", albumTitles(byName), want)
	}
	byAdded, err := Albums(ctx, pool, SortAdded)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Gamma", "alfa", "Beta"}; !slices.Equal(albumTitles(byAdded), want) {
		t.Errorf("por fecha de importación %v, want %v", albumTitles(byAdded), want)
	}
	for _, a := range byName {
		if a.HasCover || a.TrackCount != 1 || a.AddedAt.IsZero() {
			t.Errorf("álbum %q: %+v", a.Title, a)
		}
	}
}

// El año es el más antiguo de las fechas de las pistas; la duración, la suma.
func TestAlbumYearAndDurationAreComputedFromTracks(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	insert(t, pool, 1, db.NewTrack{Artist: "Grupo", Album: "Disco", ReleaseDate: "1977-05-25", Duration: 2 * time.Minute})
	insert(t, pool, 2, db.NewTrack{Artist: "Grupo", Album: "Disco", Duration: 3 * time.Minute})
	insert(t, pool, 3, db.NewTrack{Artist: "Grupo", Album: "Disco", ReleaseDate: "1975", Duration: time.Minute})
	insert(t, pool, 4, db.NewTrack{Artist: "Grupo", Album: "Sin fecha", Duration: time.Minute})

	albums, err := Albums(ctx, pool, SortName)
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]AlbumSummary{}
	for _, a := range albums {
		byTitle[a.Title] = a
	}
	disco := byTitle["Disco"]
	if disco.Year == nil || *disco.Year != 1975 {
		t.Errorf("año de Disco %v, want 1975", disco.Year)
	}
	if disco.TrackCount != 3 || disco.DurationMS != int64(6*time.Minute/time.Millisecond) {
		t.Errorf("Disco: %d pistas y %d ms", disco.TrackCount, disco.DurationMS)
	}
	if sin := byTitle["Sin fecha"]; sin.Year != nil {
		t.Errorf("Sin fecha tiene año %d, want nulo", *sin.Year)
	}
}

// Las pistas van por disco y número, sin número al final, y después por título.
func TestAlbumTracksAreOrderedByDiscAndNumber(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	insert(t, pool, 1, db.NewTrack{Artist: "G", Album: "Orden", Title: "C", DiscNumber: 1, TrackNumber: 2})
	insert(t, pool, 2, db.NewTrack{Artist: "G", Album: "Orden", Title: "A", DiscNumber: 1, TrackNumber: 1})
	insert(t, pool, 3, db.NewTrack{Artist: "G", Album: "Orden", Title: "B"})
	insert(t, pool, 4, db.NewTrack{Artist: "G", Album: "Orden", Title: "D", DiscNumber: 2, TrackNumber: 1})
	insert(t, pool, 5, db.NewTrack{Artist: "G", Album: "Orden", Title: "E", DiscNumber: 1})

	albums, err := Albums(ctx, pool, SortName)
	if err != nil || len(albums) != 1 {
		t.Fatalf("Albums = %d álbumes, %v", len(albums), err)
	}
	album, found, err := GetAlbum(ctx, pool, albums[0].ID)
	if err != nil || !found {
		t.Fatalf("Album = %v, %v", found, err)
	}
	if want := []string{"A", "C", "E", "D", "B"}; !slices.Equal(trackTitles(album.Tracks), want) {
		t.Errorf("pistas %v, want %v", trackTitles(album.Tracks), want)
	}
}

// La versión original va primero, aunque aac256 se ordene antes por nombre.
func TestTrackFilesPutOriginalFirst(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	sum := func(b byte) [32]byte { return [32]byte{b} }
	id := insert(t, pool, 1, db.NewTrack{Artist: "G", Album: "Dos versiones", Title: "Pista"},
		db.TrackFile{Quality: db.QualityAAC256, Ext: "m4a", Codec: "aac", SampleRate: 44100, Channels: 2, Bytes: 500, SHA256: sum(9)},
		db.TrackFile{Quality: db.QualityOriginal, Ext: "flac", Codec: "flac", Lossless: true, SampleRate: 44100, Bits: 16, Channels: 2, Bytes: 1000, SHA256: sum(8)})

	track, found, err := GetTrack(ctx, pool, id)
	if err != nil || !found {
		t.Fatalf("Track = %v, %v", found, err)
	}
	if len(track.Files) != 2 || track.Files[0].Quality != db.QualityOriginal || track.Files[1].Quality != db.QualityAAC256 {
		t.Fatalf("versiones %+v, want orig y luego aac256", track.Files)
	}
	if track.Files[1].Bits != nil {
		t.Errorf("aac256 con bits %d, want nulo", *track.Files[1].Bits)
	}
}

// La búsqueda encuentra palabras y prefijos sin tildes, y tolera erratas.
func TestSearchFindsWordsAccentsAndTypos(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	insert(t, pool, 1, db.NewTrack{Artist: "Beatles", Album: "Abbey Road", Title: "Come Together"})
	insert(t, pool, 2, db.NewTrack{Artist: "Beyoncé", Album: "Lemonade", Title: "Formation"})

	cases := []struct {
		query   string
		artists []string
		albums  []string
		tracks  []string
	}{
		{"beatels", []string{"Beatles"}, nil, nil},
		{"BEYONCE", []string{"Beyoncé"}, nil, nil},
		{"abbey", nil, []string{"Abbey Road"}, nil},
		{"come toget", nil, nil, []string{"Come Together"}},
		{"form", nil, nil, []string{"Formation"}},
	}
	for _, tt := range cases {
		res, err := Search(ctx, pool, tt.query, 20)
		if err != nil {
			t.Fatalf("Search(%q): %v", tt.query, err)
		}
		if !slices.Equal(artistNames(res.Artists), tt.artists) {
			t.Errorf("Search(%q) artistas %v, want %v", tt.query, artistNames(res.Artists), tt.artists)
		}
		if !slices.Equal(albumTitles(res.Albums), tt.albums) {
			t.Errorf("Search(%q) álbumes %v, want %v", tt.query, albumTitles(res.Albums), tt.albums)
		}
		if !slices.Equal(trackTitles(res.Tracks), tt.tracks) {
			t.Errorf("Search(%q) pistas %v, want %v", tt.query, trackTitles(res.Tracks), tt.tracks)
		}
	}
}

func TestSearchRespectsTheLimitAndAnEmptyQuery(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	insert(t, pool, 1, db.NewTrack{Artist: "Ana", Album: "Ana 1"})
	insert(t, pool, 2, db.NewTrack{Artist: "Ana Dos", Album: "Ana 2"})

	res, err := Search(ctx, pool, "ana", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Artists) != 1 || len(res.Albums) != 1 {
		t.Errorf("con límite 1: %d artistas y %d álbumes", len(res.Artists), len(res.Albums))
	}
	empty, err := Search(ctx, pool, "¿?", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Artists)+len(empty.Albums)+len(empty.Tracks) != 0 {
		t.Errorf("una búsqueda sin letras encontró algo: %+v", empty)
	}
}

func TestCoverExtFollowsTheAlbumsCover(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	insert(t, pool, 1, db.NewTrack{Artist: "G", Album: "Con carátula"})

	albums, err := Albums(ctx, pool, SortName)
	if err != nil || len(albums) != 1 {
		t.Fatalf("Albums = %d, %v", len(albums), err)
	}
	id := albums[0].ID
	if _, ok, err := CoverExt(ctx, pool, id); err != nil || ok {
		t.Errorf("sin carátula: ok %v, %v", ok, err)
	}
	if _, err := pool.Exec(ctx, "UPDATE albums SET cover_ext = 'jpg' WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}
	ext, ok, err := CoverExt(ctx, pool, id)
	if err != nil || !ok || ext != "jpg" {
		t.Errorf("con carátula: %q, %v, %v", ext, ok, err)
	}
	if after, _ := Albums(ctx, pool, SortName); len(after) != 1 || !after[0].HasCover {
		t.Errorf("hasCover no sigue a la carátula: %+v", after)
	}
}

func TestMissingRowsAreNotFound(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, found, err := GetArtist(ctx, pool, 999); err != nil || found {
		t.Errorf("Artist(999) = %v, %v", found, err)
	}
	if _, found, err := GetAlbum(ctx, pool, 999); err != nil || found {
		t.Errorf("Album(999) = %v, %v", found, err)
	}
	if _, found, err := GetTrack(ctx, pool, 999); err != nil || found {
		t.Errorf("Track(999) = %v, %v", found, err)
	}
}
