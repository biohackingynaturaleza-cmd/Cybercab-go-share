// Package store guarda usuarios, trayectos y reservas.
//
// La implementación de arranque es en memoria: basta para desarrollar y probar,
// y aísla al resto de la app detrás de la interfaz Store para poder cambiarla
// por Postgres sin tocar la lógica de negocio.
package store

import (
	"errors"
	"sync"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
)

// ErrNotFound se devuelve cuando el identificador no existe.
var ErrNotFound = errors.New("no encontrado")

// Store es el contrato de persistencia de la aplicación.
type Store interface {
	CreateUser(u *domain.User) error
	GetUser(id string) (*domain.User, error)

	CreateTrip(t *domain.Trip) error
	GetTrip(id string) (*domain.Trip, error)
	UpdateTrip(t *domain.Trip) error
	ListOpenTrips() ([]*domain.Trip, error)

	CreateBooking(b *domain.Booking) error
	GetBooking(id string) (*domain.Booking, error)
	UpdateBooking(b *domain.Booking) error
	BookingsByTrip(tripID string) ([]*domain.Booking, error)
	BookingsByPassenger(userID string) ([]*domain.Booking, error)
}

// Memory es un Store en memoria, seguro para uso concurrente.
type Memory struct {
	mu       sync.RWMutex
	users    map[string]*domain.User
	trips    map[string]*domain.Trip
	bookings map[string]*domain.Booking
	// tripOrder preserva el orden de alta para que los listados sean estables.
	tripOrder []string
}

// NewMemory crea un almacén vacío.
func NewMemory() *Memory {
	return &Memory{
		users:    map[string]*domain.User{},
		trips:    map[string]*domain.Trip{},
		bookings: map[string]*domain.Booking{},
	}
}

var _ Store = (*Memory)(nil)

func (m *Memory) CreateUser(u *domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[u.ID]; ok {
		return errors.New("el usuario ya existe")
	}
	cp := *u
	m.users[u.ID] = &cp
	return nil
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
