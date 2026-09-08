// Package api expone el servicio como una API REST en JSON.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/auth"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/matching"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// Server enruta las peticiones HTTP hacia el servicio.
type Server struct {
	svc *service.Service
	log *slog.Logger
	// devIdentidad solo está presente en desarrollo: permite resolver
	// verificaciones a mano. Ver WithDevIdentityResolver.
	devIdentidad *trust.Manual
	// persona está presente cuando el proveedor real de identidad está
	// configurado, y es quien valida la firma de sus avisos.
	persona *trust.Persona
}

// NewServer construye el manejador HTTP con todas las rutas registradas.
//
// Las rutas que actúan en nombre de alguien exigen un token: la identidad sale
// siempre del token verificado, nunca del cuerpo de la petición.
func NewServer(svc *service.Service, verifier auth.Verifier, log *slog.Logger, opts ...Option) http.Handler {
	s := &Server{svc: svc, log: log}
	for _, opt := range opts {
		opt(s)
	}
	requireAuth := auth.Require(verifier, unauthorized)

	// protegida envuelve un manejador para que solo lo alcance quien va identificado.
	protegida := func(h http.HandlerFunc) http.Handler { return requireAuth(h) }

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/v1/zonas", s.zonas)
	mux.HandleFunc("GET /api/v1/config", s.config)

	// Avisos del proveedor de identidad. Pública porque la llama el proveedor;
	// lo que la protege es la firma del cuerpo, no un token.
	mux.HandleFunc("POST /api/v1/webhooks/identidad", s.webhookPersona)

	// Acceso
	mux.HandleFunc("POST /api/v1/auth/register", s.register)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.Handle("GET /api/v1/me", protegida(s.me))
	mux.Handle("GET /api/v1/me/bookings", protegida(s.myBookings))
	mux.Handle("GET /api/v1/me/resumen", protegida(s.resumen))
	mux.Handle("GET /api/v1/me/trips", protegida(s.misTrayectos))

	// Perfiles públicos: se ve con quién vas a compartir coche.
	mux.HandleFunc("GET /api/v1/users/{id}", s.getUser)
	mux.HandleFunc("GET /api/v1/users/{id}/confianza", s.perfilDeConfianza)

	// Verificación de identidad
	mux.Handle("POST /api/v1/me/verificaciones", protegida(s.iniciarVerificacion))
	mux.Handle("GET /api/v1/me/verificaciones", protegida(s.misVerificaciones))
	mux.Handle("POST /api/v1/me/verificaciones/{ref}/refrescar", protegida(s.refrescarVerificacion))

	// Bloqueos
	mux.Handle("POST /api/v1/users/{id}/bloquear", protegida(s.bloquear))
	mux.Handle("POST /api/v1/users/{id}/desbloquear", protegida(s.desbloquear))

	// Trayectos
	mux.HandleFunc("GET /api/v1/trips", s.listTrips)
	mux.HandleFunc("GET /api/v1/trips/{id}", s.getTrip)
	mux.HandleFunc("GET /api/v1/trips/{id}/fare", s.tripFare)
	mux.Handle("POST /api/v1/trips", protegida(s.createTrip))
	mux.Handle("POST /api/v1/trips/{id}/cancel", protegida(s.cancelTrip))
	mux.Handle("GET /api/v1/trips/{id}/bookings", protegida(s.listTripBookings))
	mux.Handle("POST /api/v1/trips/{id}/bookings", protegida(s.createBooking))

	// Facturación
	mux.Handle("POST /api/v1/trips/{id}/completar", protegida(s.completarViaje))
	mux.Handle("GET /api/v1/me/apuntes", protegida(s.misApuntes))
	mux.Handle("GET /api/v1/me/incidencias", protegida(s.misIncidencias))
	mux.Handle("POST /api/v1/trips/{id}/incidencias", protegida(s.declararIncidencia))
	mux.Handle("POST /api/v1/incidencias/{id}/responder", protegida(s.responderIncidencia))
	mux.Handle("POST /api/v1/incidencias/{id}/retirar", protegida(s.retirarIncidencia))
	mux.Handle("GET /api/v1/facturacion/ahorro", protegida(s.ahorroDeAgrupar))

	// Reservas
	mux.Handle("POST /api/v1/bookings/{id}/decision", protegida(s.decideBooking))
	mux.Handle("POST /api/v1/bookings/{id}/cancel", protegida(s.cancelBooking))

	// La búsqueda es pública, pero si trae token se filtra por bloqueos.
	mux.Handle("POST /api/v1/search", auth.Optional(verifier)(http.HandlerFunc(s.search)))

	if s.devIdentidad != nil {
		log.Warn("endpoint de desarrollo activo: /api/v1/dev/verificaciones/{ref}/resolver")
		mux.Handle("POST /api/v1/dev/verificaciones/{ref}/resolver", protegida(s.devResolverVerificacion))
	}

	// La interfaz se sirve desde el propio binario, después de las rutas de la
	// API para que "/" no se coma nada.
	if err := montarWeb(mux); err != nil {
		log.Error("no se pudo montar la interfaz web", "err", err)
	}

	return withLogging(log, mux)
}

// --- Salud ---

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Acceso ---

type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	// Idioma en el que se le escribirá. Lo manda la interfaz según en qué
	// idioma se esté usando.
	Idioma string `json:"idioma"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decode(w, r, &req) {
		return
	}
	sess, err := s.svc.Register(req.Name, req.Email, req.Password, req.Idioma)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decode(w, r, &req) {
		return
	}
	sess, err := s.svc.Login(req.Email, req.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, err := s.svc.GetUser(actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) myBookings(w http.ResponseWriter, r *http.Request) {
	bs, err := s.svc.BookingsByPassenger(actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bookings": nonNil(bs)})
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.svc.GetUser(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// --- Trayectos ---

type createTripRequest struct {
	Origin        domain.Place       `json:"origin"`
	Destination   domain.Place       `json:"destination"`
	Waypoints     []geo.Point        `json:"waypoints"`
	DepartureTime time.Time          `json:"departure_time"`
	Vehicle       domain.VehicleType `json:"vehicle"`
	SeatsOffered  int                `json:"seats_offered"`
	MaxDetourKm   float64            `json:"max_detour_km"`
	MinTrustLevel trust.Level        `json:"min_trust_level"`
	// TarifaDeclaradaCents es el presupuesto que enseña la app de la flota
	// antes de confirmar el viaje.
	TarifaDeclaradaCents int64  `json:"tarifa_declarada_cents"`
	Notes                string `json:"notes"`
}

func (s *Server) createTrip(w http.ResponseWriter, r *http.Request) {
	var req createTripRequest
	if !decode(w, r, &req) {
		return
	}
	t, err := s.svc.CreateTrip(r.Context(), service.NewTripInput{
		HostID:               actor(r),
		Origin:               req.Origin,
		Destination:          req.Destination,
		Waypoints:            req.Waypoints,
		DepartureTime:        req.DepartureTime,
		Vehicle:              req.Vehicle,
		SeatsOffered:         req.SeatsOffered,
		MaxDetourKm:          req.MaxDetourKm,
		MinTrustLevel:        req.MinTrustLevel,
		TarifaDeclaradaCents: req.TarifaDeclaradaCents,
		Notes:                req.Notes,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tripView(t))
}

func (s *Server) listTrips(w http.ResponseWriter, r *http.Request) {
	trips, err := s.svc.ListOpenTrips()
	if err != nil {
		writeError(w, err)
		return
	}
	views := make([]any, 0, len(trips))
	for _, t := range trips {
		views = append(views, tripView(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"trips": views})
}

// misTrayectos devuelve los trayectos propios en cualquier estado: los llenos y
// los ya realizados también hacen falta, que es cuando hay que cerrarlos.
func (s *Server) misTrayectos(w http.ResponseWriter, r *http.Request) {
	trips, err := s.svc.MisTrayectos(actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	views := make([]any, 0, len(trips))
	for _, t := range trips {
		views = append(views, tripView(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"trips": views})
}

func (s *Server) getTrip(w http.ResponseWriter, r *http.Request) {
	t, err := s.svc.GetTrip(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tripView(t))
}

func (s *Server) cancelTrip(w http.ResponseWriter, r *http.Request) {
	t, err := s.svc.CancelTrip(r.PathValue("id"), actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tripView(t))
}

func (s *Server) tripFare(w http.ResponseWriter, r *http.Request) {
	fb, err := s.svc.FareBreakdownFor(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fb)
}

func (s *Server) listTripBookings(w http.ResponseWriter, r *http.Request) {
	bs, err := s.svc.BookingsByTripFor(r.PathValue("id"), actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bookings": nonNil(bs)})
}

// --- Reservas ---

type createBookingRequest struct {
	Pickup  domain.Place `json:"pickup"`
	Dropoff domain.Place `json:"dropoff"`
	Seats   int          `json:"seats"`
}

func (s *Server) createBooking(w http.ResponseWriter, r *http.Request) {
	var req createBookingRequest
	if !decode(w, r, &req) {
		return
	}
	b, err := s.svc.RequestBooking(service.BookInput{
		TripID:      r.PathValue("id"),
		PassengerID: actor(r),
		Pickup:      req.Pickup,
		Dropoff:     req.Dropoff,
		Seats:       req.Seats,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

type decisionRequest struct {
	Accept bool `json:"accept"`
	// AceptaResponsabilidad refleja que quien organiza ha visto el aviso: los
	// términos del robotaxi le hacen responder de la conducta de quien deja
	// subir al vehículo.
	AceptaResponsabilidad bool `json:"acepta_responsabilidad"`
}

func (s *Server) decideBooking(w http.ResponseWriter, r *http.Request) {
	var req decisionRequest
	if !decode(w, r, &req) {
		return
	}
	b, err := s.svc.DecideBooking(service.DecisionInput{
		BookingID:             r.PathValue("id"),
		HostID:                actor(r),
		Accept:                req.Accept,
		AceptaResponsabilidad: req.AceptaResponsabilidad,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) cancelBooking(w http.ResponseWriter, r *http.Request) {
	b, err := s.svc.CancelBooking(r.PathValue("id"), actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// --- Búsqueda ---

type searchRequest struct {
	Pickup            geo.Point `json:"pickup"`
	Dropoff           geo.Point `json:"dropoff"`
	EarliestDeparture time.Time `json:"earliest_departure"`
	LatestDeparture   time.Time `json:"latest_departure"`
	Seats             int       `json:"seats"`
	MaxWalkKm         float64   `json:"max_walk_km"`
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if !decode(w, r, &req) {
		return
	}
	if !req.Pickup.Valid() || !req.Dropoff.Valid() {
		writeProblem(w, http.StatusBadRequest, "coordenadas de recogida o destino no válidas")
		return
	}
	matches, err := s.svc.Search(r.Context(), matching.Query{
		Pickup:            req.Pickup,
		Dropoff:           req.Dropoff,
		EarliestDeparture: req.EarliestDeparture,
		LatestDeparture:   req.LatestDeparture,
		Seats:             req.Seats,
		MaxWalkKm:         req.MaxWalkKm,
		ViajeroID:         actor(r),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	if matches == nil {
		matches = []matching.Match{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": matches, "count": len(matches)})
}

// --- Auxiliares ---

// actor es el usuario autenticado de la petición. Solo se invoca desde rutas
// protegidas, donde el middleware garantiza que existe.
func actor(r *http.Request) string {
	id, _ := auth.UserFrom(r.Context())
	return id
}

// tripView añade al trayecto los campos calculados que la interfaz necesita.
func tripView(t *domain.Trip) map[string]any {
	return map[string]any{
		"id":                    t.ID,
		"host_id":               t.HostID,
		"origin":                t.Origin,
		"destination":           t.Destination,
		"route":                 t.Route,
		"departure_time":        t.DepartureTime,
		"vehicle":               t.Vehicle,
		"seats_total":           t.SeatsTotal,
		"seats_taken":           t.SeatsTaken,
		"seats_available":       t.SeatsAvailable(),
		"distance_km":           round2(t.DistanceKm()),
		"duration_min":          round2(t.DurationMin),
		"route_source":          t.RouteSource,
		"max_detour_km":         t.MaxDetourKm,
		"nivel_exigido":         t.NivelExigido(),
		"motivo_nivel":          trust.ExplicarSuelo(t.AforoTotal()),
		"aviso_responsabilidad": service.AvisoDeResponsabilidad,
		"aforo_total":           t.AforoTotal(),
		"notes":                 t.Notes,
		"status":                t.Status,
		"created_at":            t.CreatedAt,
	}
}

func round2(v float64) float64 {
	r, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 2, 64), 64)
	return r
}

func nonNil[T any](in []T) []T {
	if in == nil {
		return []T{}
	}
	return in
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeProblem(w, http.StatusBadRequest, "JSON no válido: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeProblem(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func unauthorized(w http.ResponseWriter, _ *http.Request, err error) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="cybercab-go-share"`)
	writeProblem(w, http.StatusUnauthorized, err.Error())
}

// writeError traduce los errores del dominio al código HTTP que les toca.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, http.StatusNotFound, err.Error())
	case errors.Is(err, auth.ErrCredencialesInvalidas), errors.Is(err, auth.ErrTokenInvalido):
		unauthorized(w, nil, err)
	case errors.Is(err, service.ErrNoAutorizado), errors.Is(err, service.ErrBloqueado):
		writeProblem(w, http.StatusForbidden, err.Error())
	case errors.Is(err, trust.ErrConfianzaInsuficiente):
		// 403 con el detalle de qué falta: negar el acceso sin explicar qué
		// hacer para conseguirlo solo genera abandono.
		var req *service.RequisitoNoCumplido
		if errors.As(err, &req) {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error":    err.Error(),
				"exigido":  req.Exigido,
				"actual":   req.Actual,
				"te_falta": req.TeFaltan,
				"motivo":   req.Motivo,
			})
			return
		}
		writeProblem(w, http.StatusForbidden, err.Error())
	case errors.Is(err, store.ErrComprobacionEnCurso):
		writeProblem(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrEmailEnUso):
		writeProblem(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrIncidenciaResuelta), errors.Is(err, store.ErrIncidenciaEnCurso):
		writeProblem(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrViajeNoCompletable), errors.Is(err, store.ErrApuntesDuplicados):
		writeProblem(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrNoSeats):
		writeProblem(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrResponsabilidadNoAceptada):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": err.Error(),
			"aviso": service.AvisoDeResponsabilidad,
		})
	case errors.Is(err, domain.ErrValidation):
		writeProblem(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeProblem(w, http.StatusInternalServerError, err.Error())
	}
}

// withLogging deja traza de cada petición con su código y duración.
func withLogging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
