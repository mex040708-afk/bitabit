package subsonic

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/mex040708-afk/bitabit/server/internal/catalog"
	"github.com/mex040708-afk/bitabit/server/internal/library"
)

const (
	defaultListSize = 10
	maxListSize     = 500
	maxOffset       = 1 << 30
	defaultSearch   = 20
)

// songID3 es una pista con los nombres de campo de Subsonic («child»). Habla del
// archivo original, o de la única versión que haya. La duración va en segundos.
type songID3 struct {
	ID           string `xml:"id,attr" json:"id"`
	Parent       string `xml:"parent,attr" json:"parent"`
	IsDir        bool   `xml:"isDir,attr" json:"isDir"`
	Title        string `xml:"title,attr" json:"title"`
	Album        string `xml:"album,attr" json:"album"`
	Artist       string `xml:"artist,attr" json:"artist"`
	Track        int    `xml:"track,attr,omitempty" json:"track,omitempty"`
	DiscNumber   int    `xml:"discNumber,attr,omitempty" json:"discNumber,omitempty"`
	CoverArt     string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Size         int64  `xml:"size,attr" json:"size"`
	ContentType  string `xml:"contentType,attr" json:"contentType"`
	Suffix       string `xml:"suffix,attr" json:"suffix"`
	Duration     int64  `xml:"duration,attr" json:"duration"`
	BitRate      int64  `xml:"bitRate,attr" json:"bitRate"`
	BitDepth     int    `xml:"bitDepth,attr,omitempty" json:"bitDepth,omitempty"`
	SamplingRate int    `xml:"samplingRate,attr" json:"samplingRate"`
	ChannelCount int    `xml:"channelCount,attr" json:"channelCount"`
	AlbumID      string `xml:"albumId,attr" json:"albumId"`
	ArtistID     string `xml:"artistId,attr" json:"artistId"`
	Type         string `xml:"type,attr" json:"type"`
}

// albumWithSongs es getAlbum: el álbum y sus pistas.
type albumWithSongs struct {
	albumID3
	Songs []songID3 `xml:"song" json:"song"`
}

type albumList struct {
	Albums []albumID3 `xml:"album" json:"album"`
}

type searchResult3 struct {
	Artists []artistID3 `xml:"artist" json:"artist"`
	Albums  []albumID3  `xml:"album" json:"album"`
	Songs   []songID3   `xml:"song" json:"song"`
}

func (s *server) getAlbum(r *http.Request) *response {
	id, bad := s.idParam(r, "álbum")
	if bad != nil {
		return bad
	}
	album, found, err := catalog.GetAlbum(r.Context(), s.pool, id)
	if err != nil {
		return s.internal(r, err)
	}
	if !found {
		return s.fail(codeNotFound, "No existe ese álbum.")
	}
	resp := s.ok()
	out := albumWithSongs{albumID3: albumsID3([]catalog.AlbumSummary{album.AlbumSummary})[0], Songs: songsID3(album.Tracks, album.HasCover)}
	resp.Album = &out
	return resp
}

func (s *server) getSong(r *http.Request) *response {
	id, bad := s.idParam(r, "pista")
	if bad != nil {
		return bad
	}
	track, found, err := catalog.GetTrack(r.Context(), s.pool, id)
	if err != nil {
		return s.internal(r, err)
	}
	if !found {
		return s.fail(codeNotFound, "No existe esa pista.")
	}
	resp := s.ok()
	// Si el álbum tiene carátula lo dice getAlbum; aquí basta con apuntar al álbum,
	// y getCoverArt responde 404 si no la hay.
	songs := songsID3([]catalog.Track{track}, true)
	if len(songs) == 0 {
		return s.fail(codeNotFound, "Esa pista no tiene archivos.")
	}
	resp.Song = &songs[0]
	return resp
}

func (s *server) getAlbumList2(r *http.Request) *response {
	sort := catalog.SortName
	switch r.Form.Get("type") {
	case "newest":
		sort = catalog.SortAdded
	case "alphabeticalByName", "alphabeticalByArtist", "":
	case "byYear", "random", "frequent", "recent", "highest", "starred":
		// Sin historial de escucha ni favoritos: el orden alfabético es lo único estable.
	default:
		return s.fail(codeGeneric, "Tipo de lista no admitido.")
	}
	albums, err := catalog.Albums(r.Context(), s.pool, sort)
	if err != nil {
		return s.internal(r, err)
	}
	size, offset, bad := s.window(r, "size", "offset", defaultListSize)
	if bad != nil {
		return bad
	}
	resp := s.ok()
	resp.AlbumList2 = &albumList{Albums: albumsID3(slice(albums, offset, size))}
	return resp
}

func (s *server) search3(r *http.Request) *response {
	query := strings.Trim(strings.TrimSpace(r.Form.Get("query")), `"*`)
	counts := [3]int{}
	offsets := [3]int{}
	for i, p := range [][2]string{{"artistCount", "artistOffset"}, {"albumCount", "albumOffset"}, {"songCount", "songOffset"}} {
		var bad *response
		counts[i], offsets[i], bad = s.window(r, p[0], p[1], defaultSearch)
		if bad != nil {
			return bad
		}
	}
	var artists []catalog.ArtistSummary
	var albums []catalog.AlbumSummary
	var tracks []catalog.Track
	var err error
	if query == "" {
		// Los clientes recorren la biblioteca con una búsqueda vacía.
		if artists, err = catalog.Artists(r.Context(), s.pool); err == nil {
			albums, err = catalog.Albums(r.Context(), s.pool, catalog.SortName)
		}
	} else {
		limit := max(counts[0]+offsets[0], counts[1]+offsets[1], counts[2]+offsets[2])
		var res catalog.SearchResult
		if res, err = catalog.Search(r.Context(), s.pool, query, min(limit, maxListSize)); err == nil {
			artists, albums, tracks = res.Artists, res.Albums, res.Tracks
		}
	}
	if err != nil {
		return s.internal(r, err)
	}
	out := searchResult3{Artists: []artistID3{}, Songs: songsID3(slice(tracks, offsets[2], counts[2]), true)}
	for _, a := range slice(artists, offsets[0], counts[0]) {
		out.Artists = append(out.Artists, artistID3{ID: formatID(a.ID), Name: a.Name, AlbumCount: a.AlbumCount})
	}
	out.Albums = albumsID3(slice(albums, offsets[1], counts[1]))
	resp := s.ok()
	resp.SearchResult3 = &out
	return resp
}

// getCoverArt manda la carátula de un álbum, o su miniatura si se pide un tamaño de
// 256 o menos y existe. El id es el del álbum, como lo da coverArt. La imagen sale de
// biblioteca/ por el Root, nunca de una ruta de la petición.
func (s *server) getCoverArt(w http.ResponseWriter, r *http.Request) *response {
	id, ok := parseID(r.Form.Get("id"))
	if !ok {
		return s.fail(codeNotFound, "No existe esa carátula.")
	}
	ext, has, err := catalog.CoverExt(r.Context(), s.pool, id)
	if err != nil {
		return s.internal(r, err)
	}
	if !has || s.library == nil {
		return s.fail(codeNotFound, "No existe esa carátula.")
	}
	contentType := map[string]string{"jpg": "image/jpeg", "png": "image/png"}[ext]
	if contentType == "" {
		return s.internal(r, fmt.Errorf("extensión de carátula %q", ext))
	}
	name := library.CoverFile(id, ext)
	if size, err := strconv.Atoi(r.Form.Get("size")); err == nil && size > 0 && size <= library.ThumbSize {
		if f, err := s.library.Open(library.ThumbFile(id)); err == nil {
			f.Close() // solo se comprueba que existe; se vuelve a abrir abajo
			name, contentType = library.ThumbFile(id), "image/jpeg"
		} else if !errors.Is(err, fs.ErrNotExist) {
			return s.internal(r, err)
		}
	}
	f, err := s.library.Open(name)
	if err != nil {
		return s.internal(r, err)
	}
	defer f.Close() // solo lectura: cerrar no puede perder datos
	info, err := f.Stat()
	if err != nil {
		return s.internal(r, err)
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, "", info.ModTime(), f)
	return nil
}

// idParam lee el parámetro id de una función. Falta: error 10; mal formado o
// inexistente: error 70.
func (s *server) idParam(r *http.Request, what string) (int64, *response) {
	if !r.Form.Has("id") {
		return 0, s.fail(codeMissingParameter, "Falta el parámetro id.")
	}
	id, ok := parseID(r.Form.Get("id"))
	if !ok {
		return 0, s.fail(codeNotFound, "No existe ese "+what+".")
	}
	return id, nil
}

// window lee un par tamaño/desplazamiento. Un valor que no es un entero no negativo
// es el error 0; el tamaño se acota a maxListSize.
func (s *server) window(r *http.Request, sizeName, offsetName string, defaultSize int) (size, offset int, bad *response) {
	size, offset = defaultSize, 0
	for _, p := range []struct {
		name string
		dst  *int
	}{{sizeName, &size}, {offsetName, &offset}} {
		if !r.Form.Has(p.name) {
			continue
		}
		n, err := strconv.Atoi(r.Form.Get(p.name))
		if err != nil || n < 0 {
			return 0, 0, s.fail(codeGeneric, "El parámetro "+p.name+" no es válido.")
		}
		*p.dst = n
	}
	// Sin tope, offset+size desbordaría con un offset cercano a MaxInt y search3
	// pasaría un límite negativo a PostgreSQL (error 500).
	return min(size, maxListSize), min(offset, maxOffset), nil
}

// slice devuelve hasta size elementos de items desde offset.
func slice[T any](items []T, offset, size int) []T {
	if offset >= len(items) {
		return nil
	}
	return items[offset:min(offset+size, len(items))]
}

// songsID3 convierte las pistas. Una pista sin archivos no se puede pedir y se omite.
func songsID3(tracks []catalog.Track, albumHasCover bool) []songID3 {
	out := []songID3{}
	for _, t := range tracks {
		if len(t.Files) == 0 {
			continue
		}
		f := t.Files[0] // la original, o la única versión (el catálogo las ordena así)
		suffix, contentType := fileKind(f)
		song := songID3{
			ID: formatID(t.ID), Parent: formatID(t.Album.ID), Title: t.Title, Album: t.Album.Title, Artist: t.Artist,
			Size: f.Bytes, ContentType: contentType, Suffix: suffix, Duration: (t.DurationMS + 500) / 1000,
			SamplingRate: f.SampleRate, ChannelCount: f.Channels,
			AlbumID: formatID(t.Album.ID), ArtistID: formatID(t.AlbumArtist.ID), Type: "music",
		}
		if t.DurationMS > 0 {
			song.BitRate = f.Bytes * 8 * 1000 / t.DurationMS / 1000
		}
		if f.Bits != nil {
			song.BitDepth = *f.Bits
		}
		if t.TrackNumber != nil {
			song.Track = *t.TrackNumber
		}
		if t.DiscNumber != nil {
			song.DiscNumber = *t.DiscNumber
		}
		if albumHasCover {
			song.CoverArt = formatID(t.Album.ID)
		}
		out = append(out, song)
	}
	return out
}

// fileKind da la extensión y el tipo MIME de una versión por su códec.
func fileKind(f catalog.TrackFile) (suffix, contentType string) {
	switch f.Codec {
	case "flac":
		return "flac", "audio/flac"
	case "aac":
		return "m4a", "audio/mp4"
	case "mp3":
		return "mp3", "audio/mpeg"
	}
	return f.Codec, "application/octet-stream"
}
