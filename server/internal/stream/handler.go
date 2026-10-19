package stream

import (
	"context"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/mex040708-afk/bitabit/server/internal/library"
)

// mediaWriteTimeout es el plazo de cada respuesta de /media. No hay
// WriteTimeout global porque cortaría una canción larga con poca subida.
const mediaWriteTimeout = 2 * time.Hour

// contentTypes va explícito porque la tabla de tipos de Go no incluye audio/flac
// en todas las instalaciones.
var contentTypes = map[string]string{
	"flac": "audio/flac",
	"m4a":  "audio/mp4",
	"mp3":  "audio/mpeg",
	"ogg":  "audio/ogg",
	"opus": "audio/ogg",
}

// FileInfo es lo que el endpoint necesita de una versión guardada.
type FileInfo struct {
	Ext    string
	SHA256 [32]byte
}

// LookupFunc busca la versión quality de una pista; found es false si no existe.
type LookupFunc func(ctx context.Context, trackID int64, quality Quality) (info FileInfo, found bool, err error)

// Handler sirve el audio de /media/{file} a quien traiga un enlace firmado.
// Se registra con el patrón "GET /media/{file}", que también cubre HEAD.
type Handler struct {
	signer *Signer
	lookup LookupFunc
	root   *os.Root
	log    *slog.Logger
}

// NewHandler sirve los archivos de root, la carpeta biblioteca/ abierta con
// os.Root: ninguna ruta puede salir de ella, ni con enlaces simbólicos.
func NewHandler(signer *Signer, lookup LookupFunc, root *os.Root, log *slog.Logger) *Handler {
	return &Handler{signer: signer, lookup: lookup, root: root, log: log}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
		return
	}
	link, err := h.signer.Verify(r.PathValue("file"), r.URL.RawQuery, time.Now())
	if errors.Is(err, ErrMalformed) {
		http.Error(w, "enlace mal formado", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "enlace no válido o caducado", http.StatusForbidden)
		return
	}

	info, found, err := h.lookup(r.Context(), link.TrackID, link.Quality)
	if err != nil {
		// Se registra la ruta y nunca la consulta: la firma no debe acabar en
		// un registro.
		h.log.Error("buscar la versión pedida", "ruta", r.URL.Path, "error", err)
		http.Error(w, "error interno", http.StatusInternalServerError)
		return
	}
	if !found || info.Ext != link.Ext {
		http.Error(w, "no existe", http.StatusNotFound)
		return
	}

	f, err := h.root.Open(library.File(string(link.Quality), link.TrackID, link.Ext))
	if errors.Is(err, fs.ErrNotExist) {
		h.log.Error("la base de datos tiene la versión, pero falta el archivo", "ruta", r.URL.Path)
		http.Error(w, "no existe", http.StatusNotFound)
		return
	}
	if err != nil {
		h.log.Error("abrir la versión pedida", "ruta", r.URL.Path, "error", err)
		http.Error(w, "error interno", http.StatusInternalServerError)
		return
	}
	defer f.Close() // solo lectura: cerrar no puede perder datos
	stat, err := f.Stat()
	if err != nil {
		h.log.Error("leer los datos del archivo", "ruta", r.URL.Path, "error", err)
		http.Error(w, "error interno", http.StatusInternalServerError)
		return
	}

	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(mediaWriteTimeout)); err != nil &&
		!errors.Is(err, http.ErrNotSupported) {
		h.log.Error("poner el plazo de la respuesta", "ruta", r.URL.Path, "error", err)
	}
	header := w.Header()
	header.Set("Content-Type", contentType(link.Ext))
	header.Set("ETag", `"`+hex.EncodeToString(info.SHA256[:])+`"`)
	// 12 horas, lo que dura el enlace, y solo en el dispositivo.
	header.Set("Cache-Control", "private, max-age=43200, immutable")
	http.ServeContent(w, r, "", stat.ModTime(), f)
}

func contentType(ext string) string {
	if t, ok := contentTypes[ext]; ok {
		return t
	}
	return "application/octet-stream"
}
