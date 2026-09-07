// Package routing calcula la ruta real entre dos puntos.
//
// La app no depende de un proveedor concreto: todo pasa por la interfaz Router,
// con una implementación sobre OSRM y otra en línea recta como respaldo.
package routing

import (
	"context"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
)

// Route es el resultado de calcular un trayecto.
type Route struct {
	// Geometry es la polilínea que sigue el vehículo.
	Geometry geo.Route
	// DistanceKm y DurationMin son los que da el proveedor, no una estimación.
	DistanceKm  float64
	DurationMin float64
	// Source identifica al proveedor, para poder distinguir en la interfaz una
	// ruta de calle real de una aproximación.
	Source string
}

// Router calcula la ruta que une una serie de puntos en orden.
type Router interface {
	// Route devuelve la ruta que pasa por todos los puntos dados.
	Route(ctx context.Context, points []geo.Point) (*Route, error)
}

// SourceStraightLine identifica las rutas aproximadas en línea recta.
const SourceStraightLine = "straight_line"

// StraightLine une los puntos con segmentos rectos y estima la duración con una
// velocidad media. No conoce las calles: es el respaldo cuando no hay motor de
// rutas disponible, y lo que usan las pruebas para no depender de la red.
type StraightLine struct {
	// SpeedKmh es la velocidad media asumida.
	SpeedKmh float64
}

// NewStraightLine construye el router de respaldo.
func NewStraightLine(speedKmh float64) *StraightLine {
	if speedKmh <= 0 {
		speedKmh = 45
	}
	return &StraightLine{SpeedKmh: speedKmh}
}

var _ Router = (*StraightLine)(nil)

// Route une los puntos tal cual y estima distancia y duración.
func (s *StraightLine) Route(_ context.Context, points []geo.Point) (*Route, error) {
	if len(points) < 2 {
		return nil, ErrRutaInsuficiente
	}
	line := geo.Route(append([]geo.Point(nil), points...))
	km := line.LengthKm()
	return &Route{
		Geometry:    line,
		DistanceKm:  km,
		DurationMin: km / s.SpeedKmh * 60,
		Source:      SourceStraightLine,
	}, nil
}
