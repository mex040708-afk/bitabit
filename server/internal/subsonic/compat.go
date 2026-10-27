package subsonic

import "net/http"

// Funciones que casi todo cliente Subsonic llama al arrancar aunque la biblioteca no
// tenga con qué llenarlas: sin ellas, muchos se niegan a funcionar. Devuelven la
// respuesta vacía o fija que corresponde a una cuenta única sin listas, favoritos ni
// géneros (ADR-030). No leen el catálogo.

type user struct {
	Username            string `xml:"username,attr" json:"username"`
	ScrobblingEnabled   bool   `xml:"scrobblingEnabled,attr" json:"scrobblingEnabled"`
	AdminRole           bool   `xml:"adminRole,attr" json:"adminRole"`
	SettingsRole        bool   `xml:"settingsRole,attr" json:"settingsRole"`
	DownloadRole        bool   `xml:"downloadRole,attr" json:"downloadRole"`
	UploadRole          bool   `xml:"uploadRole,attr" json:"uploadRole"`
	PlaylistRole        bool   `xml:"playlistRole,attr" json:"playlistRole"`
	CoverArtRole        bool   `xml:"coverArtRole,attr" json:"coverArtRole"`
	CommentRole         bool   `xml:"commentRole,attr" json:"commentRole"`
	PodcastRole         bool   `xml:"podcastRole,attr" json:"podcastRole"`
	StreamRole          bool   `xml:"streamRole,attr" json:"streamRole"`
	JukeboxRole         bool   `xml:"jukeboxRole,attr" json:"jukeboxRole"`
	ShareRole           bool   `xml:"shareRole,attr" json:"shareRole"`
	VideoConversionRole bool   `xml:"videoConversionRole,attr" json:"videoConversionRole"`
}

type playlists struct {
	Playlists []struct{} `xml:"playlist" json:"playlist"`
}

type starred2 struct {
	Artists []artistID3 `xml:"artist" json:"artist"`
	Albums  []albumID3  `xml:"album" json:"album"`
	Songs   []songID3   `xml:"song" json:"song"`
}

type genres struct {
	Genres []struct{} `xml:"genre" json:"genre"`
}

type scanStatus struct {
	Scanning bool  `xml:"scanning,attr" json:"scanning"`
	Count    int64 `xml:"count,attr" json:"count"`
}

// getUser: la cuenta única puede escuchar y descargar, y nada más. El nombre es el
// que mande el cliente (se ignora al autenticar, ADR-031).
func (s *server) getUser(r *http.Request) *response {
	name := r.Form.Get("username")
	if name == "" {
		name = r.Form.Get("u")
	}
	resp := s.ok()
	resp.User = &user{Username: name, StreamRole: true, DownloadRole: true, CoverArtRole: true}
	return resp
}

func (s *server) getPlaylists(*http.Request) *response {
	resp := s.ok()
	resp.Playlists = &playlists{Playlists: []struct{}{}}
	return resp
}

func (s *server) getStarred2(*http.Request) *response {
	resp := s.ok()
	resp.Starred2 = &starred2{Artists: []artistID3{}, Albums: []albumID3{}, Songs: []songID3{}}
	return resp
}

func (s *server) getGenres(*http.Request) *response {
	resp := s.ok()
	resp.Genres = &genres{Genres: []struct{}{}}
	return resp
}

// getScanStatus: la biblioteca la llena el importador de entrada/, no un escaneo que
// se pueda pedir, así que nunca hay un escaneo en curso.
func (s *server) getScanStatus(*http.Request) *response {
	resp := s.ok()
	resp.ScanStatus = &scanStatus{}
	return resp
}
