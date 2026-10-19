package ingest

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseTags(t *testing.T) {
	tests := []struct {
		desc           string
		format, stream map[string]string
		want           Tags
	}{
		{
			desc: "claves de FLAC en mayúsculas y convertidas por FFmpeg",
			format: map[string]string{
				"TITLE": "Mayús", "ARTIST": "A", "ALBUM": "B", "album_artist": "C",
				"track": "05", "TRACKTOTAL": "9", "disc": "2", "TOTALDISCS": "3", "YEAR": "1980",
			},
			want: Tags{Title: "Mayús", Artist: "A", Album: "B", AlbumArtist: "C",
				Track: 5, TrackTotal: 9, Disc: 2, DiscTotal: 3, Date: "1980"},
		},
		{
			desc:   "pares número/total",
			format: map[string]string{"track": "3/12", "disc": " 1 / 2 "},
			want:   Tags{Track: 3, TrackTotal: 12, Disc: 1, DiscTotal: 2},
		},
		{
			desc:   "el total explícito manda sobre el del par",
			format: map[string]string{"track": "3/12", "totaltracks": "13"},
			want:   Tags{Track: 3, TrackTotal: 13},
		},
		{
			desc:   "claves sin convertir",
			format: map[string]string{"TRACKNUMBER": "7", "DISCNUMBER": "1", "DISCTOTAL": "1", "ALBUMARTIST": "X"},
			want:   Tags{Track: 7, Disc: 1, DiscTotal: 1, AlbumArtist: "X"},
		},
		{
			desc:   "date manda sobre year",
			format: map[string]string{"date": "1977-05-25", "year": "1977"},
			want:   Tags{Date: "1977-05-25"},
		},
		{
			desc:   "Ogg: etiquetas en la pista",
			stream: map[string]string{"title": "En la pista", "encoder": "libvorbis"},
			want:   Tags{Title: "En la pista"},
		},
		{
			desc:   "el contenedor manda sobre la pista",
			format: map[string]string{"title": "Contenedor"},
			stream: map[string]string{"title": "Pista", "artist": "Solo en la pista"},
			want:   Tags{Title: "Contenedor", Artist: "Solo en la pista"},
		},
		{
			desc:   "una etiqueta vacía no tapa a la siguiente",
			format: map[string]string{"title": "   "},
			stream: map[string]string{"title": "Bueno"},
			want:   Tags{Title: "Bueno"},
		},
		{
			desc: "números fuera de rango o que no son números",
			format: map[string]string{
				"track": "0/10000", "disc": "abc", "tracktotal": "-1", "disctotal": "1e3",
			},
			want: Tags{},
		},
		{
			desc:   "límites del rango",
			format: map[string]string{"track": "1/9999"},
			want:   Tags{Track: 1, TrackTotal: 9999},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := parseTags(tt.format, tt.stream); got != tt.want {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestCleanTag(t *testing.T) {
	tests := []struct{ in, want string }{
		{"  Hola  ", "Hola"},
		{"a\x00b", "a b"},
		{"línea\nnueva\tcon\rcontrol", "línea nueva con control"},
		{"\x1b[31mrojo", "[31mrojo"},
		{"ñandú ÁÉÍ", "ñandú ÁÉÍ"},
		{strings.Repeat("é", 600), strings.Repeat("é", maxTagRunes)},
	}
	for _, tt := range tests {
		if got := cleanTag(tt.in); got != tt.want {
			t.Errorf("cleanTag(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseProbe(t *testing.T) {
	const good = `{"streams":[{"codec_name":"flac","sample_rate":"44100","channels":2,` +
		`"bits_per_sample":0,"bits_per_raw_sample":"16","duration":"183.5"}],` +
		`"format":{"duration":"183.6","bit_rate":"900000","tags":{"title":"T"}}}`
	got, err := parseProbe([]byte(good))
	if err != nil {
		t.Fatalf("parseProbe: %v", err)
	}
	want := Info{Codec: "flac", Lossless: true, SampleRate: 44100, Bits: 16, Channels: 2,
		Duration: 183500 * time.Millisecond, BitRate: 900000, Tags: Tags{Title: "T"}}
	if got != want {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}

	// Sin duración en la pista, vale la del contenedor.
	noStreamDur := strings.Replace(good, `"duration":"183.5"`, `"duration":"N/A"`, 1)
	if got, err := parseProbe([]byte(noStreamDur)); err != nil || got.Duration != 183600*time.Millisecond {
		t.Errorf("duración del contenedor: %v, %v", got.Duration, err)
	}

	bad := map[string]string{
		"JSON roto":            `{"streams":[`,
		"sin pistas":           `{"streams":[],"format":{}}`,
		"códec desconocido":    strings.Replace(good, `"flac"`, `"wmav2"`, 1),
		"sin frecuencia":       strings.Replace(good, `"44100"`, `"N/A"`, 1),
		"sin canales":          strings.Replace(good, `"channels":2`, `"channels":0`, 1),
		"sin bits":             strings.Replace(good, `"bits_per_raw_sample":"16"`, `"bits_per_raw_sample":"0"`, 1),
		"duración NaN":         strings.NewReplacer(`"183.5"`, `"nan"`, `"183.6"`, `"nan"`).Replace(good),
		"duración desmesurada": strings.NewReplacer(`"183.5"`, `"1e300"`, `"183.6"`, `"1e300"`).Replace(good),
		"duración negativa":    strings.NewReplacer(`"183.5"`, `"-1"`, `"183.6"`, `"-1"`).Replace(good),
	}
	for desc, data := range bad {
		if _, err := parseProbe([]byte(data)); !errors.Is(err, ErrUnreadable) {
			t.Errorf("%s: error %v, want ErrUnreadable", desc, err)
		}
	}
}

func TestCappedBuffer(t *testing.T) {
	b := &cappedBuffer{max: 5}
	for _, s := range []string{"abc", "defg", "hij"} {
		if n, err := b.Write([]byte(s)); n != len(s) || err != nil {
			t.Fatalf("Write(%q) = %d, %v", s, n, err)
		}
	}
	if b.String() != "abcde" || !b.truncated {
		t.Errorf("contenido %q, truncated %v; want abcde y true", b.String(), b.truncated)
	}
}
