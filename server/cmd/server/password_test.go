package main

import (
	"bytes"
	"context"
	"net/netip"
	"strings"
	"testing"

	"github.com/mex040708-afk/bitabit/server/internal/auth"
	"github.com/mex040708-afk/bitabit/server/internal/db/dbtest"
)

func TestReadPassword(t *testing.T) {
	good := map[string]string{
		"contraseña de prueba\n":         "contraseña de prueba",
		"contraseña de prueba\r\n":       "contraseña de prueba",
		"contraseña de prueba":           "contraseña de prueba",  // sin fin de línea
		"contraseña ñandú\nsobra esta\n": "contraseña ñandú",      // solo se lee la primera línea
		"  doce con espacios  \n":        "  doce con espacios  ", // los espacios cuentan
		"\uFEFFcontraseña de prueba\r\n": "contraseña de prueba",  // BOM de PowerShell
	}
	for in, want := range good {
		got, err := readPassword(strings.NewReader(in))
		if err != nil || got != want {
			t.Errorf("readPassword(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	bad := []string{
		"",
		"\n",
		"corta\n",
		"\r\n",
		"\uFEFF\r\n", // solo el BOM
		strings.Repeat("a", maxPasswordInput) + "\n", // demasiado larga
	}
	for _, in := range bad {
		if _, err := readPassword(strings.NewReader(in)); err == nil {
			t.Errorf("readPassword(%d bytes) acepta una entrada no válida", len(in))
		}
	}
}

// La contraseña nunca va como argumento: con cualquier argumento se rechaza sin
// tocar la base de datos.
func TestChangePasswordRefusesArguments(t *testing.T) {
	var out bytes.Buffer
	err := changePassword([]string{"contraseña de prueba"}, strings.NewReader("otra contraseña\n"), &out)
	if err == nil {
		t.Fatal("changePassword acepta la contraseña como argumento")
	}
	if strings.Contains(err.Error(), "contraseña de prueba") || out.Len() != 0 {
		t.Errorf("el error o la salida muestran el argumento: %q, %q", err, out.String())
	}
}

func TestSetPasswordReportsClosedSessions(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	const first, second = "contraseña de prueba", "otra contraseña de prueba"
	ip := netip.MustParseAddr("192.0.2.1")

	var out bytes.Buffer
	if err := setPassword(ctx, pool, first, &out); err != nil {
		t.Fatalf("setPassword: %v", err)
	}
	if want := "Contraseña guardada. Sesiones cerradas: 0.\n"; out.String() != want {
		t.Errorf("salida %q, want %q", out.String(), want)
	}

	svc := auth.NewService(pool)
	if _, _, err := svc.Login(ctx, ip, first, "Pixel"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := setPassword(ctx, pool, second, &out); err != nil {
		t.Fatalf("setPassword al cambiarla: %v", err)
	}
	if want := "Contraseña guardada. Sesiones cerradas: 1.\n"; out.String() != want {
		t.Errorf("salida %q, want %q", out.String(), want)
	}
	if strings.Contains(out.String(), second) {
		t.Error("la salida repite la contraseña")
	}
	if _, _, err := svc.Login(ctx, ip, second, "Pixel"); err != nil {
		t.Errorf("con la contraseña nueva: %v", err)
	}
}
