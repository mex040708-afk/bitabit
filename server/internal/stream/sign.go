// Package stream firma y comprueba los enlaces de audio de /media.
//
// El formato es el contrato v1 de enlaces firmados: la ruta es
// /media/{trackId}.{ext}?q={calidad}&exp={caducidad}&kid={clave}&sig={firma}
// y la firma se comprueba sin consultar la base de datos.
package stream

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LinkTTL es lo que dura un enlace: lo bastante para no cortar una canción en
// pausa ni romper un salto.
const LinkTTL = 12 * time.Hour

const (
	// messagePrefix aparta esta firma de cualquier otro uso de la clave y
	// permite cambiar de formato sin que una firma vieja valga en el nuevo.
	messagePrefix = "bitabit-media-v1"
	minSecretLen  = 32
	maxExtLen     = 8
	maxKeyIDLen   = 16
	sigLen        = 43 // 32 bytes de HMAC-SHA256 en base64url sin relleno
)

// Quality es la versión de la pista que se pide.
type Quality string

const (
	QualityOriginal Quality = "orig"
	QualityAAC256   Quality = "aac256"
)

// Los errores de Verify se distinguen con errors.Is. Ninguno incluye valores de
// la consulta: la firma no debe acabar en un registro.
var (
	// ErrMalformed indica que el enlace no cumple el formato (respuesta 400).
	ErrMalformed = errors.New("enlace mal formado")
	// ErrBadSignature indica una clave desconocida o una firma distinta (403).
	ErrBadSignature = errors.New("firma no válida")
	// ErrExpired indica que la firma es buena pero el enlace caducó (403).
	ErrExpired = errors.New("enlace caducado")
)

// Strict rechaza los bits de relleno distintos de cero, para que cada firma
// tenga una sola forma escrita.
var sigEncoding = base64.RawURLEncoding.Strict()

// Link identifica el archivo que se sirve.
type Link struct {
	TrackID int64
	Quality Quality
	Ext     string
}

func (l Link) validate() error {
	if l.TrackID <= 0 {
		return fmt.Errorf("%w: id de pista", ErrMalformed)
	}
	if !isLowerAlnum(l.Ext, maxExtLen) {
		return fmt.Errorf("%w: extensión", ErrMalformed)
	}
	switch l.Quality {
	case QualityOriginal:
	case QualityAAC256:
		if l.Ext != "m4a" {
			return fmt.Errorf("%w: aac256 siempre es m4a", ErrMalformed)
		}
	default:
		return fmt.Errorf("%w: calidad", ErrMalformed)
	}
	return nil
}

// Key es una clave HMAC con su identificador, para poder rotarla.
type Key struct {
	ID     string
	Secret []byte
}

// Signer firma con la primera clave y comprueba con cualquiera de ellas.
type Signer struct {
	active  Key
	secrets map[string][]byte
}

// NewSigner crea un Signer. La primera clave firma; las demás solo comprueban
// los enlaces firmados antes de una rotación.
func NewSigner(keys []Key) (*Signer, error) {
	if len(keys) == 0 {
		return nil, errors.New("hace falta al menos una clave")
	}
	secrets := make(map[string][]byte, len(keys))
	for i, k := range keys {
		if !isLowerAlnum(k.ID, maxKeyIDLen) {
			return nil, fmt.Errorf("clave %d: el identificador debe ser [a-z0-9]{1,%d}", i+1, maxKeyIDLen)
		}
		if len(k.Secret) < minSecretLen {
			return nil, fmt.Errorf("clave %q: tiene %d bytes y hacen falta al menos %d", k.ID, len(k.Secret), minSecretLen)
		}
		if _, dup := secrets[k.ID]; dup {
			return nil, fmt.Errorf("clave %q: identificador repetido", k.ID)
		}
		secrets[k.ID] = k.Secret
	}
	return &Signer{active: keys[0], secrets: secrets}, nil
}

// ParseKeys lee el valor de BITABIT_MEDIA_KEYS: "kid:clave,kid:clave", con
// cada clave en base64url sin relleno. NewSigner valida el resultado.
func ParseKeys(s string) ([]Key, error) {
	if s == "" {
		return nil, errors.New("no hay claves")
	}
	var keys []Key
	for i, part := range strings.Split(s, ",") {
		id, encoded, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("clave %d: falta \":\" entre el identificador y la clave", i+1)
		}
		secret, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			// El error de base64 solo dice la posición, nunca el contenido.
			return nil, fmt.Errorf("clave %d: no es base64url sin relleno: %w", i+1, err)
		}
		keys = append(keys, Key{ID: id, Secret: secret})
	}
	return keys, nil
}

// URL devuelve la ruta firmada de l, que caduca LinkTTL después de now.
func (s *Signer) URL(l Link, now time.Time) (string, error) {
	if err := l.validate(); err != nil {
		return "", err
	}
	exp := now.Add(LinkTTL).Unix()
	sig := sigEncoding.EncodeToString(mac(s.active.Secret, l, exp, s.active.ID))
	return fmt.Sprintf("/media/%d.%s?q=%s&exp=%d&kid=%s&sig=%s",
		l.TrackID, l.Ext, l.Quality, exp, s.active.ID, sig), nil
}

// Verify comprueba un enlace a partir del último segmento de la ruta
// ("4312.flac") y de la consulta sin decodificar.
func (s *Signer) Verify(name, rawQuery string, now time.Time) (Link, error) {
	l, err := parseName(name)
	if err != nil {
		return Link{}, err
	}

	query, err := url.ParseQuery(rawQuery)
	if err != nil || len(query) != 4 {
		return Link{}, fmt.Errorf("%w: la consulta lleva q, exp, kid y sig, y nada más", ErrMalformed)
	}
	var values [4]string
	for i, key := range [4]string{"q", "exp", "kid", "sig"} {
		v := query[key]
		if len(v) != 1 {
			return Link{}, fmt.Errorf("%w: %s debe aparecer una vez", ErrMalformed, key)
		}
		values[i] = v[0]
	}
	q, rawExp, kid, sig := values[0], values[1], values[2], values[3]

	l.Quality = Quality(q)
	if err := l.validate(); err != nil {
		return Link{}, err
	}
	exp, ok := parsePositive(rawExp)
	if !ok {
		return Link{}, fmt.Errorf("%w: caducidad", ErrMalformed)
	}
	if !isLowerAlnum(kid, maxKeyIDLen) {
		return Link{}, fmt.Errorf("%w: identificador de clave", ErrMalformed)
	}
	got, err := sigEncoding.DecodeString(sig)
	if len(sig) != sigLen || err != nil {
		return Link{}, fmt.Errorf("%w: firma", ErrMalformed)
	}

	secret, ok := s.secrets[kid]
	if !ok {
		return Link{}, fmt.Errorf("%w: clave desconocida", ErrBadSignature)
	}
	if !hmac.Equal(got, mac(secret, l, exp, kid)) {
		return Link{}, ErrBadSignature
	}
	// La caducidad se mira después de la firma: antes, exp no es de fiar.
	if now.Unix() >= exp {
		return Link{}, ErrExpired
	}
	return l, nil
}

func parseName(name string) (Link, error) {
	id, ext, ok := strings.Cut(name, ".")
	if !ok {
		return Link{}, fmt.Errorf("%w: falta la extensión", ErrMalformed)
	}
	trackID, ok := parsePositive(id)
	if !ok {
		return Link{}, fmt.Errorf("%w: id de pista", ErrMalformed)
	}
	return Link{TrackID: trackID, Ext: ext}, nil
}

// parsePositive admite solo la forma canónica: dígitos, sin signo ni ceros a la
// izquierda. strconv.ParseInt aceptaría "+5" y "007", y entonces varias URLs
// distintas valdrían con la misma firma.
func parsePositive(s string) (int64, bool) {
	if s == "" || s[0] == '0' {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

func isLowerAlnum(s string, maxLen int) bool {
	if len(s) == 0 || len(s) > maxLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func mac(secret []byte, l Link, exp int64, kid string) []byte {
	h := hmac.New(sha256.New, secret)
	msg := fmt.Sprintf("%s\n%d\n%s\n%s\n%d\n%s", messagePrefix, l.TrackID, l.Quality, l.Ext, exp, kid)
	h.Write([]byte(msg)) // hash.Hash.Write nunca devuelve error
	return h.Sum(nil)
}
