package store

import (
	"context"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
)

// GeoQuery acota los trayectos candidatos a una búsqueda.
type GeoQuery struct {
	Pickup            geo.Point
	Dropoff           geo.Point
	EarliestDeparture time.Time
	LatestDeparture   time.Time
	Seats             int
	// MaxDistanceKm es lo que la ruta puede separarse de los puntos pedidos.
	MaxDistanceKm float64
}

// GeoSearcher lo implementan los almacenes capaces de filtrar por proximidad
// geográfica ellos mismos.
//
// Es una interfaz aparte a propósito: el almacén en memoria no la implementa y
// el servicio recurre entonces a recorrer los trayectos abiertos, que es
// razonable con pocos datos. Postgres sí la implementa, y ahí el índice
// espacial evita traerse toda la tabla.
type GeoSearcher interface {
	CandidateTrips(ctx context.Context, q GeoQuery) ([]*domain.Trip, error)
}
