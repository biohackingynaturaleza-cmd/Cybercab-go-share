// Command server arranca la API de Cybercab Go Share.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/api"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/auth"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pricing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/routing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	tariff := pricing.DefaultTariff()
	tariff.BaseCents = envInt64("FARE_BASE_CENTS", tariff.BaseCents)
	tariff.PerKmCents = envInt64("FARE_PER_KM_CENTS", tariff.PerKmCents)
	tariff.PerMinuteCents = envInt64("FARE_PER_MINUTE_CENTS", tariff.PerMinuteCents)
	tariff.MinimumCents = envInt64("FARE_MINIMUM_CENTS", tariff.MinimumCents)

	speed := float64(envInt64("AVG_SPEED_KMH", service.DefaultSpeedKmh))

	tokens, err := buildTokenIssuer(log)
	if err != nil {
		log.Error("no se pudo preparar la autenticación", "err", err)
		os.Exit(1)
	}

	identidad := buildIdentityProvider(log)

	db, closeDB, err := buildStore(context.Background(), log)
	if err != nil {
		log.Error("no se pudo preparar el almacén", "err", err)
		os.Exit(1)
	}
	defer closeDB()

	svc := service.New(db, service.Config{
		Tariff:    tariff,
		SpeedKmh:  speed,
		Tokens:    tokens,
		Router:    buildRouter(log, speed),
		Identidad: identidad,
	})

	if os.Getenv("SEED_DEMO") == "1" {
		if err := seedDemo(context.Background(), svc); err != nil {
			log.Error("no se pudo cargar la demo", "err", err)
		} else {
			log.Info("datos de demostración cargados (Austin)")
		}
	}

	addr := ":" + envString("PORT", "8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           api.NewServer(svc, tokens, log, devOptions(identidad)...),
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

// buildStore elige dónde se guardan los datos: Postgres si hay DATABASE_URL,
// y si no, memoria. La memoria solo sirve para desarrollo: al reiniciar se
// pierde todo.
func buildStore(ctx context.Context, log *slog.Logger) (store.Store, func(), error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		if os.Getenv("ENV") == "production" {
			return nil, nil, errors.New("DATABASE_URL es obligatorio en producción")
		}
		log.Warn("DATABASE_URL sin definir: los datos se guardan en memoria y se perderán al reiniciar")
		return store.NewMemory(), func() {}, nil
	}

	pg, err := store.NewPostgres(ctx, dsn)
	if err != nil {
		return nil, nil, err
	}
	if err := pg.Migrate(ctx); err != nil {
		pg.Close()
		return nil, nil, fmt.Errorf("aplicando migraciones: %w", err)
	}
	log.Info("almacén en Postgres, migraciones al día")
	return pg, pg.Close, nil
}

// buildTokenIssuer prepara la firma de sesiones. En producción el secreto es
// obligatorio: si se generase uno nuevo en cada arranque, todas las sesiones
// caducarían en cada despliegue y no habría forma de escalar a varias réplicas.
func buildTokenIssuer(log *slog.Logger) (*auth.TokenIssuer, error) {
	secret := []byte(os.Getenv("AUTH_SECRET"))
	if len(secret) == 0 {
		if os.Getenv("ENV") == "production" {
			return nil, errors.New("AUTH_SECRET es obligatorio en producción")
		}
		generated, err := auth.GenerateSecret()
		if err != nil {
			return nil, err
		}
		secret = generated
		log.Warn("AUTH_SECRET sin definir: se ha generado uno temporal. " +
			"Las sesiones se invalidarán al reiniciar")
	}
	ttl := time.Duration(envInt64("AUTH_TOKEN_TTL_HOURS", 24)) * time.Hour
	return auth.NewTokenIssuer(secret, ttl)
}

// buildIdentityProvider elige quién acredita las identidades.
//
// En producción es obligatorio un proveedor real: sin él, nadie llega a nivel
// verificado y ningún trayecto que lo exija admite pasajeros. Es deliberado:
// preferimos no dar viajes a darlos sin saber quién viaja.
func buildIdentityProvider(log *slog.Logger) trust.Provider {
	// IDENTITY_PROVIDER_URL queda preparado para el proveedor real (Stripe
	// Identity, Onfido, Persona). Mientras no exista, solo el modo manual.
	if os.Getenv("ENV") == "production" {
		log.Error("no hay proveedor de identidad configurado: " +
			"nadie podrá acreditar su identidad y los trayectos que la exijan quedarán vacíos")
	} else {
		log.Warn("proveedor de identidad en modo manual: NO verifica nada, solo desarrollo")
	}
	return trust.NewManual(envString("PUBLIC_URL", "http://localhost:8080"))
}

// buildRouter elige el motor de rutas: OSRM si hay servidor configurado, con
// respaldo en línea recta para que un fallo del proveedor no tumbe la app.
// devOptions activa los atajos de desarrollo. En producción devuelve nada: el
// endpoint que resuelve verificaciones a mano no debe existir siquiera.
func devOptions(identidad trust.Provider) []api.Option {
	if os.Getenv("ENV") == "production" {
		return nil
	}
	if m, ok := identidad.(*trust.Manual); ok {
		return []api.Option{api.WithDevIdentityResolver(m)}
	}
	return nil
}

func buildRouter(log *slog.Logger, speedKmh float64) routing.Router {
	backup := routing.NewStraightLine(speedKmh)

	url := os.Getenv("OSRM_URL")
	if url == "" {
		log.Info("OSRM_URL sin definir: las rutas serán aproximaciones en línea recta")
		return backup
	}
	log.Info("motor de rutas activo", "url", url)
	return routing.WithFallback(routing.NewOSRM(url), backup, log)
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
