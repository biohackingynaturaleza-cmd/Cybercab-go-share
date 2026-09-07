package geo

import (
	"math"
	"testing"
)

var (
	downtown = Point{Lat: 30.2685, Lng: -97.7425}
	airport  = Point{Lat: 30.1975, Lng: -97.6664}
)

func TestDistanceKmCentroAeropuerto(t *testing.T) {
	// En línea recta el centro de Austin queda a ~11 km del aeropuerto.
	got := DistanceKm(downtown, airport)
	if math.Abs(got-11.0) > 1.0 {
		t.Fatalf("distancia centro→AUS = %.2f km, esperaba ~11 km", got)
	}
}

func TestDistanceKmEsSimetricaYNulaEnElMismoPunto(t *testing.T) {
	if d := DistanceKm(downtown, downtown); d != 0 {
		t.Fatalf("distancia a uno mismo = %v, esperaba 0", d)
	}
	if a, b := DistanceKm(downtown, airport), DistanceKm(airport, downtown); math.Abs(a-b) > 1e-9 {
		t.Fatalf("distancia no simétrica: %v vs %v", a, b)
	}
}

func TestProjectPuntoIntermedioCaeSobreLaRuta(t *testing.T) {
	route := Route{downtown, airport}
	// Punto medio exacto del segmento.
	mid := Point{Lat: (downtown.Lat + airport.Lat) / 2, Lng: (downtown.Lng + airport.Lng) / 2}

	p := route.Project(mid)
	if p.OffRouteKm > 0.05 {
		t.Fatalf("el punto medio se separa %.3f km de la ruta, esperaba ~0", p.OffRouteKm)
	}
	if want := route.LengthKm() / 2; math.Abs(p.AlongKm-want) > 0.2 {
		t.Fatalf("AlongKm = %.2f, esperaba ~%.2f", p.AlongKm, want)
	}
}

func TestProjectOrdenaLosPuntosEnElSentidoDeLaMarcha(t *testing.T) {
	route := Route{downtown, airport}
	cerca := Point{Lat: 30.2600, Lng: -97.7330} // más cerca del centro
	lejos := Point{Lat: 30.2100, Lng: -97.6800} // más cerca del aeropuerto

	if a, b := route.Project(cerca).AlongKm, route.Project(lejos).AlongKm; a >= b {
		t.Fatalf("orden incorrecto: %.2f km debería ir antes que %.2f km", a, b)
	}
}

func TestProjectDetectaUnPuntoFueraDeRuta(t *testing.T) {
	route := Route{downtown, airport}
	// ~9 km al norte del centro, claramente fuera del corredor.
	fuera := Point{Lat: 30.3500, Lng: -97.7425}

	if off := route.Project(fuera).OffRouteKm; off < 5 {
		t.Fatalf("OffRouteKm = %.2f, esperaba una separación clara (>5 km)", off)
	}
}

func TestProjectUsaLosWaypointsDeLaPolilinea(t *testing.T) {
	waypoint := Point{Lat: 30.2380, Lng: -97.7180}
	directa := Route{downtown, airport}
	conDesvio := Route{downtown, waypoint, airport}

	if conDesvio.LengthKm() < directa.LengthKm() {
		t.Fatal("la ruta con waypoint no puede ser más corta que la directa")
	}
	if off := conDesvio.Project(waypoint).OffRouteKm; off > 0.01 {
		t.Fatalf("el waypoint debería caer sobre su propia ruta, off = %.3f km", off)
	}
}

func TestRouteValid(t *testing.T) {
	cases := map[string]struct {
		route Route
		want  bool
	}{
		"vacía":            {Route{}, false},
		"un solo punto":    {Route{downtown}, false},
		"origen y destino": {Route{downtown, airport}, true},
		"latitud inválida": {Route{{Lat: 120, Lng: 0}, airport}, false},
	}
	for name, tc := range cases {
		if got := tc.route.Valid(); got != tc.want {
			t.Errorf("%s: Valid() = %v, esperaba %v", name, got, tc.want)
		}
	}
}

func TestProjectNuncaSuperaLaLongitudDeLaRuta(t *testing.T) {
	// AlongKm y LengthKm tienen que medir con la misma vara: si no, la parada
	// final "cae" fuera de la ruta y el reparto del coste se descuadra.
	route := Route{downtown, {Lat: 30.2380, Lng: -97.7180}, airport}
	total := route.LengthKm()

	for _, p := range []Point{downtown, airport, {Lat: 30.2380, Lng: -97.7180}} {
		got := route.Project(p).AlongKm
		if got < 0 || got > total+1e-9 {
			t.Errorf("Project(%v).AlongKm = %.6f, fuera de [0, %.6f]", p, got, total)
		}
	}
	if got := route.Project(airport).AlongKm; math.Abs(got-total) > 1e-6 {
		t.Errorf("el destino proyecta en %.6f km, esperaba el final de la ruta (%.6f km)", got, total)
	}
}
