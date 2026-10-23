package proxyproto

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// serve abre un listener de bucle local con las redes de confianza dadas y devuelve
// la dirección a la que llamar y un canal con lo que ve el servidor de cada conexión.
type seen struct {
	remote string
	data   string
	err    error
}

func serve(t *testing.T, trusted string, timeout time.Duration) (addr string, results chan seen) {
	t.Helper()
	nets, err := ParseTrusted(trusted)
	if err != nil {
		t.Fatal(err)
	}
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := NewListener(base, nets)
	if timeout > 0 {
		ln.timeout = timeout
	}
	t.Cleanup(func() { ln.Close() })
	results = make(chan seen, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				remote := c.RemoteAddr().String()
				data, err := io.ReadAll(bufio.NewReader(c))
				results <- seen{remote: remote, data: string(data), err: err}
			}()
		}
	}()
	return base.Addr().String(), results
}

func send(t *testing.T, addr string, payload []byte) {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := c.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	// Se espera a que el servidor cierre para no soltar la conexión antes de tiempo.
	io.Copy(io.Discard, c)
	c.Close()
}

func wait(t *testing.T, results chan seen) seen {
	t.Helper()
	select {
	case s := <-results:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("el servidor no vio la conexión")
		return seen{}
	}
}

func v2(command, family byte, block []byte) []byte {
	out := append([]byte{}, v2Signature...)
	out = append(out, 0x20|command, family<<4|1)
	out = binary.BigEndian.AppendUint16(out, uint16(len(block)))
	return append(out, block...)
}

func v2inet4(src, dst string, sport, dport uint16) []byte {
	block := netip.MustParseAddr(src).AsSlice()
	block = append(block, netip.MustParseAddr(dst).AsSlice()...)
	block = binary.BigEndian.AppendUint16(block, sport)
	block = binary.BigEndian.AppendUint16(block, dport)
	return v2(1, 1, block)
}

func TestTrustedV1GivesTheRealAddress(t *testing.T) {
	addr, results := serve(t, "127.0.0.0/8", 0)
	send(t, addr, []byte("PROXY TCP4 203.0.113.9 192.0.2.1 51000 443\r\nGET / HTTP/1.1\r\n\r\n"))
	got := wait(t, results)
	if got.remote != "203.0.113.9:51000" || got.data != "GET / HTTP/1.1\r\n\r\n" {
		t.Errorf("remoto %q, datos %q", got.remote, got.data)
	}
}

func TestTrustedV1IPv6(t *testing.T) {
	addr, results := serve(t, "127.0.0.1", 0)
	send(t, addr, []byte("PROXY TCP6 2001:db8::7 2001:db8::1 1234 443\r\nhola"))
	if got := wait(t, results); got.remote != "[2001:db8::7]:1234" || got.data != "hola" {
		t.Errorf("remoto %q, datos %q", got.remote, got.data)
	}
}

func TestTrustedV2GivesTheRealAddress(t *testing.T) {
	addr, results := serve(t, "127.0.0.0/8", 0)
	send(t, addr, append(v2inet4("198.51.100.4", "192.0.2.1", 40000, 443), "hola"...))
	if got := wait(t, results); got.remote != "198.51.100.4:40000" || got.data != "hola" {
		t.Errorf("remoto %q, datos %q", got.remote, got.data)
	}
}

func TestTrustedV2IPv6AndTLVsAreSkipped(t *testing.T) {
	block := netip.MustParseAddr("2001:db8::99").AsSlice()
	block = append(block, netip.MustParseAddr("2001:db8::1").AsSlice()...)
	block = binary.BigEndian.AppendUint16(block, 4321)
	block = binary.BigEndian.AppendUint16(block, 443)
	block = append(block, 0x04, 0x00, 0x03, 'a', 'b', 'c') // un TLV que se ignora
	addr, results := serve(t, "127.0.0.1", 0)
	send(t, addr, append(v2(1, 2, block), "datos"...))
	if got := wait(t, results); got.remote != "[2001:db8::99]:4321" || got.data != "datos" {
		t.Errorf("remoto %q, datos %q", got.remote, got.data)
	}
}

func TestLocalAndUnknownKeepTheProxyAddress(t *testing.T) {
	addr, results := serve(t, "127.0.0.1", 0)
	send(t, addr, append(v2(0, 0, nil), "a"...))
	got := wait(t, results)
	if !strings.HasPrefix(got.remote, "127.0.0.1:") || got.data != "a" {
		t.Errorf("LOCAL: remoto %q, datos %q", got.remote, got.data)
	}
	send(t, addr, []byte("PROXY UNKNOWN\r\nb"))
	got = wait(t, results)
	if !strings.HasPrefix(got.remote, "127.0.0.1:") || got.data != "b" {
		t.Errorf("UNKNOWN: remoto %q, datos %q", got.remote, got.data)
	}
}

// Lo importante: una cabecera PROXY de una dirección que no es de confianza no se
// interpreta. La IP sigue siendo la de la conexión y la cabecera pasa como datos.
func TestUntrustedHeaderIsNotInterpreted(t *testing.T) {
	for name, header := range map[string][]byte{
		"v1": []byte("PROXY TCP4 203.0.113.9 192.0.2.1 51000 443\r\n"),
		"v2": v2inet4("198.51.100.4", "192.0.2.1", 40000, 443),
	} {
		addr, results := serve(t, "10.0.0.0/8", 0) // el bucle local no es de confianza
		send(t, addr, append(header, "GET /"...))
		got := wait(t, results)
		if !strings.HasPrefix(got.remote, "127.0.0.1:") {
			t.Errorf("%s: una IP no confiable falseó su dirección: %q", name, got.remote)
		}
		if !strings.HasSuffix(got.data, "GET /") || !strings.Contains(got.data, "PROXY") && name == "v1" {
			t.Errorf("%s: la cabecera debía pasar como datos, llegó %q", name, got.data)
		}
	}
}

func TestEmptyTrustedListNeverReadsHeaders(t *testing.T) {
	addr, results := serve(t, "", 0)
	send(t, addr, []byte("PROXY TCP4 203.0.113.9 192.0.2.1 51000 443\r\nx"))
	if got := wait(t, results); !strings.HasPrefix(got.remote, "127.0.0.1:") {
		t.Errorf("remoto %q", got.remote)
	}
}

func TestTrustedConnectionWithoutHeaderIsClosed(t *testing.T) {
	addr, results := serve(t, "127.0.0.1", 0)
	send(t, addr, []byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	got := wait(t, results)
	if !errors.Is(got.err, ErrNoHeader) || got.data != "" {
		t.Errorf("error %v, datos %q; want ErrNoHeader y nada", got.err, got.data)
	}
}

func TestMalformedHeadersAreRejected(t *testing.T) {
	bad := map[string][]byte{
		"v1 sin CRLF":            []byte("PROXY TCP4 203.0.113.9 192.0.2.1 51000 443\nx"),
		"v1 campos de menos":     []byte("PROXY TCP4 203.0.113.9 192.0.2.1 51000\r\n"),
		"v1 familia que no es":   []byte("PROXY TCP4 2001:db8::1 2001:db8::2 1 2\r\n"),
		"v1 puerto enorme":       []byte("PROXY TCP4 203.0.113.9 192.0.2.1 70000 443\r\n"),
		"v1 muy larga":           []byte("PROXY TCP4 " + strings.Repeat("1", 200) + "\r\n"),
		"v2 versión 3":           append(append([]byte{}, v2Signature...), 0x31, 0x11, 0, 0),
		"v2 bloque corto":        v2(1, 1, []byte{1, 2, 3}),
		"v2 comando desconocido": v2(7, 1, nil),
		"v2 longitud enorme":     append(append([]byte{}, v2Signature...), 0x21, 0x11, 0xFF, 0xFF),
		"v2 cortada":             append(append([]byte{}, v2Signature...), 0x21, 0x11, 0x00, 0x0C, 1, 2),
	}
	for name, payload := range bad {
		addr, results := serve(t, "127.0.0.1", time.Second)
		send(t, addr, payload)
		got := wait(t, results)
		if got.err == nil || got.data != "" {
			t.Errorf("%s: error %v, datos %q; debía cerrarse con error", name, got.err, got.data)
		}
	}
}

func TestSilentTrustedConnectionTimesOut(t *testing.T) {
	addr, results := serve(t, "127.0.0.1", 150*time.Millisecond)
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	start := time.Now()
	got := wait(t, results)
	if got.err == nil {
		t.Error("una conexión de confianza muda debía acabar con error")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("tardó %v en cortar", time.Since(start))
	}
}

func TestParseTrusted(t *testing.T) {
	got, err := ParseTrusted(" 192.0.2.0/24, 10.1.2.3 ,,2001:db8::/32")
	if err != nil || len(got) != 3 {
		t.Fatalf("ParseTrusted = %v, %v", got, err)
	}
	if got[1].String() != "10.1.2.3/32" {
		t.Errorf("una dirección suelta es una /32: %v", got[1])
	}
	if _, err := ParseTrusted("no-es-una-red"); err == nil {
		t.Error("debía rechazar lo que no es una red")
	}
	if none, err := ParseTrusted(""); err != nil || len(none) != 0 {
		t.Errorf("lista vacía: %v, %v", none, err)
	}
}
