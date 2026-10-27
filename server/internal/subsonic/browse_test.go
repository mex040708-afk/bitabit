package subsonic

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/catalog"
	"github.com/mex040708-afk/bitabit/server/internal/db"
	"github.com/mex040708-afk/bitabit/server/internal/db/dbtest"
	"github.com/mex040708-afk/bitabit/server/internal/stream"
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

// getAlbum, getSong, getAlbumList2, search3 y getCoverArt sobre un catálogo real.
func TestLibraryFunctionsWithTheCatalog(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	flac := db.TrackFile{Quality: db.QualityOriginal, Ext: "flac", Codec: "flac", Lossless: true, SampleRate: 96000, Bits: 24, Channels: 2, Bytes: 12_000_000, SHA256: [32]byte{1}}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	trackID, err := db.InsertTrack(ctx, tx, db.NewTrack{Title: "Canción uno", Artist: "Grupo", Album: "Disco", ReleaseDate: "1977-05-25",
		TrackNumber: 3, DiscNumber: 1, Duration: 2 * time.Minute, SourceSHA256: [32]byte{1}}, []db.TrackFile{flac})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var albumID int64
	if err := pool.QueryRow(ctx, "SELECT album_id FROM tracks WHERE id = $1", trackID).Scan(&albumID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE albums SET cover_ext = 'jpg' WHERE id = $1", albumID); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "covers"), 0o755); err != nil {
		t.Fatal(err)
	}
	cover, thumb := []byte("\xff\xd8\xff\xe0original"), []byte("\xff\xd8\xff\xe0miniatura")
	if err := os.WriteFile(filepath.Join(dir, "covers", fmt.Sprintf("%d.jpg", albumID)), cover, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "covers", fmt.Sprintf("%d-256.jpg", albumID)), thumb, 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })

	h := New(Config{Library: root, Pool: pool, Auth: &fakeAuth{}, ServerVersion: "prueba", Log: quietLog()})
	creds := func(extra url.Values) url.Values {
		extra.Set("apiKey", "bueno")
		extra.Set("f", "json")
		return extra
	}
	albumIDText, trackIDText := fmt.Sprint(albumID), fmt.Sprint(trackID)

	album := jsonBody(t, get(t, h, http.MethodGet, "/rest/getAlbum", creds(url.Values{"id": {albumIDText}})))["album"].(map[string]any)
	songs := album["song"].([]any)
	if album["name"] != "Disco" || album["coverArt"] != albumIDText || len(songs) != 1 {
		t.Fatalf("álbum: %v", album)
	}
	song := songs[0].(map[string]any)
	if song["id"] != trackIDText || song["suffix"] != "flac" || song["contentType"] != "audio/flac" || song["duration"].(float64) != 120 ||
		song["track"].(float64) != 3 || song["bitDepth"].(float64) != 24 || song["samplingRate"].(float64) != 96000 || song["bitRate"].(float64) != 800 {
		t.Errorf("pista: %v", song)
	}

	one := jsonBody(t, get(t, h, http.MethodGet, "/rest/getSong", creds(url.Values{"id": {trackIDText}})))["song"].(map[string]any)
	if one["title"] != "Canción uno" || one["albumId"] != albumIDText {
		t.Errorf("getSong: %v", one)
	}

	list := jsonBody(t, get(t, h, http.MethodGet, "/rest/getAlbumList2", creds(url.Values{"type": {"newest"}, "size": {"5"}})))["albumList2"].(map[string]any)["album"].([]any)
	if len(list) != 1 {
		t.Errorf("getAlbumList2: %v", list)
	}
	if got := errCode(t, get(t, h, http.MethodGet, "/rest/getAlbumList2", creds(url.Values{"type": {"inventado"}}))); got != codeGeneric {
		t.Errorf("tipo desconocido: error %d", got)
	}
	if got := errCode(t, get(t, h, http.MethodGet, "/rest/getAlbumList2", creds(url.Values{"size": {"-1"}}))); got != codeGeneric {
		t.Errorf("size negativo: error %d", got)
	}

	found := jsonBody(t, get(t, h, http.MethodGet, "/rest/search3", creds(url.Values{"query": {"canci"}})))["searchResult3"].(map[string]any)
	if len(found["song"].([]any)) != 1 {
		t.Errorf("search3 por título: %v", found)
	}
	all := jsonBody(t, get(t, h, http.MethodGet, "/rest/search3", creds(url.Values{"query": {`""`}})))["searchResult3"].(map[string]any)
	if len(all["artist"].([]any)) != 1 || len(all["album"].([]any)) != 1 {
		t.Errorf("search3 vacío debe listar la biblioteca: %v", all)
	}

	for _, c := range []struct {
		size string
		want []byte
	}{{"", cover}, {"256", thumb}, {"128", thumb}, {"512", cover}} {
		form := creds(url.Values{"id": {albumIDText}})
		if c.size != "" {
			form.Set("size", c.size)
		}
		rec := get(t, h, http.MethodGet, "/rest/getCoverArt", form)
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), c.want) || rec.Header().Get("Content-Type") != "image/jpeg" {
			t.Errorf("getCoverArt size=%q: %d %q", c.size, rec.Code, rec.Body)
		}
	}
	if got := errCode(t, get(t, h, http.MethodGet, "/rest/getCoverArt", creds(url.Values{"id": {"999999"}}))); got != codeNotFound {
		t.Errorf("carátula inexistente: error %d", got)
	}
	for _, f := range []string{"getAlbum", "getSong"} {
		if got := errCode(t, get(t, h, http.MethodGet, "/rest/"+f, creds(url.Values{}))); got != codeMissingParameter {
			t.Errorf("%s sin id: error %d", f, got)
		}
	}
}

func TestChooseFile(t *testing.T) {
	tr := catalog.Track{DurationMS: 100_000, Files: []catalog.TrackFile{
		{Quality: "orig", Bytes: 10_000_000},  // 800 kbit/s
		{Quality: "aac256", Bytes: 3_200_000}, // 256 kbit/s
	}}
	cases := []struct {
		name       string
		raw        bool
		maxBitRate int
		want       string
	}{
		{"sin límite", false, 0, "orig"},
		{"límite por encima", false, 1000, "orig"},
		{"límite por debajo", false, 320, "aac256"},
		{"raw ignora el límite", true, 320, "orig"},
	}
	for _, c := range cases {
		if got, ok := chooseFile(tr, c.raw, c.maxBitRate); !ok || got.Quality != c.want {
			t.Errorf("%s: %q, %v; want %q", c.name, got.Quality, ok, c.want)
		}
	}
	onlyOrig := catalog.Track{DurationMS: 100_000, Files: tr.Files[:1]}
	if got, _ := chooseFile(onlyOrig, false, 128); got.Quality != "orig" {
		t.Errorf("sin AAC se manda el original, no %q", got.Quality)
	}
	if _, ok := chooseFile(catalog.Track{}, false, 0); ok {
		t.Error("una pista sin archivos no tiene versión")
	}
}

// stream y download redirigen a un enlace firmado de /media; scrobble no hace nada.
func TestStreamRedirectsToASignedMediaLink(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	flac := db.TrackFile{Quality: db.QualityOriginal, Ext: "flac", Codec: "flac", Lossless: true, SampleRate: 44100, Bits: 16, Channels: 2, Bytes: 10_000_000, SHA256: [32]byte{1}}
	aac := db.TrackFile{Quality: db.QualityAAC256, Ext: "m4a", Codec: "aac", SampleRate: 44100, Channels: 2, Bytes: 3_200_000, SHA256: [32]byte{2}}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	trackID, err := db.InsertTrack(ctx, tx, db.NewTrack{Title: "Uno", Artist: "Grupo", Album: "Disco", Duration: 100 * time.Second,
		SourceSHA256: [32]byte{1}}, []db.TrackFile{flac, aac})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	signer, err := stream.NewSigner([]stream.Key{{ID: "k1", Secret: bytes.Repeat([]byte{7}, 32)}})
	if err != nil {
		t.Fatal(err)
	}
	h := New(Config{Signer: signer, Pool: pool, Auth: &fakeAuth{}, ServerVersion: "prueba", Log: quietLog()})
	id := fmt.Sprint(trackID)
	for _, c := range []struct {
		name, path string
		form       url.Values
		want       string
	}{
		{"stream", "stream", url.Values{}, fmt.Sprintf("/media/%d.flac?q=orig&", trackID)},
		{"stream limitado", "stream", url.Values{"maxBitRate": {"320"}}, fmt.Sprintf("/media/%d.m4a?q=aac256&", trackID)},
		{"stream raw", "stream", url.Values{"maxBitRate": {"320"}, "format": {"raw"}}, fmt.Sprintf("/media/%d.flac?q=orig&", trackID)},
		{"download", "download", url.Values{"maxBitRate": {"320"}}, fmt.Sprintf("/media/%d.flac?q=orig&", trackID)},
	} {
		c.form.Set("apiKey", "bueno")
		c.form.Set("id", id)
		rec := get(t, h, http.MethodGet, "/rest/"+c.path, c.form)
		loc := rec.Header().Get("Location")
		if rec.Code != http.StatusFound || !strings.HasPrefix(loc, c.want) || !strings.Contains(loc, "sig=") {
			t.Errorf("%s: %d %q, want redirección a %s…", c.name, rec.Code, loc, c.want)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: el enlace firmado no debe cachearse", c.name)
		}
	}
	if got := errCode(t, get(t, h, http.MethodGet, "/rest/stream", url.Values{"apiKey": {"bueno"}, "f": {"json"}, "id": {"999999"}})); got != codeNotFound {
		t.Errorf("pista inexistente: error %d", got)
	}
	if got := errCode(t, get(t, h, http.MethodGet, "/rest/stream", url.Values{"apiKey": {"bueno"}, "f": {"json"}, "id": {id}, "maxBitRate": {"x"}})); got != codeGeneric {
		t.Errorf("maxBitRate no numérico: error %d", got)
	}
	ok := jsonBody(t, get(t, h, http.MethodGet, "/rest/scrobble", url.Values{"apiKey": {"bueno"}, "f": {"json"}, "id": {id}}))
	if ok["status"] != "ok" {
		t.Errorf("scrobble: %v", ok)
	}
}
