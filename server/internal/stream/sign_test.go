package stream_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/stream"
)

// Vector del contrato v1, calculado con openssl sin pasar por este código.
// Clave k1: los bytes 0x00 a 0x1f.
const (
	vectorExp  = 1767225600 // 2026-01-01 00:00:00 UTC
	vectorFLAC = "/media/4312.flac?q=orig&exp=1767225600&kid=k1&sig=lPcWGxpWTo9I7h0clRUkvaAFXkBwXt3Ea3zFFF_rexY"
	vectorAAC  = "/media/4312.m4a?q=aac256&exp=1767225600&kid=k1&sig=PMzRXqhJtggik_2L9hgh1OwFeOCUfzIJSML6jFH1tw8"
)

func secret(first byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = first + byte(i)
	}
	return b
}

var (
	keyK1 = stream.Key{ID: "k1", Secret: secret(0)}
	keyK2 = stream.Key{ID: "k2", Secret: secret(100)}
	// signedAt es el momento en que se firmó el vector: caduca 12 h después.
	signedAt = time.Unix(vectorExp, 0).Add(-stream.LinkTTL)
)

func newSigner(t *testing.T, keys ...stream.Key) *stream.Signer {
	t.Helper()
	s, err := stream.NewSigner(keys)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return s
}

// split separa una ruta firmada en lo que recibe Verify.
func split(t *testing.T, u string) (name, rawQuery string) {
	t.Helper()
	path, rawQuery, ok := strings.Cut(u, "?")
	name, found := strings.CutPrefix(path, "/media/")
	if !ok || !found {
		t.Fatalf("ruta inesperada: %s", u)
	}
	return name, rawQuery
}

func TestURLMatchesContractVector(t *testing.T) {
	s := newSigner(t, keyK1)
	tests := []struct {
		link stream.Link
		want string
	}{
		{stream.Link{TrackID: 4312, Quality: stream.QualityOriginal, Ext: "flac"}, vectorFLAC},
		{stream.Link{TrackID: 4312, Quality: stream.QualityAAC256, Ext: "m4a"}, vectorAAC},
	}
	for _, tt := range tests {
		got, err := s.URL(tt.link, signedAt)
		if err != nil {
			t.Fatalf("URL(%+v): %v", tt.link, err)
		}
		if got != tt.want {
			t.Errorf("URL(%+v)\n got  %s\n want %s", tt.link, got, tt.want)
		}
	}
}

func TestVerifyAcceptsContractVector(t *testing.T) {
	s := newSigner(t, keyK1)
	want := map[string]stream.Link{
		vectorFLAC: {TrackID: 4312, Quality: stream.QualityOriginal, Ext: "flac"},
		vectorAAC:  {TrackID: 4312, Quality: stream.QualityAAC256, Ext: "m4a"},
	}
	for u, wantLink := range want {
		name, rawQuery := split(t, u)
		got, err := s.Verify(name, rawQuery, signedAt)
		if err != nil {
			t.Fatalf("Verify(%s): %v", u, err)
		}
		if got != wantLink {
			t.Errorf("Verify(%s) = %+v, want %+v", u, got, wantLink)
		}
	}
}

func TestVerifyAcceptsAnyQueryOrder(t *testing.T) {
	s := newSigner(t, keyK1)
	_, err := s.Verify("4312.flac",
		"sig=lPcWGxpWTo9I7h0clRUkvaAFXkBwXt3Ea3zFFF_rexY&kid=k1&exp=1767225600&q=orig", signedAt)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestVerifyExpiry(t *testing.T) {
	s := newSigner(t, keyK1)
	name, rawQuery := split(t, vectorFLAC)

	if _, err := s.Verify(name, rawQuery, time.Unix(vectorExp-1, 0)); err != nil {
		t.Errorf("un segundo antes de caducar: %v", err)
	}
	for _, now := range []time.Time{time.Unix(vectorExp, 0), time.Unix(vectorExp+3600, 0)} {
		if _, err := s.Verify(name, rawQuery, now); !errors.Is(err, stream.ErrExpired) {
			t.Errorf("en %v: error %v, want ErrExpired", now.UTC(), err)
		}
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	s := newSigner(t, keyK1)
	const query = "q=orig&exp=1767225600&kid=k1&sig=lPcWGxpWTo9I7h0clRUkvaAFXkBwXt3Ea3zFFF_rexY"
	tests := []struct {
		desc, name, rawQuery string
	}{
		{"otra pista", "4313.flac", query},
		{"otra extensión", "4312.mp3", query},
		{"otra calidad", "4312.m4a", strings.Replace(query, "q=orig", "q=aac256", 1)},
		{"caducidad alargada", "4312.flac", strings.Replace(query, "exp=1767225600", "exp=1767225601", 1)},
		{"firma cambiada", "4312.flac", strings.Replace(query, "sig=l", "sig=m", 1)},
		{"clave desconocida", "4312.flac", strings.Replace(query, "kid=k1", "kid=k2", 1)},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			_, err := s.Verify(tt.name, tt.rawQuery, signedAt)
			if !errors.Is(err, stream.ErrBadSignature) {
				t.Errorf("error %v, want ErrBadSignature", err)
			}
		})
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	s := newSigner(t, keyK1)
	const (
		sig   = "lPcWGxpWTo9I7h0clRUkvaAFXkBwXt3Ea3zFFF_rexY"
		query = "q=orig&exp=1767225600&kid=k1&sig=" + sig
	)
	replace := func(old, new string) string { return strings.Replace(query, old, new, 1) }
	tests := []struct {
		desc, name, rawQuery string
	}{
		{"sin extensión", "4312", query},
		{"id vacío", ".flac", query},
		{"id cero", "0.flac", query},
		{"id con cero delante", "04312.flac", query},
		{"id con signo", "+4312.flac", query},
		{"id negativo", "-4312.flac", query},
		{"id que no cabe en int64", "9223372036854775808.flac", query},
		{"extensión vacía", "4312.", query},
		{"extensión en mayúsculas", "4312.FLAC", query},
		{"extensión con punto", "4312.fl.ac", query},
		{"extensión larga", "4312.abcdefghi", query},
		{"aac256 en flac", "4312.flac", replace("q=orig", "q=aac256")},
		{"calidad desconocida", "4312.flac", replace("q=orig", "q=hires")},
		{"consulta vacía", "4312.flac", ""},
		{"falta la firma", "4312.flac", "q=orig&exp=1767225600&kid=k1"},
		{"parámetro repetido", "4312.flac", "q=orig&q=orig&exp=1767225600&kid=k1&sig=" + sig},
		{"parámetro de más", "4312.flac", query + "&x=1"},
		{"punto y coma", "4312.flac", replace("&kid", ";kid")},
		{"escape roto", "4312.flac", replace("q=orig", "q=%zz")},
		{"caducidad cero", "4312.flac", replace("exp=1767225600", "exp=0")},
		{"caducidad con cero delante", "4312.flac", replace("exp=1767225600", "exp=01767225600")},
		{"caducidad negativa", "4312.flac", replace("exp=1767225600", "exp=-1767225600")},
		{"clave en mayúsculas", "4312.flac", replace("kid=k1", "kid=K1")},
		{"clave vacía", "4312.flac", replace("kid=k1", "kid=")},
		{"clave larga", "4312.flac", replace("kid=k1", "kid=k1234567890123456")},
		{"firma corta", "4312.flac", replace(sig, sig[:42])},
		{"firma con carácter no válido", "4312.flac", replace(sig, "*"+sig[1:])},
		{"firma con relleno", "4312.flac", replace(sig, sig+"=")},
		// Mismos bytes que la buena, pero con bits de relleno distintos de cero.
		{"firma no canónica", "4312.flac", replace(sig, sig[:42]+"Z")},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			_, err := s.Verify(tt.name, tt.rawQuery, signedAt)
			if !errors.Is(err, stream.ErrMalformed) {
				t.Errorf("error %v, want ErrMalformed", err)
			}
		})
	}
}

func TestKeyRotation(t *testing.T) {
	old := newSigner(t, keyK1)
	rotated := newSigner(t, keyK2, keyK1)
	retired := newSigner(t, keyK2)
	link := stream.Link{TrackID: 7, Quality: stream.QualityOriginal, Ext: "flac"}

	oldURL, err := old.URL(link, signedAt)
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	name, rawQuery := split(t, oldURL)
	if _, err := rotated.Verify(name, rawQuery, signedAt); err != nil {
		t.Errorf("durante la rotación, el enlace viejo no vale: %v", err)
	}
	if _, err := retired.Verify(name, rawQuery, signedAt); !errors.Is(err, stream.ErrBadSignature) {
		t.Errorf("con la clave vieja retirada: error %v, want ErrBadSignature", err)
	}

	newURL, err := rotated.URL(link, signedAt)
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if !strings.Contains(newURL, "&kid=k2&") {
		t.Errorf("durante la rotación se firma con la clave nueva: %s", newURL)
	}
}

func TestURLRejectsInvalidLink(t *testing.T) {
	s := newSigner(t, keyK1)
	links := []stream.Link{
		{TrackID: 0, Quality: stream.QualityOriginal, Ext: "flac"},
		{TrackID: -1, Quality: stream.QualityOriginal, Ext: "flac"},
		{TrackID: 1, Quality: "hires", Ext: "flac"},
		{TrackID: 1, Quality: stream.QualityAAC256, Ext: "flac"},
		{TrackID: 1, Quality: stream.QualityOriginal, Ext: ""},
		{TrackID: 1, Quality: stream.QualityOriginal, Ext: "../x"},
	}
	for _, l := range links {
		if _, err := s.URL(l, signedAt); !errors.Is(err, stream.ErrMalformed) {
			t.Errorf("URL(%+v): error %v, want ErrMalformed", l, err)
		}
	}
}

func TestNewSignerRejectsBadKeys(t *testing.T) {
	tests := []struct {
		desc string
		keys []stream.Key
	}{
		{"sin claves", nil},
		{"clave corta", []stream.Key{{ID: "k1", Secret: secret(0)[:31]}}},
		{"identificador vacío", []stream.Key{{ID: "", Secret: secret(0)}}},
		{"identificador en mayúsculas", []stream.Key{{ID: "K1", Secret: secret(0)}}},
		{"identificador repetido", []stream.Key{keyK1, {ID: "k1", Secret: secret(100)}}},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if _, err := stream.NewSigner(tt.keys); err == nil {
				t.Error("NewSigner aceptó claves no válidas")
			}
		})
	}
}

func TestParseKeys(t *testing.T) {
	const k1 = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
	keys, err := stream.ParseKeys("k2:ZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXp7fH1-f4CBgoM,k1:" + k1)
	if err != nil {
		t.Fatalf("ParseKeys: %v", err)
	}
	if len(keys) != 2 || keys[0].ID != "k2" || keys[1].ID != "k1" {
		t.Fatalf("claves leídas: %+v", keys)
	}
	if string(keys[0].Secret) != string(keyK2.Secret) || string(keys[1].Secret) != string(keyK1.Secret) {
		t.Errorf("secretos mal decodificados")
	}
	if _, err := stream.NewSigner(keys); err != nil {
		t.Errorf("NewSigner con claves leídas: %v", err)
	}

	for _, bad := range []string{"", "k1", "k1:" + k1 + "=", "k1:" + k1 + "+", "k1:" + k1 + ","} {
		_, err := stream.ParseKeys(bad)
		if err == nil {
			t.Errorf("ParseKeys(%q) aceptó un valor no válido", bad)
			continue
		}
		if strings.Contains(err.Error(), k1[:8]) {
			t.Errorf("el error muestra la clave: %v", err)
		}
	}
}

// FuzzVerify comprueba que ninguna entrada hace fallar a Verify y que todo
// rechazo es uno de los tres errores del contrato.
func FuzzVerify(f *testing.F) {
	for _, u := range []string{vectorFLAC, vectorAAC} {
		path, rawQuery, _ := strings.Cut(u, "?")
		f.Add(strings.TrimPrefix(path, "/media/"), rawQuery)
	}
	s, err := stream.NewSigner([]stream.Key{keyK1})
	if err != nil {
		f.Fatalf("NewSigner: %v", err)
	}
	f.Fuzz(func(t *testing.T, name, rawQuery string) {
		_, err := s.Verify(name, rawQuery, signedAt)
		if err != nil &&
			!errors.Is(err, stream.ErrMalformed) &&
			!errors.Is(err, stream.ErrBadSignature) &&
			!errors.Is(err, stream.ErrExpired) {
			t.Errorf("error fuera del contrato: %v", err)
		}
	})
}
