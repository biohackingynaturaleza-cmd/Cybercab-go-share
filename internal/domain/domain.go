// Package domain define las entidades del producto: quién viaja, qué trayecto
// se comparte y qué plazas se reservan.
package domain

import (
	"errors"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
)

// ErrValidation envuelve cualquier fallo de validación de entrada.
var ErrValidation = errors.New("validación")

// VehicleType distingue los vehículos de la flota de robotaxis.
type VehicleType string

const (
	// VehicleCybercab es el Cybercab de producción: biplaza, sin volante.
	// Compartirlo significa una plaza para quien organiza y una para quien se une.
	VehicleCybercab VehicleType = "cybercab"
	// VehicleModelY es el robotaxi que Tesla opera hoy en Austin.
	VehicleModelY VehicleType = "model_y"
)

// Seats devuelve las plazas utilizables del vehículo.
func (v VehicleType) Seats() int {
	switch v {
	case VehicleCybercab:
		return 2
	case VehicleModelY:
		return 4
	default:
		return 0
	}
}

// Valid indica si el tipo de vehículo está soportado.
func (v VehicleType) Valid() bool { return v.Seats() > 0 }

// Place es un punto con nombre legible: "AUS Terminal Barbara Jordan".
type Place struct {
	Name  string    `json:"name"`
	Point geo.Point `json:"point"`
}

// User es una persona registrada en la app.
type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Rating    float64   `json:"rating"`
	RideCount int       `json:"ride_count"`
	CreatedAt time.Time `json:"created_at"`
}

// TripStatus es el ciclo de vida de un trayecto compartido.
type TripStatus string

const (
	TripOpen      TripStatus = "open"      // admite pasajeros
	TripFull      TripStatus = "full"      // sin plazas libres
	TripCancelled TripStatus = "cancelled" // anulado por quien lo organiza
	TripCompleted TripStatus = "completed" // ya realizado
)

// Trip es un trayecto que alguien ofrece compartir: reserva (o reservará) el
// robotaxi y abre las plazas libres a otros usuarios.
type Trip struct {
	ID          string `json:"id"`
	HostID      string `json:"host_id"`
	Origin      Place  `json:"origin"`
	Destination Place  `json:"destination"`
	// Route es la polilínea completa: origen, waypoints y destino.
	Route         geo.Route   `json:"route"`
	DepartureTime time.Time   `json:"departure_time"`
	Vehicle       VehicleType `json:"vehicle"`
	SeatsTotal    int         `json:"seats_total"`
	SeatsTaken    int         `json:"seats_taken"`
	// MaxDetourKm es cuánto acepta desviarse quien organiza para recoger o
	// dejar a alguien fuera de la línea de la ruta.
	MaxDetourKm float64    `json:"max_detour_km"`
	Notes       string     `json:"notes,omitempty"`
	Status      TripStatus `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
}

// SeatsAvailable son las plazas que quedan para pasajeros.
func (t *Trip) SeatsAvailable() int {
	free := t.SeatsTotal - t.SeatsTaken
	if free < 0 {
		return 0
	}
	return free
}

// DistanceKm es la longitud del trayecto completo.
func (t *Trip) DistanceKm() float64 { return t.Route.LengthKm() }

// Bookable indica si el trayecto admite todavía reservas.
func (t *Trip) Bookable() bool {
	return t.Status == TripOpen && t.SeatsAvailable() > 0
}

// Validate comprueba la coherencia del trayecto antes de guardarlo.
func (t *Trip) Validate() error {
	switch {
	case t.HostID == "":
		return errors.New("falta el usuario que organiza el trayecto")
	case !t.Vehicle.Valid():
		return errors.New("tipo de vehículo no soportado")
	case !t.Route.Valid():
		return errors.New("la ruta necesita al menos dos puntos válidos")
	case t.DepartureTime.IsZero():
		return errors.New("falta la hora de salida")
	case t.SeatsTotal < 1 || t.SeatsTotal > t.Vehicle.Seats()-1:
		// Una plaza siempre es de quien organiza; el resto son compartibles.
		return errors.New("plazas ofertadas fuera del aforo del vehículo")
	case t.MaxDetourKm < 0:
		return errors.New("el desvío máximo no puede ser negativo")
	}
	return nil
}

// BookingStatus es el ciclo de vida de una reserva.
type BookingStatus string

const (
	BookingPending   BookingStatus = "pending"   // esperando a quien organiza
	BookingConfirmed BookingStatus = "confirmed" // plaza asegurada
	BookingRejected  BookingStatus = "rejected"
	BookingCancelled BookingStatus = "cancelled"
)

// Active indica si la reserva ocupa plaza ahora mismo.
func (s BookingStatus) Active() bool {
	return s == BookingPending || s == BookingConfirmed
}

// Booking es la petición de un pasajero para recorrer un tramo del trayecto.
type Booking struct {
	ID          string `json:"id"`
	TripID      string `json:"trip_id"`
	PassengerID string `json:"passenger_id"`
	Pickup      Place  `json:"pickup"`
	Dropoff     Place  `json:"dropoff"`
	// PickupAlongKm y DropoffAlongKm sitúan el tramo sobre la ruta del viaje.
	PickupAlongKm  float64 `json:"pickup_along_km"`
	DropoffAlongKm float64 `json:"dropoff_along_km"`
	Seats          int     `json:"seats"`
	// PriceCents es la estimación mostrada al reservar; el reparto definitivo
	// se recalcula al cerrarse el trayecto.
	PriceCents int64         `json:"price_cents"`
	Status     BookingStatus `json:"status"`
	CreatedAt  time.Time     `json:"created_at"`
}

// SharedKm es la distancia que el pasajero recorre dentro del trayecto.
func (b *Booking) SharedKm() float64 { return b.DropoffAlongKm - b.PickupAlongKm }
