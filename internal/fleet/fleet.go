// Package fleet es la frontera con la flota de robotaxis.
//
// Estado de la realidad, a día de hoy: Tesla no publica ninguna API para que
// una aplicación de terceros pida un robotaxi. La Fleet API existe, pero sirve
// para que el dueño de un coche controle su propio coche —telemetría, comandos,
// carga—, no para llamar a un vehículo del servicio. Los viajes de Austin se
// piden desde la app de Tesla y punto.
//
// Por eso el modelo que funciona hoy es el traspaso: esta app empareja a la
// gente, calcula el reparto y coordina el encuentro; quien organiza pide el
// coche en la app de Tesla y registra aquí la referencia del viaje. Todo eso
// vive detrás de la interfaz Provider, de modo que el día que exista una API
// real se implementa otro Provider y no cambia nada más.
package fleet

import (
	"context"
	"errors"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
)

// RideStatus es el estado del viaje en la flota.
type RideStatus string

const (
	// RidePendiente aún no se ha pedido el vehículo.
	RidePendiente RideStatus = "pendiente"
	// RideSolicitado se ha pedido y se espera confirmación.
	RideSolicitado RideStatus = "solicitado"
	// RideAsignado hay un vehículo en camino.
	RideAsignado RideStatus = "asignado"
	// RideEnCurso el viaje ha empezado.
	RideEnCurso RideStatus = "en_curso"
	// RideFinalizado el viaje ha terminado.
	RideFinalizado RideStatus = "finalizado"
	// RideCancelado el viaje se anuló.
	RideCancelado RideStatus = "cancelado"
)

// Ride es lo que sabemos del viaje en la flota.
type Ride struct {
	// Ref identifica el viaje en el proveedor. En el modo de traspaso es la
	// referencia que la persona copia de la app de Tesla.
	Ref    string     `json:"ref"`
	Status RideStatus `json:"status"`
	// Vehicle describe el coche asignado, cuando se conoce.
	Vehicle string `json:"vehicle,omitempty"`
	// Position es dónde está el vehículo, si el proveedor lo publica.
	Position *geo.Point `json:"position,omitempty"`
	// ETA es cuándo se espera que llegue a recoger.
	ETA time.Time `json:"eta,omitzero"`
	// FareCents es el importe real cobrado por la flota. Mientras no se sepa,
	// el reparto usa la estimación de la tarifa.
	FareCents int64     `json:"fare_cents,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RideRequest es lo que se le pide a la flota.
type RideRequest struct {
	Pickup      geo.Point
	Dropoff     geo.Point
	Waypoints   []geo.Point
	Passengers  int
	DepartureAt time.Time
}

// ErrNoSoportado se devuelve cuando el proveedor no puede hacer algo por sí
// mismo y hace falta una persona.
var ErrNoSoportado = errors.New("el proveedor de flota no puede hacer esto automáticamente")

// Provider es la frontera con la flota de robotaxis.
type Provider interface {
	// Request pide un vehículo. Un proveedor de traspaso devuelve
	// ErrNoSoportado con las instrucciones para pedirlo a mano.
	Request(ctx context.Context, req RideRequest) (*Ride, error)
	// Status consulta el estado de un viaje ya pedido.
	Status(ctx context.Context, ref string) (*Ride, error)
	// Cancel anula un viaje.
	Cancel(ctx context.Context, ref string) error
	// Nombre identifica al proveedor en la interfaz de usuario.
	Nombre() string
}
