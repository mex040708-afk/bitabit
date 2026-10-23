package subsonic

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/catalog"
	"github.com/mex040708-afk/bitabit/server/internal/db"
	"github.com/mex040708-afk/bitabit/server/internal/db/dbtest"
)

func TestInitialGroupsByFirstLetter(t *testing.T) {
	cases := map[string]string{"Beatles": "B", "álamo": "Á", "  zeta": "Z", "1000 gecs": "#", "": "#", "¿Qué?": "#"}
	for name, want := range cases {
		if got := initial(name); got != want {
			t.Errorf("initial(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestIndexArtistsKeepsOrderAndIDsAsStrings(t *testing.T) {
	got := indexArtists([]catalog.ArtistSummary{
		{ID: 3, Name: "Abba", AlbumCount: 2}, {ID: 4, Name: "Adele", AlbumCount: 1}, {ID: 9, Name: "Bowie", AlbumCount: 5},
	})
	if len(got) != 2 || got[0].Name != "A" || len(got[0].Artists) != 2 || got[1].Name != "B" {
		t.Fatalf("índice inesperado: %+v", got)
	}
	if got[0].Artists[0].ID != "3" || got[1].Artists[0].AlbumCount != 5 {
		t.Errorf("artistas inesperados: %+v", got)
	}
	if empty := indexArtists(nil); empty == nil || len(empty) != 0 {
		t.Errorf("sin artistas debe salir una lista vacía, no nil: %#v", empty)
	}
}

func TestAlbumsID3UsesSecondsAndCoverIDs(t *testing.T) {
	year := 1977
	added := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	got := albumsID3([]catalog.AlbumSummary{
		{ID: 7, Title: "Disco", Artist: catalog.ArtistRef{ID: 3, Name: "Grupo"}, Year: &year, TrackCount: 2, DurationMS: 90500, AddedAt: added, HasCover: true},
		{ID: 8, Title: "Sin", Artist: catalog.ArtistRef{ID: 3, Name: "Grupo"}, TrackCount: 1, DurationMS: 1000, AddedAt: added},
	})
	if a := got[0]; a.ID != "7" || a.ArtistID != "3" || a.CoverArt != "7" || a.Duration != 91 || a.Year != 1977 || a.Created != "2026-10-22T12:00:00Z" {
		t.Errorf("álbum con carátula: %+v", a)
	}
	if a := got[1]; a.CoverArt != "" || a.Year != 0 {
		t.Errorf("álbum sin carátula ni año: %+v", a)
	}
}

func TestParseID(t *testing.T) {
	for raw, want := range map[string]bool{"1": true, "42": true, "0": false, "-1": false, "+3": false, "007": false, "abc": false, "": false} {
		if _, ok := parseID(raw); ok != want {
			t.Errorf("parseID(%q) ok = %v, want %v", raw, ok, want)
		}
	}
}

func TestBrowseWithTheCatalog(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	file := db.TrackFile{Quality: db.QualityOriginal, Ext: "flac", Codec: "flac", Lossless: true, SampleRate: 44100, Bits: 16, Channels: 2, Bytes: 1000, SHA256: [32]byte{1}}
	if _, err := db.InsertTrack(ctx, tx, db.NewTrack{Title: "Uno", Artist: "Grupo", Album: "Disco", ReleaseDate: "1977-05-25",
		TrackNumber: 1, DiscNumber: 1, Duration: 2 * time.Minute, SourceSHA256: [32]byte{1}}, []db.TrackFile{file}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	h := New(Config{Pool: pool, Auth: &fakeAuth{}, ServerVersion: "prueba", Log: quietLog()})
	creds := func(extra url.Values) url.Values {
		extra.Set("apiKey", "bueno")
		extra.Set("f", "json")
		return extra
	}

	folders := jsonBody(t, get(t, h, http.MethodGet, "/rest/getMusicFolders", creds(url.Values{})))
	if list := folders["musicFolders"].(map[string]any)["musicFolder"].([]any); len(list) != 1 {
		t.Errorf("carpetas: %v", folders)
	}

	artists := jsonBody(t, get(t, h, http.MethodGet, "/rest/getArtists", creds(url.Values{})))
	index := artists["artists"].(map[string]any)["index"].([]any)
	first := index[0].(map[string]any)
	artist := first["artist"].([]any)[0].(map[string]any)
	if first["name"] != "G" || artist["name"] != "Grupo" || artist["albumCount"].(float64) != 1 {
		t.Fatalf("índice: %v", artists)
	}

	one := jsonBody(t, get(t, h, http.MethodGet, "/rest/getArtist", creds(url.Values{"id": {artist["id"].(string)}})))
	albums := one["artist"].(map[string]any)["album"].([]any)
	if len(albums) != 1 || albums[0].(map[string]any)["name"] != "Disco" || albums[0].(map[string]any)["year"].(float64) != 1977 {
		t.Errorf("álbumes del artista: %v", one)
	}

	if got := errCode(t, get(t, h, http.MethodGet, "/rest/getArtist", creds(url.Values{}))); got != codeMissingParameter {
		t.Errorf("sin id: error %d, want %d", got, codeMissingParameter)
	}
	for _, id := range []string{"999999", "abc", "-1"} {
		if got := errCode(t, get(t, h, http.MethodGet, "/rest/getArtist", creds(url.Values{"id": {id}}))); got != codeNotFound {
			t.Errorf("id %q: error %d, want %d", id, got, codeNotFound)
		}
	}
}
