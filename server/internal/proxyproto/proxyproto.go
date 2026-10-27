// Package proxyproto lee el protocolo PROXY (v1 y v2) para saber la IP real del
// cliente cuando el tráfico llega reenviado por un VPS (ADR-040 y ADR-034).
//
// La cabecera solo se interpreta si la conexión viene de una dirección de confianza.
// Una cabecera PROXY que llegue de cualquier otra dirección no se lee: pasa como
// bytes de la conexión, el servidor HTTP la rechaza como petición mal formada y la
// IP que cuenta sigue siendo la de la conexión. Así nadie puede falsear su IP.
package proxyproto

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// HeaderTimeout es lo que se espera la cabecera de una conexión de confianza.
	HeaderTimeout = 5 * time.Second

	maxV1Len = 107  // la especificación fija 107 bytes como máximo en la línea de v1
	maxV2Len = 1024 // address block + TLV; mucho más de lo que manda un proxy normal
)

var (
	// ErrNoHeader: la conexión de confianza no empezó con una cabecera PROXY.
	ErrNoHeader = errors.New("proxyproto: la conexión de confianza no trae cabecera PROXY")
	// ErrMalformed: la cabecera PROXY está mal formada.
	ErrMalformed = errors.New("proxyproto: cabecera PROXY mal formada")

	v2Signature = []byte("\r\n\r\n\x00\r\nQUIT\n")
)

// ParseTrusted lee una lista separada por comas de redes (192.0.2.0/24) o direcciones
// sueltas. Una lista vacía no confía en nadie.
func ParseTrusted(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if p, err := netip.ParsePrefix(item); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("proxyproto: %q no es una red ni una dirección", item)
		}
		out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return out, nil
}

// Listener envuelve a un net.Listener: lee la cabecera PROXY de las conexiones que
// llegan de una dirección de confianza y deja las demás como están.
type Listener struct {
	net.Listener
	trusted []netip.Prefix
	timeout time.Duration
}

// NewListener envuelve ln. Con trusted vacío no cambia nada.
func NewListener(ln net.Listener, trusted []netip.Prefix) *Listener {
	return &Listener{Listener: ln, trusted: trusted, timeout: HeaderTimeout}
}

// Accept devuelve la siguiente conexión. La cabecera de una conexión de confianza se
// lee en la primera lectura (o al pedir su RemoteAddr), no aquí, para que un cliente
// lento no pare el bucle de aceptar.
func (l *Listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	addr, ok := c.RemoteAddr().(*net.TCPAddr)
	if !ok || !l.isTrusted(addr.AddrPort().Addr()) {
		return c, nil
	}
	return &Conn{Conn: c, r: bufio.NewReader(c), timeout: l.timeout}, nil
}

func (l *Listener) isTrusted(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range l.trusted {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Conn es una conexión de confianza cuya primera cosa es la cabecera PROXY.
type Conn struct {
	net.Conn
	r       *bufio.Reader
	timeout time.Duration

	once   sync.Once
	remote net.Addr // la del cliente, si la cabecera la trae
	err    error
}

func (c *Conn) init() {
	c.once.Do(func() {
		if err := c.Conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
			c.err = fmt.Errorf("proxyproto: poner el plazo de la cabecera: %w", err)
			c.Conn.Close()
			return
		}
		addr, err := readHeader(c.r)
		if err != nil {
			c.err = err
			c.Conn.Close()
			return
		}
		if err := c.Conn.SetReadDeadline(time.Time{}); err != nil {
			c.err = fmt.Errorf("proxyproto: quitar el plazo de la cabecera: %w", err)
			c.Conn.Close()
			return
		}
		if addr.IsValid() {
			c.remote = net.TCPAddrFromAddrPort(addr)
		}
	})
}

// Read lee de la conexión una vez consumida la cabecera. Si la cabecera falta o está
// mal, la conexión ya está cerrada y devuelve ese error.
func (c *Conn) Read(p []byte) (int, error) {
	c.init()
	if c.err != nil {
		return 0, c.err
	}
	return c.r.Read(p)
}

// RemoteAddr es la dirección real del cliente si la cabecera la dio (los comandos
// LOCAL y UNKNOWN dejan la del proxy).
func (c *Conn) RemoteAddr() net.Addr {
	c.init()
	if c.remote != nil {
		return c.remote
	}
	return c.Conn.RemoteAddr()
}

// readHeader lee una cabecera v1 o v2 y devuelve la dirección de origen, o la cero si
// el comando es LOCAL o la familia UNKNOWN.
func readHeader(r *bufio.Reader) (netip.AddrPort, error) {
	head, err := r.Peek(len(v2Signature))
	if err != nil && len(head) < 6 {
		return netip.AddrPort{}, fmt.Errorf("%w: %v", ErrNoHeader, err)
	}
	switch {
	case bytes.HasPrefix(head, v2Signature):
		return readV2(r)
	case bytes.HasPrefix(head, []byte("PROXY ")):
		return readV1(r)
	}
	return netip.AddrPort{}, ErrNoHeader
}

func readV1(r *bufio.Reader) (netip.AddrPort, error) {
	line := make([]byte, 0, maxV1Len)
	for {
		b, err := r.ReadByte()
		if err != nil {
			return netip.AddrPort{}, fmt.Errorf("%w: %v", ErrMalformed, err)
		}
		line = append(line, b)
		if b == '\n' {
			break
		}
		if len(line) >= maxV1Len {
			return netip.AddrPort{}, fmt.Errorf("%w: la línea pasa de %d bytes", ErrMalformed, maxV1Len)
		}
	}
	if !bytes.HasSuffix(line, []byte("\r\n")) {
		return netip.AddrPort{}, fmt.Errorf("%w: la línea no acaba en CRLF", ErrMalformed)
	}
	fields := strings.Fields(string(line[:len(line)-2]))
	if len(fields) == 2 && fields[1] == "UNKNOWN" {
		return netip.AddrPort{}, nil
	}
	if len(fields) != 6 || (fields[1] != "TCP4" && fields[1] != "TCP6") {
		return netip.AddrPort{}, fmt.Errorf("%w: campos no válidos", ErrMalformed)
	}
	src, err := netip.ParseAddr(fields[2])
	if err != nil || (fields[1] == "TCP4") != src.Is4() {
		return netip.AddrPort{}, fmt.Errorf("%w: dirección de origen no válida", ErrMalformed)
	}
	if _, err := netip.ParseAddr(fields[3]); err != nil {
		return netip.AddrPort{}, fmt.Errorf("%w: dirección de destino no válida", ErrMalformed)
	}
	port, err := strconv.ParseUint(fields[4], 10, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("%w: puerto de origen no válido", ErrMalformed)
	}
	if _, err := strconv.ParseUint(fields[5], 10, 16); err != nil {
		return netip.AddrPort{}, fmt.Errorf("%w: puerto de destino no válido", ErrMalformed)
	}
	return netip.AddrPortFrom(src, uint16(port)), nil
}

func readV2(r *bufio.Reader) (netip.AddrPort, error) {
	var fixed [16]byte
	if _, err := readFull(r, fixed[:]); err != nil {
		return netip.AddrPort{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if fixed[12]>>4 != 2 {
		return netip.AddrPort{}, fmt.Errorf("%w: versión %d", ErrMalformed, fixed[12]>>4)
	}
	command, family := fixed[12]&0x0F, fixed[13]>>4
	length := int(binary.BigEndian.Uint16(fixed[14:16]))
	if length > maxV2Len {
		return netip.AddrPort{}, fmt.Errorf("%w: %d bytes de datos", ErrMalformed, length)
	}
	block := make([]byte, length)
	if _, err := readFull(r, block); err != nil {
		return netip.AddrPort{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	switch command {
	case 0: // LOCAL: una comprobación del propio proxy; vale la dirección de la conexión
		return netip.AddrPort{}, nil
	case 1: // PROXY
	default:
		return netip.AddrPort{}, fmt.Errorf("%w: comando %d", ErrMalformed, command)
	}
	switch family {
	case 1: // INET
		if len(block) < 12 {
			return netip.AddrPort{}, fmt.Errorf("%w: bloque INET de %d bytes", ErrMalformed, len(block))
		}
		return netip.AddrPortFrom(netip.AddrFrom4([4]byte(block[0:4])), binary.BigEndian.Uint16(block[8:10])), nil
	case 2: // INET6
		if len(block) < 36 {
			return netip.AddrPort{}, fmt.Errorf("%w: bloque INET6 de %d bytes", ErrMalformed, len(block))
		}
		return netip.AddrPortFrom(netip.AddrFrom16([16]byte(block[0:16])), binary.BigEndian.Uint16(block[32:34])), nil
	}
	// UNSPEC o UNIX: no hay IP que dar.
	return netip.AddrPort{}, nil
}

func readFull(r *bufio.Reader, p []byte) (int, error) {
	n := 0
	for n < len(p) {
		m, err := r.Read(p[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
