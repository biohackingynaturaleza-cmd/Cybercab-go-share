// Command server arranca la API de Cybercab Go Share.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/api"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pricing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	tariff := pricing.DefaultTariff()
	tariff.BaseCents = envInt64("FARE_BASE_CENTS", tariff.BaseCents)
	tariff.PerKmCents = envInt64("FARE_PER_KM_CENTS", tariff.PerKmCents)
	tariff.PerMinuteCents = envInt64("FARE_PER_MINUTE_CENTS", tariff.PerMinuteCents)
	tariff.MinimumCents = envInt64("FARE_MINIMUM_CENTS", tariff.MinimumCents)

	db := store.NewMemory()
	svc := service.New(db, service.Config{
		Tariff:   tariff,
		SpeedKmh: float64(envInt64("AVG_SPEED_KMH", service.DefaultSpeedKmh)),
	})

	if os.Getenv("SEED_DEMO") == "1" {
		if err := seedDemo(svc); err != nil {
			log.Error("no se pudo cargar la demo", "err", err)
		} else {
			log.Info("datos de demostración cargados (Austin)")
		}
	}

	addr := ":" + envString("PORT", "8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           api.NewServer(svc, log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Info("servidor escuchando", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("el servidor se detuvo", "err", err)
			os.Exit(1)
		}
	}()

	// Apagado ordenado: dejamos terminar las peticiones en vuelo.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("apagado forzado", "err", err)
	}
	log.Info("servidor detenido")
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}
