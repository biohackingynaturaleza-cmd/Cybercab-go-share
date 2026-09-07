// Package geo contiene las utilidades geográficas mínimas para calcular
// distancias y para proyectar un punto sobre la ruta de un viaje.
//
// No dependemos de un motor de rutas externo: la ruta se modela como una
// polilínea (origen, waypoints opcionales, destino) y todas las distancias se
// calculan sobre ella. Es suficiente para decidir si una recogida "queda de
// camino", que es la pregunta central del producto.
package geo

import "math"

// EarthRadiusKm es el radio medio de la Tierra (WGS-84).
const EarthRadiusKm = 6371.0088

// Point es una coordenada geográfica en grados decimales.
type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Valid indica si el punto cae dentro del rango de coordenadas válido.
func (p Point) Valid() bool {
	return p.Lat >= -90 && p.Lat <= 90 && p.Lng >= -180 && p.Lng <= 180
}

// DistanceKm devuelve la distancia de círculo máximo entre dos puntos.
func DistanceKm(a, b Point) float64 {
	lat1 := rad(a.Lat)
	lat2 := rad(b.Lat)
	dLat := lat2 - lat1
	dLng := rad(b.Lng - a.Lng)

	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * EarthRadiusKm * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Route es la polilínea que recorre un viaje, del origen al destino.
type Route []Point

// Valid comprueba que la ruta tenga al menos dos puntos válidos.
func (r Route) Valid() bool {
	if len(r) < 2 {
		return false
	}
	for _, p := range r {
		if !p.Valid() {
			return false
		}
	}
	return true
}

// LengthKm es la longitud total de la ruta.
func (r Route) LengthKm() float64 {
	var total float64
	for i := 1; i < len(r); i++ {
		total += DistanceKm(r[i-1], r[i])
	}
	return total
}

// Projection describe dónde "engancha" un punto con la ruta.
type Projection struct {
	// AlongKm es el kilómetro de la ruta en el que se produce el enganche,
	// medido desde el origen. Ordena las paradas entre sí.
	AlongKm float64
	// OffRouteKm es lo que el punto se separa de la ruta: cuánto tendría que
	// caminar el pasajero (o desviarse el coche) para llegar a él.
	OffRouteKm float64
	// SegmentIndex es el tramo de la polilínea sobre el que cae la proyección.
	SegmentIndex int
}

// Project busca el punto de la ruta más cercano a p.
//
// Trabajamos en un plano local equirectangular centrado en p: a escala urbana
// el error es despreciable y nos permite proyectar sobre cada segmento con
// álgebra vectorial simple.
func (r Route) Project(p Point) Projection {
	best := Projection{OffRouteKm: math.Inf(1)}
	if len(r) == 0 {
		return best
	}
	if len(r) == 1 {
		return Projection{AlongKm: 0, OffRouteKm: DistanceKm(r[0], p)}
	}

	cosLat := math.Cos(rad(p.Lat))
	// toPlane convierte a kilómetros relativos a p.
	toPlane := func(q Point) (x, y float64) {
		x = rad(q.Lng-p.Lng) * cosLat * EarthRadiusKm
		y = rad(q.Lat-p.Lat) * EarthRadiusKm
		return
	}

	var traveled float64
	for i := 1; i < len(r); i++ {
		ax, ay := toPlane(r[i-1])
		bx, by := toPlane(r[i])
		dx, dy := bx-ax, by-ay

		segLen := math.Hypot(dx, dy)
		var t float64
		if segLen > 0 {
			// p está en el origen del plano, así que el vector a->p es (-ax, -ay).
			t = clamp((-ax*dx-ay*dy)/(segLen*segLen), 0, 1)
		}

		cx, cy := ax+t*dx, ay+t*dy
		off := math.Hypot(cx, cy)

		// El avance se mide con la longitud haversine del tramo, la misma que
		// usa LengthKm: así AlongKm nunca se sale de la longitud de la ruta.
		segKm := DistanceKm(r[i-1], r[i])
		if off < best.OffRouteKm {
			best = Projection{
				AlongKm:      traveled + t*segKm,
				OffRouteKm:   off,
				SegmentIndex: i - 1,
			}
		}
		traveled += segKm
	}
	return best
}

func rad(deg float64) float64 { return deg * math.Pi / 180 }

func clamp(v, lo, hi float64) float64 {
	return math.Min(hi, math.Max(lo, v))
}
