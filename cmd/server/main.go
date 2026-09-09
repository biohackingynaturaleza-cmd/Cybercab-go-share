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
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pricing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/routing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	addr := ":" + envString("PORT", "8080")

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

	identidad, personaReal := buildIdentityProvider(log)

	avisos := buildAvisos(log)
	defer avisos.Cerrar()

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
		Avisos:    avisos,
		PublicURL: envString("PUBLIC_URL", "http://localhost"+addr),
		Log:       log,
	})

	if os.Getenv("SEED_DEMO") == "1" {
		if err := seedDemo(context.Background(), svc); err != nil {
			log.Error("no se pudo cargar la demo", "err", err)
		} else {
			log.Info("datos de demostración cargados (Austin)")
		}
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.NewServer(svc, tokens, log, opcionesDeServidor(identidad, personaReal)...),
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

	// El cierre de periodos va dentro del propio binario, no en un cron del
	// sistema: desplegar la app no puede exigir además configurar una tarea
	// aparte que, si falta, hace que nadie cobre y nadie se entere.
	fondo, pararFondo := context.WithCancel(context.Background())
	defer pararFondo()
	go service.NuevoProgramador(svc).Arrancar(fondo)

	// Apagado ordenado: dejamos terminar las peticiones en vuelo.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	pararFondo()

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

// buildAvisos elige por dónde salen los correos.
//
// Con SMTP_HOST se envían de verdad. Sin él se escriben en el registro, que en
// desarrollo es lo que se quiere: se ve exactamente qué se habría mandado, sin
// montar un servidor de correo y sin riesgo de escribir a direcciones reales
// desde una máquina de pruebas.
func buildAvisos(log *slog.Logger) *notify.Cola {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		if os.Getenv("ENV") == "production" {
			log.Error("sin SMTP_HOST no se avisa a nadie: quien organiza no " +
				"se enterará de que le han pedido plaza")
		} else {
			log.Warn("correo en modo registro: los avisos se escriben aquí, no se envían")
		}
		return notify.NuevaCola(&notify.Registro{Log: log}, log, 256)
	}

	enviador, err := notify.NuevoSMTP(host, os.Getenv("SMTP_PORT"),
		os.Getenv("SMTP_USER"), os.Getenv("SMTP_PASSWORD"),
		envString("SMTP_FROM", "Cybercab Go Share <no-reply@localhost>"))
	if err != nil {
		log.Error("correo mal configurado, se sigue en modo registro", "err", err)
		return notify.NuevaCola(&notify.Registro{Log: log}, log, 256)
	}
	log.Info("correo activo", "servidor", host)
	return notify.NuevaCola(enviador, log, 512)
}

// buildIdentityProvider elige quién acredita las identidades.
//
// Con PERSONA_API_KEY se usa Persona. Sin ella queda el modo manual, que no
// verifica nada: en producción eso significa que nadie alcanza el nivel
// verificado y ningún Cybercab admite pasajeros. Es el fallo seguro correcto —
// preferimos no dar viajes a darlos sin saber quién viaja— pero conviene que
// se vea en los registros.
func buildIdentityProvider(log *slog.Logger) (trust.Provider, *trust.Persona) {
	clave := os.Getenv("PERSONA_API_KEY")
	if clave == "" {
		if os.Getenv("ENV") == "production" {
			log.Error("sin PERSONA_API_KEY nadie podrá acreditar su identidad, " +
				"y los trayectos que la exijan quedarán vacíos")
		} else {
			log.Warn("proveedor de identidad en modo manual: NO verifica nada, solo desarrollo")
		}
		return trust.NewManual(envString("PUBLIC_URL", "http://localhost:8080")), nil
	}

	plantillas := map[trust.CheckKind]string{}
	// Una sola plantilla puede acreditar documento y cara a la vez, que es
	// como lo hacen los proveedores: un trámite, no dos.
	if id := os.Getenv("PERSONA_TEMPLATE_ID"); id != "" {
		plantillas[trust.CheckGovernmentID] = id
		plantillas[trust.CheckSelfie] = id
	}
	// El correo no aparece aquí: lo comprobamos nosotros con un código. Ver
	// internal/service/correo.go.
	if id := os.Getenv("PERSONA_TEMPLATE_PHONE"); id != "" {
		plantillas[trust.CheckPhone] = id
	}

	p, err := trust.NewPersona(trust.PersonaConfig{
		APIKey:            clave,
		Plantillas:        plantillas,
		WebhookSecret:     os.Getenv("PERSONA_WEBHOOK_SECRET"),
		AceptarCompletado: os.Getenv("PERSONA_ACEPTAR_COMPLETADO") == "1",
	})
	if err != nil {
		log.Error("Persona mal configurado, se sigue en modo manual", "err", err)
		return trust.NewManual(envString("PUBLIC_URL", "http://localhost:8080")), nil
	}
	if os.Getenv("PERSONA_WEBHOOK_SECRET") == "" {
		log.Warn("sin PERSONA_WEBHOOK_SECRET los avisos se rechazarán: " +
			"las verificaciones solo se resolverán al consultarlas")
	}
	log.Info("proveedor de identidad: Persona", "comprobaciones", len(plantillas))
	return p, p
}

// buildRouter elige el motor de rutas: OSRM si hay servidor configurado, con
// respaldo en línea recta para que un fallo del proveedor no tumbe la app.
// opcionesDeServidor conecta el proveedor real y, fuera de producción, los
// atajos de desarrollo. El endpoint que resuelve verificaciones a mano no debe
// existir siquiera en producción.
func opcionesDeServidor(identidad trust.Provider, persona *trust.Persona) []api.Option {
	var opts []api.Option
	if persona != nil {
		opts = append(opts, api.WithPersona(persona))
	}
	// Detrás de un proxy, la IP del cliente llega en una cabecera y no en la
	// conexión. Sin esto, todo el tráfico contaría como una sola dirección y el
	// límite de peticiones dejaría fuera a todo el mundo a la vez.
	if os.Getenv("TRUST_PROXY") == "1" {
		opts = append(opts, api.WithProxyDeConfianza())
	}
	// La cola de revisión de denuncias. Sin secreto configurado no existe:
	// preferimos que no esté a que esté con un token adivinable.
	if token := os.Getenv("OPS_TOKEN"); token != "" {
		opts = append(opts, api.WithPanelDeOperaciones(token))
	}
	if os.Getenv("ENV") == "production" {
		return opts
	}
	if m, ok := identidad.(*trust.Manual); ok {
		opts = append(opts, api.WithDevIdentityResolver(m))
	}
	return opts
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
