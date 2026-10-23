// El servidor de Bitabit: importa lo que se copia a entrada/ y sirve el audio
// por /media con enlaces firmados.
//
//	bitabit [serve]                     arranca el servidor: /media y la API /api/v1
//	bitabit link <pista> [orig|aac256]  imprime un enlace firmado (fase 1)
//	bitabit password                    cambia la contraseña; se lee de la entrada estándar
//
// Configuración por variables de entorno:
//
//	BITABIT_DATABASE_URL  cadena de conexión a PostgreSQL
//	                      (en el servidor: postgres:///bitabit?host=/var/run/postgresql)
//	BITABIT_DATA_DIR      carpeta con entrada/, biblioteca/ y fallidos/ (ruta absoluta)
//	BITABIT_MEDIA_KEYS    claves de los enlaces: "kid:clave,…" en base64url
//	BITABIT_HTTP_ADDR     dónde escucha el HTTP (por defecto 127.0.0.1:8080)
//	BITABIT_PUBLIC_URL    opcional: con él, «link» imprime la URL completa
//	BITABIT_PROXY_TRUSTED opcional: redes (CIDR, separadas por comas) de las que se lee la
//	                      cabecera del protocolo PROXY con la IP real del cliente
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mex040708-afk/bitabit/server/internal/api"
	"github.com/mex040708-afk/bitabit/server/internal/auth"
	"github.com/mex040708-afk/bitabit/server/internal/db"
	"github.com/mex040708-afk/bitabit/server/internal/ingest"
	"github.com/mex040708-afk/bitabit/server/internal/proxyproto"
	"github.com/mex040708-afk/bitabit/server/internal/stream"
	"github.com/mex040708-afk/bitabit/server/internal/subsonic"
)

const (
	defaultHTTPAddr = "127.0.0.1:8080"
	// serverVersion se muestra en GET /api/v1/info, para depurar. Se sube al etiquetar.
	serverVersion   = "0.2.0"
	shutdownTimeout = 10 * time.Second
	linkTimeout     = 10 * time.Second
	usage           = "uso: bitabit [serve] | bitabit link <pista> [orig|aac256] | bitabit password"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	var err error
	failure := "el servidor se detiene"
	switch {
	case len(os.Args) == 1 || (len(os.Args) == 2 && os.Args[1] == "serve"):
		err = serve(log)
	case os.Args[1] == "link":
		failure = "no se pudo crear el enlace"
		err = printLink(os.Args[2:])
	case os.Args[1] == "password":
		failure = "no se pudo cambiar la contraseña"
		err = changePassword(os.Args[2:], os.Stdin, os.Stdout)
	default:
		failure = "orden desconocida"
		err = errors.New(usage)
	}
	if err != nil {
		log.Error(failure, "error", err)
		os.Exit(1)
	}
}

func serve(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn, err := requiredEnv("BITABIT_DATABASE_URL")
	if err != nil {
		return err
	}
	dataDir, err := requiredEnv("BITABIT_DATA_DIR")
	if err != nil {
		return err
	}
	signer, err := signerFromEnv()
	if err != nil {
		return err
	}
	addr := os.Getenv("BITABIT_HTTP_ADDR")
	if addr == "" {
		addr = defaultHTTPAddr
	}

	pool, err := db.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}

	im, err := ingest.NewImporter(dataDir, pool, log)
	if err != nil {
		return err
	}
	defer im.Close() // solo lectura al cerrar: no puede perder datos
	if _, err := im.Recover(ctx); err != nil {
		return err
	}
	library, err := os.OpenRoot(filepath.Join(dataDir, "biblioteca"))
	if err != nil {
		return fmt.Errorf("abrir la biblioteca: %w", err)
	}
	defer library.Close() // solo lectura: cerrar no puede perder datos

	mux := http.NewServeMux()
	load := newLoadLimits()
	mux.Handle("GET /media/{file}", load.mediaHandler(stream.NewHandler(signer, lookup(pool), library, log)))
	// Un solo servicio de autenticación: el bloqueo por IP se comparte entre /api y /rest.
	authSvc := auth.NewService(pool)
	mux.Handle("/rest/", load.apiHandler(subsonic.New(subsonic.Config{Signer: signer, Library: library, Pool: pool, Auth: authSvc, ServerVersion: serverVersion, Log: log})))
	mux.Handle("/api/", load.apiHandler(api.New(api.Config{
		Pool:          pool,
		Auth:          authSvc,
		Signer:        signer,
		Library:       library,
		ServerVersion: serverVersion,
		Log:           log,
	})))
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	// Se escucha antes de arrancar nada, para que un puerto ocupado se vea al
	// momento.
	base, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("escuchar en %s: %w", addr, err)
	}
	// Con un VPS delante, la IP real del cliente llega en la cabecera PROXY, y solo se
	// lee de las direcciones de confianza (ADR-040). Sin lista, no cambia nada.
	trusted, err := proxyproto.ParseTrusted(os.Getenv("BITABIT_PROXY_TRUSTED"))
	if err != nil {
		base.Close()
		return fmt.Errorf("BITABIT_PROXY_TRUSTED: %w", err)
	}
	ln := proxyproto.NewListener(base, trusted)
	if len(trusted) > 0 {
		log.Info("se lee el protocolo PROXY de las direcciones de confianza", "redes", len(trusted))
	}

	// Si uno de los dos se cae, se para el otro.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	importErr := make(chan error, 1)
	go func() { importErr <- im.Run(ctx, ingest.ScanInterval, ingest.QuietPeriod) }()
	httpErr := make(chan error, 1)
	go func() { httpErr <- srv.Serve(ln) }()
	log.Info("en marcha", "http", ln.Addr().String(), "datos", dataDir)

	var errs []error
	select {
	case <-ctx.Done():
	case err := <-importErr:
		errs = append(errs, fmt.Errorf("importación: %w", err))
		importErr = nil
	case err := <-httpErr:
		errs = append(errs, fmt.Errorf("HTTP: %w", err))
		httpErr = nil
	}
	cancel()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, fmt.Errorf("parar el HTTP: %w", err))
	}
	if httpErr != nil {
		if err := <-httpErr; !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, fmt.Errorf("HTTP: %w", err))
		}
	}
	if importErr != nil {
		if err := <-importErr; !errors.Is(err, context.Canceled) {
			errs = append(errs, fmt.Errorf("importación: %w", err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	log.Info("parado")
	return nil
}

// lookup busca en la base de datos la versión que pide un enlace.
func lookup(pool *pgxpool.Pool) stream.LookupFunc {
	return func(ctx context.Context, id int64, q stream.Quality) (stream.FileInfo, bool, error) {
		f, found, err := db.FileByTrack(ctx, pool, id, string(q))
		return stream.FileInfo{Ext: f.Ext, SHA256: f.SHA256}, found, err
	}
}

// printLink imprime un enlace firmado. En la fase 1 no hay API con inicio de
// sesión: solo quien entra en el servidor puede pedir enlaces.
func printLink(args []string) error {
	id, quality, err := parseLinkArgs(args)
	if err != nil {
		return err
	}
	dsn, err := requiredEnv("BITABIT_DATABASE_URL")
	if err != nil {
		return err
	}
	signer, err := signerFromEnv()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), linkTimeout)
	defer cancel()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	f, found, err := db.FileByTrack(ctx, pool, id, string(quality))
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("la pista %d no tiene versión %s", id, quality)
	}
	u, err := signer.URL(stream.Link{TrackID: id, Quality: quality, Ext: f.Ext}, time.Now())
	if err != nil {
		return err
	}
	_, err = fmt.Println(strings.TrimSuffix(os.Getenv("BITABIT_PUBLIC_URL"), "/") + u)
	return err
}

func parseLinkArgs(args []string) (int64, stream.Quality, error) {
	if len(args) < 1 || len(args) > 2 {
		return 0, "", errors.New(usage)
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, "", fmt.Errorf("número de pista no válido: %q", args[0])
	}
	quality := stream.QualityOriginal
	if len(args) == 2 {
		quality = stream.Quality(args[1])
	}
	if quality != stream.QualityOriginal && quality != stream.QualityAAC256 {
		return 0, "", fmt.Errorf("calidad no válida: %q (orig o aac256)", quality)
	}
	return id, quality, nil
}

func signerFromEnv() (*stream.Signer, error) {
	raw, err := requiredEnv("BITABIT_MEDIA_KEYS")
	if err != nil {
		return nil, err
	}
	keys, err := stream.ParseKeys(raw)
	if err != nil {
		return nil, fmt.Errorf("BITABIT_MEDIA_KEYS: %w", err)
	}
	signer, err := stream.NewSigner(keys)
	if err != nil {
		return nil, fmt.Errorf("BITABIT_MEDIA_KEYS: %w", err)
	}
	return signer, nil
}

func requiredEnv(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("falta la variable de entorno %s", name)
	}
	return v, nil
}
