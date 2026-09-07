// Package api expone el servicio como una API REST en JSON.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/matching"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
)

// Server enruta las peticiones HTTP hacia el servicio.
type Server struct {
	svc *service.Service
	log *slog.Logger
}

// NewServer construye el manejador HTTP con todas las rutas registradas.
func NewServer(svc *service.Service, log *slog.Logger) http.Handler {
	s := &Server{svc: svc, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)

	mux.HandleFunc("POST /api/v1/users", s.createUser)
	mux.HandleFunc("GET /api/v1/users/{id}", s.getUser)
	mux.HandleFunc("GET /api/v1/users/{id}/bookings", s.listUserBookings)

	mux.HandleFunc("POST /api/v1/trips", s.createTrip)
	mux.HandleFunc("GET /api/v1/trips", s.listTrips)
	mux.HandleFunc("GET /api/v1/trips/{id}", s.getTrip)
	mux.HandleFunc("POST /api/v1/trips/{id}/cancel", s.cancelTrip)
	mux.HandleFunc("GET /api/v1/trips/{id}/fare", s.tripFare)
	mux.HandleFunc("GET /api/v1/trips/{id}/bookings", s.listTripBookings)
	mux.HandleFunc("POST /api/v1/trips/{id}/bookings", s.createBooking)

	mux.HandleFunc("POST /api/v1/bookings/{id}/decision", s.decideBooking)
	mux.HandleFunc("POST /api/v1/bookings/{id}/cancel", s.cancelBooking)

	mux.HandleFunc("POST /api/v1/search", s.search)

	return withLogging(log, mux)
}

// --- Salud ---

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Usuarios ---

type createUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if !decode(w, r, &req) {
		return
	}
	u, err := s.svc.CreateUser(req.Name, req.Email)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.svc.GetUser(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) listUserBookings(w http.ResponseWriter, r *http.Request) {
	bs, err := s.svc.BookingsByPassenger(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bookings": nonNil(bs)})
}

// --- Trayectos ---

type createTripRequest struct {
	HostID        string             `json:"host_id"`
	Origin        domain.Place       `json:"origin"`
	Destination   domain.Place       `json:"destination"`
	Waypoints     []geo.Point        `json:"waypoints"`
	DepartureTime time.Time          `json:"departure_time"`
	Vehicle       domain.VehicleType `json:"vehicle"`
	SeatsOffered  int                `json:"seats_offered"`
	MaxDetourKm   float64            `json:"max_detour_km"`
	Notes         string             `json:"notes"`
}

func (s *Server) createTrip(w http.ResponseWriter, r *http.Request) {
	var req createTripRequest
	if !decode(w, r, &req) {
		return
	}
	t, err := s.svc.CreateTrip(service.NewTripInput{
		HostID:        req.HostID,
		Origin:        req.Origin,
		Destination:   req.Destination,
		Waypoints:     req.Waypoints,
		DepartureTime: req.DepartureTime,
		Vehicle:       req.Vehicle,
		SeatsOffered:  req.SeatsOffered,
		MaxDetourKm:   req.MaxDetourKm,
		Notes:         req.Notes,
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

func (s *Server) getTrip(w http.ResponseWriter, r *http.Request) {
	t, err := s.svc.GetTrip(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tripView(t))
}

type actorRequest struct {
	ActorID string `json:"actor_id"`
}

func (s *Server) cancelTrip(w http.ResponseWriter, r *http.Request) {
	var req actorRequest
	if !decode(w, r, &req) {
		return
	}
	t, err := s.svc.CancelTrip(r.PathValue("id"), req.ActorID)
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
	bs, err := s.svc.BookingsByTrip(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bookings": nonNil(bs)})
}

// --- Reservas ---

type createBookingRequest struct {
	PassengerID string       `json:"passenger_id"`
	Pickup      domain.Place `json:"pickup"`
	Dropoff     domain.Place `json:"dropoff"`
	Seats       int          `json:"seats"`
}

func (s *Server) createBooking(w http.ResponseWriter, r *http.Request) {
	var req createBookingRequest
	if !decode(w, r, &req) {
		return
	}
	b, err := s.svc.RequestBooking(service.BookInput{
		TripID:      r.PathValue("id"),
		PassengerID: req.PassengerID,
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
	ActorID string `json:"actor_id"`
	Accept  bool   `json:"accept"`
}

func (s *Server) decideBooking(w http.ResponseWriter, r *http.Request) {
	var req decisionRequest
	if !decode(w, r, &req) {
		return
	}
	b, err := s.svc.DecideBooking(r.PathValue("id"), req.ActorID, req.Accept)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) cancelBooking(w http.ResponseWriter, r *http.Request) {
	var req actorRequest
	if !decode(w, r, &req) {
		return
	}
	b, err := s.svc.CancelBooking(r.PathValue("id"), req.ActorID)
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
	matches, err := s.svc.Search(matching.Query{
		Pickup:            req.Pickup,
		Dropoff:           req.Dropoff,
		EarliestDeparture: req.EarliestDeparture,
		LatestDeparture:   req.LatestDeparture,
		Seats:             req.Seats,
		MaxWalkKm:         req.MaxWalkKm,
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

// --- Serialización ---

// tripView añade al trayecto los campos calculados que la interfaz necesita.
func tripView(t *domain.Trip) map[string]any {
	return map[string]any{
		"id":              t.ID,
		"host_id":         t.HostID,
		"origin":          t.Origin,
		"destination":     t.Destination,
		"route":           t.Route,
		"departure_time":  t.DepartureTime,
		"vehicle":         t.Vehicle,
		"seats_total":     t.SeatsTotal,
		"seats_taken":     t.SeatsTaken,
		"seats_available": t.SeatsAvailable(),
		"distance_km":     round2(t.DistanceKm()),
		"max_detour_km":   t.MaxDetourKm,
		"notes":           t.Notes,
		"status":          t.Status,
		"created_at":      t.CreatedAt,
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

// writeError traduce los errores del dominio al código HTTP que les toca.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrValidation):
		writeProblem(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, service.ErrNoSeats):
		writeProblem(w, http.StatusConflict, err.Error())
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
