// El servidor de Bitabit. Por ahora solo importa lo que se copia a entrada/;
// el servidor HTTP llega con el endpoint /media.
//
// Configuración por variables de entorno:
//
//	BITABIT_DATABASE_URL  cadena de conexión a PostgreSQL
//	                      (en el servidor: "host=/var/run/postgresql dbname=bitabit")
//	BITABIT_DATA_DIR      carpeta con entrada/, biblioteca/ y fallidos/ (ruta absoluta)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mex040708-afk/bitabit/server/internal/db"
	"github.com/mex040708-afk/bitabit/server/internal/ingest"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("el servidor se detiene", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
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
	if _, err := im.Recover(ctx); err != nil {
		return err
	}

	log.Info("importación en marcha", "datos", dataDir, "cada", ingest.ScanInterval, "quieto", ingest.QuietPeriod)
	err = im.Run(ctx, ingest.ScanInterval, ingest.QuietPeriod)
	if errors.Is(err, context.Canceled) {
		log.Info("parado")
		return nil
	}
	return err
}

func requiredEnv(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("falta la variable de entorno %s", name)
	}
	return v, nil
}
