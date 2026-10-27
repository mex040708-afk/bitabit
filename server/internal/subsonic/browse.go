package subsonic

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mex040708-afk/bitabit/server/internal/catalog"
)

const (
	codeNotFound = 70

	// musicFolderID es la única carpeta de música: la biblioteca entera.
	musicFolderID   = 1
	musicFolderName = "Biblioteca"
)

type musicFolders struct {
	Folders []musicFolder `xml:"musicFolder" json:"musicFolder"`
}

type musicFolder struct {
	ID   int64  `xml:"id,attr" json:"id"`
	Name string `xml:"name,attr" json:"name"`
}

type artistsID3 struct {
	IgnoredArticles string     `xml:"ignoredArticles,attr" json:"ignoredArticles"`
	Index           []indexID3 `xml:"index" json:"index"`
}

type indexID3 struct {
	Name    string      `xml:"name,attr" json:"name"`
	Artists []artistID3 `xml:"artist" json:"artist"`
}

// artistID3 lleva los álbumes solo en getArtist.
type artistID3 struct {
	ID         string     `xml:"id,attr" json:"id"`
	Name       string     `xml:"name,attr" json:"name"`
	AlbumCount int        `xml:"albumCount,attr" json:"albumCount"`
	Albums     []albumID3 `xml:"album,omitempty" json:"album,omitempty"`
}

// albumID3 es un álbum con los nombres de campo de Subsonic. La duración va en
// segundos y los ids como cadenas, que es lo que exige la especificación.
type albumID3 struct {
	ID        string `xml:"id,attr" json:"id"`
	Name      string `xml:"name,attr" json:"name"`
	Artist    string `xml:"artist,attr" json:"artist"`
	ArtistID  string `xml:"artistId,attr" json:"artistId"`
	CoverArt  string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	SongCount int    `xml:"songCount,attr" json:"songCount"`
	Duration  int64  `xml:"duration,attr" json:"duration"`
	Created   string `xml:"created,attr" json:"created"`
	Year      int    `xml:"year,attr,omitempty" json:"year,omitempty"`
}

func (s *server) getMusicFolders(*http.Request) *response {
	resp := s.ok()
	resp.MusicFolders = &musicFolders{Folders: []musicFolder{{ID: musicFolderID, Name: musicFolderName}}}
	return resp
}

func (s *server) getArtists(r *http.Request) *response {
	artists, err := catalog.Artists(r.Context(), s.pool)
	if err != nil {
		return s.internal(r, err)
	}
	resp := s.ok()
	resp.Artists = &artistsID3{Index: indexArtists(artists)}
	return resp
}

func (s *server) getArtist(r *http.Request) *response {
	if !r.Form.Has("id") {
		return s.fail(codeMissingParameter, "Falta el parámetro id.")
	}
	id, ok := parseID(r.Form.Get("id"))
	if !ok {
		return s.fail(codeNotFound, "No existe ese artista.")
	}
	artist, found, err := catalog.GetArtist(r.Context(), s.pool, id)
	if err != nil {
		return s.internal(r, err)
	}
	if !found {
		return s.fail(codeNotFound, "No existe ese artista.")
	}
	resp := s.ok()
	resp.Artist = &artistID3{
		ID: formatID(artist.ID), Name: artist.Name, AlbumCount: len(artist.Albums),
		Albums: albumsID3(artist.Albums),
	}
	return resp
}

// parseID lee un id de la forma en que lo da formatID: un entero positivo sin
// signo ni ceros a la izquierda.
func parseID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id > 0 && raw == strconv.FormatInt(id, 10)
}

func formatID(id int64) string { return strconv.FormatInt(id, 10) }

// indexArtists agrupa los artistas por la inicial de su nombre, en el orden en
// que llegan; lo que no empieza por una letra va en «#».
func indexArtists(artists []catalog.ArtistSummary) []indexID3 {
	index := []indexID3{}
	for _, a := range artists {
		letter := initial(a.Name)
		if n := len(index); n == 0 || index[n-1].Name != letter {
			index = append(index, indexID3{Name: letter, Artists: []artistID3{}})
		}
		last := &index[len(index)-1]
		last.Artists = append(last.Artists, artistID3{ID: formatID(a.ID), Name: a.Name, AlbumCount: a.AlbumCount})
	}
	return index
}

// initial es la primera letra del nombre en mayúscula, o «#».
func initial(name string) string {
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) {
			return string(unicode.ToUpper(r))
		}
		break
	}
	return "#"
}

func albumsID3(albums []catalog.AlbumSummary) []albumID3 {
	out := make([]albumID3, 0, len(albums))
	for _, a := range albums {
		item := albumID3{
			ID: formatID(a.ID), Name: a.Title, Artist: a.Artist.Name, ArtistID: formatID(a.Artist.ID),
			SongCount: a.TrackCount, Duration: (a.DurationMS + 500) / 1000,
			Created: a.AddedAt.UTC().Format(time.RFC3339),
		}
		if a.HasCover {
			item.CoverArt = formatID(a.ID)
		}
		if a.Year != nil {
			item.Year = *a.Year
		}
		out = append(out, item)
	}
	return out
}
