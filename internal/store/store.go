// Package store guarda usuarios, trayectos y reservas.
//
// La implementación de arranque es en memoria: basta para desarrollar y probar,
// y aísla al resto de la app detrás de la interfaz Store para poder cambiarla
// por Postgres sin tocar la lógica de negocio.
package store

import (
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/billing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// ErrNotFound se devuelve cuando el identificador no existe.
var ErrNotFound = errors.New("no encontrado")

// ErrEmailEnUso se devuelve al registrar un email ya dado de alta.
var ErrEmailEnUso = errors.New("ese email ya está registrado")

// ErrSinPlazas se devuelve cuando el trayecto no admite las plazas pedidas.
var ErrSinPlazas = errors.New("no quedan plazas libres")

// ErrComprobacionEnCurso se devuelve al abrir una comprobación de un tipo que
// esa persona ya tiene pendiente o superada.
var ErrComprobacionEnCurso = errors.New("ya tienes una comprobación de ese tipo en curso o superada")

// Store es el contrato de persistencia de la aplicación.
type Store interface {
	CreateUser(u *domain.User) error
	GetUser(id string) (*domain.User, error)
	GetUserByEmail(email string) (*domain.User, error)

	CreateTrip(t *domain.Trip) error
	GetTrip(id string) (*domain.Trip, error)
	UpdateTrip(t *domain.Trip) error
	ListOpenTrips() ([]*domain.Trip, error)
	// TripsByHost devuelve los trayectos de una persona en cualquier estado.
	// ListOpenTrips no sirve para esto: en cuanto un trayecto se llena o se
	// cierra desaparecería de la lista de quien lo organizó, que es justo
	// cuando más lo necesita.
	TripsByHost(hostID string) ([]*domain.Trip, error)

	// ReserveSeats ocupa plazas de forma atómica y devuelve ErrSinPlazas si no
	// quedan suficientes. Es una sola operación a propósito: leer las plazas,
	// decidir y luego escribirlas deja una ventana en la que dos personas
	// reservan el mismo asiento.
	ReserveSeats(tripID string, seats int) error
	// ReleaseSeats devuelve plazas al trayecto tras un rechazo o una anulación.
	ReleaseSeats(tripID string, seats int) error

	CreateBooking(b *domain.Booking) error
	GetBooking(id string) (*domain.Booking, error)
	UpdateBooking(b *domain.Booking) error
	BookingsByTrip(tripID string) ([]*domain.Booking, error)
	BookingsByPassenger(userID string) ([]*domain.Booking, error)

	// Comprobaciones de identidad
	CreateCheck(c *trust.Check) error
	UpdateCheck(c *trust.Check) error
	GetCheckByRef(providerRef string) (*trust.Check, error)
	ChecksByUser(userID string) ([]trust.Check, error)

	// Libro de apuntes
	CreateEntries(entries []billing.Entry) error
	PendingEntries() ([]billing.Entry, error)
	MarkSettled(entryIDs []string, settlementID string) error

	// Incidencias posteriores al viaje
	CreateIncidencia(i *domain.Incidencia) error
	GetIncidencia(id string) (*domain.Incidencia, error)
	UpdateIncidencia(i *domain.Incidencia) error
	IncidenciasDe(userID string) ([]*domain.Incidencia, error)

	// Bloqueos entre personas
	CreateBlock(blockerID, blockedID string) error
	DeleteBlock(blockerID, blockedID string) error
	// BlockedPairs devuelve a quién ha bloqueado userID y quién le ha
	// bloqueado a él. El bloqueo corta en las dos direcciones: quien bloquea
	// no quiere ver a la otra persona, y quien es bloqueado tampoco debe poder
	// buscarla.
	BlockedPairs(userID string) (map[string]bool, error)
}

// Memory es un Store en memoria, seguro para uso concurrente.
type Memory struct {
	mu    sync.RWMutex
	users map[string]*domain.User
	// byEmail indexa por email en minúsculas para garantizar la unicidad.
	byEmail     map[string]string
	trips       map[string]*domain.Trip
	bookings    map[string]*domain.Booking
	checks      map[string]*trust.Check
	entries     map[string]*billing.Entry
	incidencias map[string]*domain.Incidencia
	// blocks son pares "bloqueador|bloqueado".
	blocks map[string]bool
	// tripOrder preserva el orden de alta para que los listados sean estables.
	tripOrder []string
}

// NewMemory crea un almacén vacío.
func NewMemory() *Memory {
	return &Memory{
		users:       map[string]*domain.User{},
		byEmail:     map[string]string{},
		trips:       map[string]*domain.Trip{},
		bookings:    map[string]*domain.Booking{},
		checks:      map[string]*trust.Check{},
		entries:     map[string]*billing.Entry{},
		incidencias: map[string]*domain.Incidencia{},
		blocks:      map[string]bool{},
	}
}

var _ Store = (*Memory)(nil)

func (m *Memory) CreateUser(u *domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[u.ID]; ok {
		return errors.New("el usuario ya existe")
	}
	key := emailKey(u.Email)
	if _, ok := m.byEmail[key]; ok {
		return ErrEmailEnUso
	}
	cp := *u
	cp.Idioma = domain.NormalizarIdioma(cp.Idioma)
	m.users[u.ID] = &cp
	m.byEmail[key] = u.ID
	return nil
}

func (m *Memory) GetUserByEmail(email string) (*domain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.byEmail[emailKey(email)]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *m.users[id]
	return &cp, nil
}

// emailKey normaliza el email para comparar: los buzones no distinguen
// mayúsculas en la práctica, y "Ana@X.com" no debe poder registrarse dos veces.
func emailKey(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (m *Memory) GetUser(id string) (*domain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (m *Memory) CreateTrip(t *domain.Trip) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.trips[t.ID]; ok {
		return errors.New("el trayecto ya existe")
	}
	m.trips[t.ID] = cloneTrip(t)
	m.tripOrder = append(m.tripOrder, t.ID)
	return nil
}

func (m *Memory) GetTrip(id string) (*domain.Trip, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.trips[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneTrip(t), nil
}

func (m *Memory) UpdateTrip(t *domain.Trip) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.trips[t.ID]; !ok {
		return ErrNotFound
	}
	m.trips[t.ID] = cloneTrip(t)
	return nil
}

// ReserveSeats ocupa plazas bajo el mismo cerrojo que las comprueba.
func (m *Memory) ReserveSeats(tripID string, seats int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.trips[tripID]
	if !ok {
		return ErrNotFound
	}
	if t.Status != domain.TripOpen || t.SeatsAvailable() < seats {
		return ErrSinPlazas
	}
	t.SeatsTaken += seats
	if t.SeatsAvailable() == 0 {
		t.Status = domain.TripFull
	}
	return nil
}

// ReleaseSeats libera plazas y reabre el trayecto si estaba completo.
func (m *Memory) ReleaseSeats(tripID string, seats int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.trips[tripID]
	if !ok {
		return ErrNotFound
	}
	t.SeatsTaken -= seats
	if t.SeatsTaken < 0 {
		t.SeatsTaken = 0
	}
	if t.Status == domain.TripFull && t.SeatsAvailable() > 0 {
		t.Status = domain.TripOpen
	}
	return nil
}

func (m *Memory) ListOpenTrips() ([]*domain.Trip, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.Trip, 0, len(m.tripOrder))
	for _, id := range m.tripOrder {
		if t := m.trips[id]; t != nil && t.Status == domain.TripOpen {
			out = append(out, cloneTrip(t))
		}
	}
	return out, nil
}

func (m *Memory) TripsByHost(hostID string) ([]*domain.Trip, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.Trip, 0, 4)
	for _, id := range m.tripOrder {
		if t := m.trips[id]; t != nil && t.HostID == hostID {
			out = append(out, cloneTrip(t))
		}
	}
	return out, nil
}

func (m *Memory) CreateBooking(b *domain.Booking) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.bookings[b.ID]; ok {
		return errors.New("la reserva ya existe")
	}
	cp := *b
	m.bookings[b.ID] = &cp
	return nil
}

func (m *Memory) GetBooking(id string) (*domain.Booking, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.bookings[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *b
	return &cp, nil
}

func (m *Memory) UpdateBooking(b *domain.Booking) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.bookings[b.ID]; !ok {
		return ErrNotFound
	}
	cp := *b
	m.bookings[b.ID] = &cp
	return nil
}

func (m *Memory) BookingsByTrip(tripID string) ([]*domain.Booking, error) {
	return m.filterBookings(func(b *domain.Booking) bool { return b.TripID == tripID }), nil
}

func (m *Memory) BookingsByPassenger(userID string) ([]*domain.Booking, error) {
	return m.filterBookings(func(b *domain.Booking) bool { return b.PassengerID == userID }), nil
}

func (m *Memory) filterBookings(keep func(*domain.Booking) bool) []*domain.Booking {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Booking
	for _, b := range m.bookings {
		if keep(b) {
			cp := *b
			out = append(out, &cp)
		}
	}
	// Orden estable por identificador: las reservas se crean con IDs ordenables.
	sortBookings(out)
	return out
}

func cloneTrip(t *domain.Trip) *domain.Trip {
	cp := *t
	cp.Route = append(cp.Route[:0:0], t.Route...)
	return &cp
}

// --- Comprobaciones de identidad ---

func (m *Memory) CreateCheck(c *trust.Check) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.checks[c.ID]; ok {
		return errors.New("la comprobación ya existe")
	}
	// Una sola comprobación viva de cada tipo por persona: sin esto, un
	// rechazo se podría enterrar bajo intentos repetidos.
	for _, existente := range m.checks {
		if existente.UserID == c.UserID && existente.Kind == c.Kind &&
			(existente.Status == trust.StatusPending || existente.Status == trust.StatusVerified) {
			return ErrComprobacionEnCurso
		}
	}
	cp := *c
	m.checks[c.ID] = &cp
	return nil
}

func (m *Memory) UpdateCheck(c *trust.Check) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.checks[c.ID]; !ok {
		return ErrNotFound
	}
	cp := *c
	m.checks[c.ID] = &cp
	return nil
}

func (m *Memory) GetCheckByRef(providerRef string) (*trust.Check, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.checks {
		if c.ProviderRef == providerRef {
			cp := *c
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) ChecksByUser(userID string) ([]trust.Check, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []trust.Check
	for _, c := range m.checks {
		if c.UserID == userID {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// --- Bloqueos ---

func blockKey(blocker, blocked string) string { return blocker + "|" + blocked }

func (m *Memory) CreateBlock(blockerID, blockedID string) error {
	if blockerID == blockedID {
		return errors.New("no puedes bloquearte a ti mismo")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocks[blockKey(blockerID, blockedID)] = true
	return nil
}

func (m *Memory) DeleteBlock(blockerID, blockedID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.blocks, blockKey(blockerID, blockedID))
	return nil
}

func (m *Memory) BlockedPairs(userID string) (map[string]bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]bool{}
	for key := range m.blocks {
		blocker, blocked, _ := strings.Cut(key, "|")
		switch userID {
		case blocker:
			out[blocked] = true
		case blocked:
			out[blocker] = true
		}
	}
	return out, nil
}

// --- Libro de apuntes ---

// ErrApuntesDuplicados se devuelve al escribir apuntes que ya existen: es lo
// que impide cobrar dos veces por completar el mismo viaje.
var ErrApuntesDuplicados = errors.New("esos apuntes ya están en el libro")

// CreateEntries escribe apuntes. O entran todos o no entra ninguno: unos
// apuntes a medias dejarían una deuda sin su comisión, o al revés.
func (m *Memory) CreateEntries(entries []billing.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range entries {
		if err := e.Validate(); err != nil {
			return err
		}
		if _, ok := m.entries[e.ID]; ok {
			return ErrApuntesDuplicados
		}
		if e.BookingID != "" {
			for _, ya := range m.entries {
				if ya.BookingID == e.BookingID && ya.Kind == e.Kind {
					return ErrApuntesDuplicados
				}
			}
		}
	}
	for _, e := range entries {
		cp := e
		m.entries[e.ID] = &cp
	}
	return nil
}

func (m *Memory) PendingEntries() ([]billing.Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []billing.Entry
	for _, e := range m.entries {
		if e.Pendiente() {
			out = append(out, *e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// MarkSettled marca los apuntes como liquidados. Solo marca los que siguen
// pendientes: si otro cierre se adelantó, no se pisa su identificador.
func (m *Memory) MarkSettled(entryIDs []string, settlementID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range entryIDs {
		e, ok := m.entries[id]
		if !ok {
			return ErrNotFound
		}
		if e.Pendiente() {
			e.SettlementID = settlementID
		}
	}
	return nil
}
