// Package service contiene la lógica de negocio: crear trayectos, buscarlos,
// reservar plaza y repartir el coste.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/auth"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/matching"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pricing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/routing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// DefaultSpeedKmh es la velocidad media que asumimos para estimar duraciones
// mientras no consultemos un motor de rutas real.
const DefaultSpeedKmh = 45

// Config son los parámetros ajustables del servicio.
type Config struct {
	Tariff   pricing.Tariff
	SpeedKmh float64
	// Tokens emite las sesiones. Sin él, Register y Login no están disponibles.
	Tokens *auth.TokenIssuer
	// Router calcula las rutas de los trayectos. Por defecto, línea recta.
	Router routing.Router
	// ComisionBps es la comisión de servicio en puntos básicos. La paga el
	// pasajero y es ingreso de la plataforma: no entra en el reparto, así que
	// quien organiza sigue sin ganar dinero.
	ComisionBps int64
	// Identidad verifica quién es cada persona. Sin él no se pueden acreditar
	// identidades, y ningún trayecto que exija nivel verificado admitirá a
	// nadie: es deliberado, preferimos no dar viajes a darlos sin verificar.
	Identidad trust.Provider
	// Now permite fijar el reloj en las pruebas.
	Now func() time.Time
}

// ErrNoAutorizado se devuelve cuando quien pide la acción no tiene permiso
// sobre ese recurso. Se distingue de la validación porque merece un 403, no un
// 422: la petición es correcta, quien la hace no.
var ErrNoAutorizado = errors.New("no tienes permiso sobre este recurso")

// Service es el punto de entrada a la lógica de negocio.
type Service struct {
	store store.Store
	cfg   Config
}

// New construye el servicio, rellenando los valores de configuración omitidos.
func New(s store.Store, cfg Config) *Service {
	if cfg.SpeedKmh <= 0 {
		cfg.SpeedKmh = DefaultSpeedKmh
	}
	if cfg.Tariff == (pricing.Tariff{}) {
		cfg.Tariff = pricing.DefaultTariff()
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.Router == nil {
		cfg.Router = routing.NewStraightLine(cfg.SpeedKmh)
	}
	return &Service{store: s, cfg: cfg}
}

// Tariff expone la tarifa vigente.
func (s *Service) Tariff() pricing.Tariff { return s.cfg.Tariff }

// --- Usuarios ---

// Session es lo que recibe quien se registra o entra: quién es y su token.
type Session struct {
	User      *domain.User `json:"user"`
	Token     string       `json:"token"`
	ExpiresAt time.Time    `json:"expires_at"`
}

// Register da de alta a una persona y le abre sesión.
func (s *Service) Register(name, email, password string) (*Session, error) {
	if name == "" || email == "" {
		return nil, fmt.Errorf("%w: nombre y email son obligatorios", domain.ErrValidation)
	}
	if !strings.Contains(email, "@") {
		return nil, fmt.Errorf("%w: el email no parece válido", domain.ErrValidation)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrValidation, err)
	}

	u := &domain.User{
		ID:           newID("usr"),
		Name:         name,
		Email:        strings.TrimSpace(email),
		PasswordHash: hash,
		Rating:       5,
		CreatedAt:    s.cfg.Now(),
	}
	if err := s.store.CreateUser(u); err != nil {
		return nil, err
	}
	return s.openSession(u)
}

// Login comprueba las credenciales y abre sesión.
func (s *Service) Login(email, password string) (*Session, error) {
	u, err := s.store.GetUserByEmail(email)
	if err != nil {
		// Gastamos el mismo tiempo que en un acierto: si respondiéramos antes
		// cuando el email no existe, el propio retardo revelaría quién está
		// registrado.
		auth.CheckPassword(dummyHash, password) //nolint:errcheck // solo iguala el tiempo
		return nil, auth.ErrCredencialesInvalidas
	}
	if err := auth.CheckPassword(u.PasswordHash, password); err != nil {
		return nil, err
	}
	return s.openSession(u)
}

// dummyHash es un bcrypt válido de una contraseña que nadie usa. Solo sirve
// para que Login tarde lo mismo exista o no el usuario.
const dummyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

func (s *Service) openSession(u *domain.User) (*Session, error) {
	if s.cfg.Tokens == nil {
		return nil, errors.New("el servicio no tiene configurado el emisor de tokens")
	}
	token, expires, err := s.cfg.Tokens.Issue(u.ID, s.cfg.Now())
	if err != nil {
		return nil, err
	}
	return &Session{User: u, Token: token, ExpiresAt: expires}, nil
}

// GetUser recupera un usuario por identificador.
func (s *Service) GetUser(id string) (*domain.User, error) { return s.store.GetUser(id) }

// --- Trayectos ---

// NewTripInput son los datos para publicar un trayecto compartido.
type NewTripInput struct {
	HostID        string
	Origin        domain.Place
	Destination   domain.Place
	Waypoints     []geo.Point
	DepartureTime time.Time
	Vehicle       domain.VehicleType
	SeatsOffered  int
	MaxDetourKm   float64
	Notes         string
	// MinTrustLevel es el nivel que se exige a quien se suba. El suelo del
	// vehículo puede elevarlo, nunca rebajarlo.
	MinTrustLevel trust.Level
}

// CreateTrip publica un trayecto que otros podrán compartir.
//
// La ruta la traza el motor de rutas: la polilínea que se guarda es la que el
// coche va a seguir de verdad, no la línea recta entre origen y destino. De ahí
// depende que el emparejamiento y el reparto del coste sean fieles.
func (s *Service) CreateTrip(ctx context.Context, in NewTripInput) (*domain.Trip, error) {
	if _, err := s.store.GetUser(in.HostID); err != nil {
		return nil, fmt.Errorf("%w: el usuario que organiza no existe", domain.ErrValidation)
	}
	if in.Vehicle == "" {
		in.Vehicle = domain.VehicleCybercab
	}
	if in.SeatsOffered == 0 {
		// Por defecto se ofrece todo lo que no ocupa quien organiza.
		in.SeatsOffered = in.Vehicle.Seats() - 1
	}
	if in.MaxDetourKm == 0 {
		in.MaxDetourKm = matching.DefaultMaxWalkKm
	}

	waypoints := make([]geo.Point, 0, len(in.Waypoints)+2)
	waypoints = append(waypoints, in.Origin.Point)
	waypoints = append(waypoints, in.Waypoints...)
	waypoints = append(waypoints, in.Destination.Point)

	computed, err := s.cfg.Router.Route(ctx, waypoints)
	if err != nil {
		if errors.Is(err, routing.ErrSinRuta) {
			return nil, fmt.Errorf("%w: no hay ruta por carretera entre esos puntos", domain.ErrValidation)
		}
		return nil, fmt.Errorf("calculando la ruta: %w", err)
	}

	t := &domain.Trip{
		ID:            newID("trip"),
		HostID:        in.HostID,
		Origin:        in.Origin,
		Destination:   in.Destination,
		Route:         computed.Geometry,
		DurationMin:   computed.DurationMin,
		RouteSource:   computed.Source,
		DepartureTime: in.DepartureTime.UTC(),
		Vehicle:       in.Vehicle,
		SeatsTotal:    in.SeatsOffered,
		MaxDetourKm:   in.MaxDetourKm,
		MinTrustLevel: in.MinTrustLevel,
		Notes:         in.Notes,
		Status:        domain.TripOpen,
		CreatedAt:     s.cfg.Now(),
	}
	if err := t.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrValidation, err)
	}
	if t.DepartureTime.Before(s.cfg.Now()) {
		return nil, fmt.Errorf("%w: la salida ya ha pasado", domain.ErrValidation)
	}
	if err := s.store.CreateTrip(t); err != nil {
		return nil, err
	}
	return t, nil
}

// GetTrip recupera un trayecto.
func (s *Service) GetTrip(id string) (*domain.Trip, error) { return s.store.GetTrip(id) }

// ListOpenTrips devuelve los trayectos que admiten pasajeros.
func (s *Service) ListOpenTrips() ([]*domain.Trip, error) { return s.store.ListOpenTrips() }

// CancelTrip anula un trayecto y con él sus reservas activas.
func (s *Service) CancelTrip(tripID, hostID string) (*domain.Trip, error) {
	t, err := s.store.GetTrip(tripID)
	if err != nil {
		return nil, err
	}
	if t.HostID != hostID {
		return nil, fmt.Errorf("%w: solo quien organiza puede anular el trayecto", ErrNoAutorizado)
	}
	bookings, err := s.store.BookingsByTrip(tripID)
	if err != nil {
		return nil, err
	}
	for _, b := range bookings {
		if b.Status.Active() {
			b.Status = domain.BookingCancelled
			if err := s.store.UpdateBooking(b); err != nil {
				return nil, err
			}
		}
	}
	t.Status = domain.TripCancelled
	t.SeatsTaken = 0
	if err := s.store.UpdateTrip(t); err != nil {
		return nil, err
	}
	return t, nil
}

// Search busca trayectos compatibles con lo que pide un pasajero.
//
// El filtro grueso —qué rutas pasan cerca— lo hace el almacén si sabe
// (Postgres, con su índice espacial); si no, se recorren los trayectos
// abiertos. El emparejamiento fino es siempre el mismo código, de modo que el
// resultado no depende de dónde estén guardados los datos.
func (s *Service) Search(ctx context.Context, q matching.Query) ([]matching.Match, error) {
	trips, err := s.candidates(ctx, q)
	if err != nil {
		return nil, err
	}
	// Quien busca no ve los trayectos de quienes ha bloqueado ni de quienes le
	// han bloqueado a él.
	bloqueos, err := s.bloqueados(q.ViajeroID)
	if err != nil {
		return nil, err
	}
	trips = filtrarBloqueados(trips, bloqueos)
	occ := matching.Occupancy{}
	for _, t := range trips {
		o, err := s.occupants(t)
		if err != nil {
			return nil, err
		}
		occ[t.ID] = o
	}
	return matching.Find(trips, q, occ, s.cfg.Tariff, s.cfg.SpeedKmh), nil
}

// candidates devuelve los trayectos que merece la pena evaluar.
func (s *Service) candidates(ctx context.Context, q matching.Query) ([]*domain.Trip, error) {
	searcher, ok := s.store.(store.GeoSearcher)
	if !ok {
		return s.store.ListOpenTrips()
	}

	maxDist := q.MaxWalkKm
	if maxDist <= 0 {
		maxDist = matching.DefaultMaxWalkKm
	}
	// El filtro del almacén debe ser más ancho que el criterio final: quien
	// organiza puede aceptar un desvío mayor que lo que el pasajero pide
	// caminar, y descartarlo aquí perdería trayectos válidos.
	return searcher.CandidateTrips(ctx, store.GeoQuery{
		Pickup:            q.Pickup,
		Dropoff:           q.Dropoff,
		EarliestDeparture: q.EarliestDeparture,
		LatestDeparture:   q.LatestDeparture,
		Seats:             q.Seats,
		MaxDistanceKm:     maxDist + maxDetourMargenKm,
	})
}

// maxDetourMargenKm es el margen que se añade al filtro del almacén para no
// descartar trayectos cuyo organizador acepta desviarse más de lo pedido.
const maxDetourMargenKm = 5

// --- Reservas ---

// ErrNoSeats indica que el trayecto ya no admite más pasajeros.
var ErrNoSeats = errors.New("no quedan plazas libres")

// BookInput son los datos de una petición de plaza.
type BookInput struct {
	TripID      string
	PassengerID string
	Pickup      domain.Place
	Dropoff     domain.Place
	Seats       int
}

// RequestBooking pide plaza en un trayecto para el tramo indicado.
func (s *Service) RequestBooking(in BookInput) (*domain.Booking, error) {
	if in.Seats < 1 {
		in.Seats = 1
	}
	t, err := s.store.GetTrip(in.TripID)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.GetUser(in.PassengerID); err != nil {
		return nil, fmt.Errorf("%w: el pasajero no existe", domain.ErrValidation)
	}
	if in.PassengerID == t.HostID {
		return nil, fmt.Errorf("%w: quien organiza ya viaja en el trayecto", domain.ErrValidation)
	}
	// La confianza se comprueba antes que las plazas: alguien que no puede
	// subirse no debe llegar siquiera a retener un asiento.
	if err := s.comprobarConfianza(t, in.PassengerID); err != nil {
		return nil, err
	}
	if !t.Bookable() {
		return nil, ErrNoSeats
	}
	if t.SeatsAvailable() < in.Seats {
		return nil, ErrNoSeats
	}

	pickup := t.Route.Project(in.Pickup.Point)
	dropoff := t.Route.Project(in.Dropoff.Point)
	maxOff := t.MaxDetourKm
	if maxOff < matching.DefaultMaxWalkKm {
		maxOff = matching.DefaultMaxWalkKm
	}
	if pickup.OffRouteKm > maxOff || dropoff.OffRouteKm > maxOff {
		return nil, fmt.Errorf("%w: los puntos pedidos quedan fuera de la ruta", domain.ErrValidation)
	}
	if dropoff.AlongKm-pickup.AlongKm < matching.MinSharedKm {
		return nil, fmt.Errorf("%w: la bajada debe ir por detrás de la recogida en el sentido de la marcha", domain.ErrValidation)
	}

	current, err := s.occupants(t)
	if err != nil {
		return nil, err
	}
	routeKm := t.DistanceKm()
	price := pricing.EstimateSeatPrice(
		s.tripCost(routeKm),
		routeKm,
		current,
		pricing.Occupant{ID: "__candidate__", StartKm: pickup.AlongKm, EndKm: dropoff.AlongKm, Seats: in.Seats},
		t.HostID,
	)

	// La plaza se retiene desde la petición, y se retiene antes de crear la
	// reserva: si dos personas piden a la vez la última plaza, solo una pasa
	// de aquí.
	if err := s.store.ReserveSeats(t.ID, in.Seats); err != nil {
		if errors.Is(err, store.ErrSinPlazas) {
			return nil, ErrNoSeats
		}
		return nil, err
	}

	b := &domain.Booking{
		ID:             newID("bkg"),
		TripID:         t.ID,
		PassengerID:    in.PassengerID,
		Pickup:         in.Pickup,
		Dropoff:        in.Dropoff,
		PickupAlongKm:  pickup.AlongKm,
		DropoffAlongKm: dropoff.AlongKm,
		Seats:          in.Seats,
		PriceCents:     price,
		Status:         domain.BookingPending,
		CreatedAt:      s.cfg.Now(),
	}
	if err := s.store.CreateBooking(b); err != nil {
		// La reserva no llegó a existir: devolvemos la plaza en vez de dejarla
		// retenida para siempre.
		_ = s.store.ReleaseSeats(t.ID, in.Seats)
		return nil, err
	}
	return b, nil
}

// DecisionInput es la decisión de quien organiza sobre una petición de plaza.
type DecisionInput struct {
	BookingID string
	HostID    string
	Accept    bool
	// AceptaResponsabilidad tiene que ser cierto para aceptar a alguien.
	//
	// Los términos del servicio de robotaxi hacen a quien pide el viaje
	// "plenamente responsable de la conducta de cualquier otra persona a la
	// que permita entrar en el vehículo". Quien acepta a un desconocido está
	// asumiendo esa responsabilidad, y dejar que lo haga sin saberlo sería
	// ocultarle una obligación real frente a Tesla.
	AceptaResponsabilidad bool
}

// ErrResponsabilidadNoAceptada se devuelve al aceptar a alguien sin asumir la
// responsabilidad que imponen los términos de la flota.
var ErrResponsabilidadNoAceptada = errors.New(
	"para aceptar a alguien tienes que asumir que respondes de su conducta dentro del vehículo")

// AvisoDeResponsabilidad es el texto que hay que mostrar antes de aceptar.
const AvisoDeResponsabilidad = "Los términos del robotaxi te hacen responsable de la conducta " +
	"de quien dejes subir al vehículo, incluidos los daños que cause. " +
	"Revisa su perfil de confianza antes de aceptar."

// DecideBooking confirma o rechaza una petición. Solo quien organiza decide.
func (s *Service) DecideBooking(in DecisionInput) (*domain.Booking, error) {
	b, err := s.store.GetBooking(in.BookingID)
	if err != nil {
		return nil, err
	}
	t, err := s.store.GetTrip(b.TripID)
	if err != nil {
		return nil, err
	}
	if t.HostID != in.HostID {
		return nil, fmt.Errorf("%w: solo quien organiza puede decidir sobre la reserva", ErrNoAutorizado)
	}
	if b.Status != domain.BookingPending {
		return nil, fmt.Errorf("%w: la reserva ya está en estado %q", domain.ErrValidation, b.Status)
	}
	// Rechazar no exige asumir nada; aceptar, sí.
	if in.Accept && !in.AceptaResponsabilidad {
		return nil, ErrResponsabilidadNoAceptada
	}

	if accept := in.Accept; accept {
		b.Status = domain.BookingConfirmed
		if err := s.store.UpdateBooking(b); err != nil {
			return nil, err
		}
		return b, nil
	}

	b.Status = domain.BookingRejected
	if err := s.store.UpdateBooking(b); err != nil {
		return nil, err
	}
	return b, s.releaseSeats(t, b.Seats)
}

// CancelBooking permite al pasajero (o a quien organiza) anular una reserva.
func (s *Service) CancelBooking(bookingID, actorID string) (*domain.Booking, error) {
	b, err := s.store.GetBooking(bookingID)
	if err != nil {
		return nil, err
	}
	t, err := s.store.GetTrip(b.TripID)
	if err != nil {
		return nil, err
	}
	if actorID != b.PassengerID && actorID != t.HostID {
		return nil, fmt.Errorf("%w: no puedes anular esta reserva", ErrNoAutorizado)
	}
	if !b.Status.Active() {
		return b, nil // anular dos veces no es un error
	}
	b.Status = domain.BookingCancelled
	if err := s.store.UpdateBooking(b); err != nil {
		return nil, err
	}
	return b, s.releaseSeats(t, b.Seats)
}

// BookingsByTrip lista las reservas de un trayecto, sin comprobar permisos.
// Es de uso interno; la API entra por BookingsByTripFor.
func (s *Service) BookingsByTrip(tripID string) ([]*domain.Booking, error) {
	return s.store.BookingsByTrip(tripID)
}

// BookingsByTripFor lista las reservas que actorID tiene derecho a ver: quien
// organiza las ve todas; un pasajero, solo la suya. Sin este filtro, cualquiera
// podría leer con quién viaja el resto de la gente.
func (s *Service) BookingsByTripFor(tripID, actorID string) ([]*domain.Booking, error) {
	t, err := s.store.GetTrip(tripID)
	if err != nil {
		return nil, err
	}
	all, err := s.store.BookingsByTrip(tripID)
	if err != nil {
		return nil, err
	}
	if t.HostID == actorID {
		return all, nil
	}
	mine := make([]*domain.Booking, 0, 1)
	for _, b := range all {
		if b.PassengerID == actorID {
			mine = append(mine, b)
		}
	}
	if len(mine) == 0 {
		return nil, ErrNoAutorizado
	}
	return mine, nil
}

// BookingsByPassenger lista las reservas de un pasajero.
func (s *Service) BookingsByPassenger(userID string) ([]*domain.Booking, error) {
	return s.store.BookingsByPassenger(userID)
}

// --- Reparto del coste ---

// FareShare es lo que paga una persona del trayecto.
type FareShare struct {
	UserID      string  `json:"user_id"`
	Role        string  `json:"role"` // "host" o "passenger"
	FromKm      float64 `json:"from_km"`
	ToKm        float64 `json:"to_km"`
	Seats       int     `json:"seats"`
	AmountCents int64   `json:"amount_cents"`
}

// FareBreakdown es el desglose completo del coste de un trayecto.
type FareBreakdown struct {
	TripID      string  `json:"trip_id"`
	DistanceKm  float64 `json:"distance_km"`
	DurationMin float64 `json:"duration_min"`
	TotalCents  int64   `json:"total_cents"`
	// SoloCostCents es lo que le costaría a quien organiza ir sin compartir:
	// la diferencia con su parte es el ahorro de usar la app.
	SoloCostCents int64       `json:"solo_cost_cents"`
	Shares        []FareShare `json:"shares"`
}

// FareBreakdownFor calcula cómo se reparte el coste de un trayecto entre quien
// lo organiza y las reservas vivas.
func (s *Service) FareBreakdownFor(tripID string) (*FareBreakdown, error) {
	t, err := s.store.GetTrip(tripID)
	if err != nil {
		return nil, err
	}
	bookings, err := s.store.BookingsByTrip(tripID)
	if err != nil {
		return nil, err
	}

	routeKm := t.DistanceKm()
	total := s.tripCost(routeKm)

	occupants := []pricing.Occupant{{ID: t.HostID, StartKm: 0, EndKm: routeKm, Seats: 1}}
	active := make([]*domain.Booking, 0, len(bookings))
	for _, b := range bookings {
		if !b.Status.Active() {
			continue
		}
		active = append(active, b)
		occupants = append(occupants, pricing.Occupant{
			ID:      b.PassengerID,
			StartKm: b.PickupAlongKm,
			EndKm:   b.DropoffAlongKm,
			Seats:   b.Seats,
		})
	}

	amounts := pricing.SplitFare(total, routeKm, occupants, t.HostID)

	// La comprobación no es decorativa: si quien organiza ganara dinero, el
	// servicio dejaría de ser gasto compartido y pasaría a ser transporte
	// comercial sujeto a permiso estatal. Antes de enseñar un reparto que
	// rompiera esa línea, preferimos fallar.
	if err := pricing.VerificarSinLucro(total, amounts, t.HostID); err != nil {
		return nil, err
	}

	shares := []FareShare{{
		UserID:      t.HostID,
		Role:        "host",
		FromKm:      0,
		ToKm:        routeKm,
		Seats:       1,
		AmountCents: amounts[t.HostID],
	}}
	for _, b := range active {
		shares = append(shares, FareShare{
			UserID:      b.PassengerID,
			Role:        "passenger",
			FromKm:      b.PickupAlongKm,
			ToKm:        b.DropoffAlongKm,
			Seats:       b.Seats,
			AmountCents: amounts[b.PassengerID],
		})
	}

	return &FareBreakdown{
		TripID:        t.ID,
		DistanceKm:    routeKm,
		DurationMin:   routeKm / s.cfg.SpeedKmh * 60,
		TotalCents:    total,
		SoloCostCents: total,
		Shares:        shares,
	}, nil
}

// --- Auxiliares ---

func (s *Service) tripCost(routeKm float64) int64 {
	return s.cfg.Tariff.TripCostCents(routeKm, routeKm/s.cfg.SpeedKmh*60)
}

// occupants devuelve quién va a bordo del trayecto: quien organiza durante todo
// el recorrido y cada reserva viva en su tramo.
func (s *Service) occupants(t *domain.Trip) ([]pricing.Occupant, error) {
	bookings, err := s.store.BookingsByTrip(t.ID)
	if err != nil {
		return nil, err
	}
	out := []pricing.Occupant{{ID: t.HostID, StartKm: 0, EndKm: t.DistanceKm(), Seats: 1}}
	for _, b := range bookings {
		if b.Status.Active() {
			out = append(out, pricing.Occupant{
				ID:      b.PassengerID,
				StartKm: b.PickupAlongKm,
				EndKm:   b.DropoffAlongKm,
				Seats:   b.Seats,
			})
		}
	}
	return out, nil
}

// releaseSeats devuelve plazas al trayecto tras un rechazo o una anulación.
func (s *Service) releaseSeats(t *domain.Trip, seats int) error {
	return s.store.ReleaseSeats(t.ID, seats)
}
