// Package simulacion mide cuánto sube la ocupación de la flota cuando la gente
// comparte trayecto.
//
// La pregunta que responde no es nuestra, es de Tesla: con una flota pequeña
// para un área metropolitana entera, ¿cuánta más demanda sirve el mismo número
// de coches si los viajes se comparten? Esa cifra es lo que convierte esta app
// de algo que Tesla puede cerrar en algo que a Tesla le interesa.
//
// La simulación usa el motor de emparejamiento real, no una aproximación: si el
// emparejador rechaza un viaje aquí, lo rechazaría también en producción.
package simulacion

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
)

// Zona es un punto de atracción de viajes dentro del área de servicio.
type Zona struct {
	Nombre string
	Punto  geo.Point
	// PesoOrigen y PesoDestino son la probabilidad relativa de que un viaje
	// empiece o termine aquí. No son simétricos: al aeropuerto se va mucho
	// más de lo que se sale de él hacia la ciudad en hora punta.
	PesoOrigen  float64
	PesoDestino float64
}

// ZonasAustin son puntos reales dentro del área donde opera el robotaxi.
//
// Los pesos son una estimación razonada de los patrones de una ciudad como
// Austin, no datos medidos. La conclusión de la simulación es robusta frente a
// ellos —lo que se compara es el mismo escenario con y sin compartir—, pero los
// valores absolutos no deben tomarse como una predicción.
var ZonasAustin = []Zona{
	{"Centro (Congress & 6th)", geo.Point{Lat: 30.2685, Lng: -97.7425}, 22, 20},
	{"Aeropuerto AUS", geo.Point{Lat: 30.1975, Lng: -97.6664}, 8, 18},
	{"The Domain", geo.Point{Lat: 30.4013, Lng: -97.7256}, 14, 13},
	{"East Riverside", geo.Point{Lat: 30.2380, Lng: -97.7180}, 13, 8},
	{"Campus UT / West Campus", geo.Point{Lat: 30.2861, Lng: -97.7394}, 12, 10},
	{"Mueller", geo.Point{Lat: 30.2988, Lng: -97.7050}, 8, 7},
	{"South Congress", geo.Point{Lat: 30.2489, Lng: -97.7501}, 9, 10},
	{"Pflugerville", geo.Point{Lat: 30.4394, Lng: -97.6200}, 7, 5},
	{"Gigafactory / Del Valle", geo.Point{Lat: 30.2206, Lng: -97.6183}, 5, 7},
	{"Zilker / Barton Springs", geo.Point{Lat: 30.2669, Lng: -97.7729}, 6, 8},
}

// Peticion es una persona que quiere ir de un sitio a otro a una hora.
type Peticion struct {
	ID            string
	Origen        geo.Point
	Destino       geo.Point
	NombreOrigen  string
	NombreDestino string
	Salida        time.Time
}

// DistanciaKm es la distancia en línea recta del viaje pedido.
func (p Peticion) DistanciaKm() float64 { return geo.DistanceKm(p.Origen, p.Destino) }

// GenerarDemanda produce un día de peticiones con la misma semilla siempre, de
// modo que los resultados se puedan reproducir y discutir.
func GenerarDemanda(n int, dia time.Time, semilla int64) []Peticion {
	r := rand.New(rand.NewSource(semilla))

	peticiones := make([]Peticion, 0, n)
	for i := 0; i < n; i++ {
		origen := elegirZona(r, func(z Zona) float64 { return z.PesoOrigen })
		destino := elegirZona(r, func(z Zona) float64 { return z.PesoDestino })
		// Nadie pide un viaje al sitio donde ya está.
		for intentos := 0; destino.Nombre == origen.Nombre && intentos < 10; intentos++ {
			destino = elegirZona(r, func(z Zona) float64 { return z.PesoDestino })
		}
		if destino.Nombre == origen.Nombre {
			continue
		}

		peticiones = append(peticiones, Peticion{
			ID:            fmt.Sprintf("pet_%04d", i),
			Origen:        dispersar(r, origen.Punto),
			Destino:       dispersar(r, destino.Punto),
			NombreOrigen:  origen.Nombre,
			NombreDestino: destino.Nombre,
			Salida:        horaDeSalida(r, dia),
		})
	}
	return peticiones
}

// elegirZona sortea una zona según el peso indicado.
func elegirZona(r *rand.Rand, peso func(Zona) float64) Zona {
	var total float64
	for _, z := range ZonasAustin {
		total += peso(z)
	}
	corte := r.Float64() * total
	var acumulado float64
	for _, z := range ZonasAustin {
		acumulado += peso(z)
		if corte <= acumulado {
			return z
		}
	}
	return ZonasAustin[len(ZonasAustin)-1]
}

// dispersar mueve el punto unos cientos de metros al azar: la gente no sale
// exactamente del mismo portal, y si lo hiciera el emparejamiento sería
// artificialmente fácil.
func dispersar(r *rand.Rand, p geo.Point) geo.Point {
	const radioKm = 0.8
	angulo := r.Float64() * 2 * math.Pi
	distancia := math.Sqrt(r.Float64()) * radioKm // uniforme sobre el área

	dLat := distancia / 111.0
	dLng := distancia / (111.0 * math.Cos(p.Lat*math.Pi/180))
	return geo.Point{
		Lat: p.Lat + dLat*math.Sin(angulo),
		Lng: p.Lng + dLng*math.Cos(angulo),
	}
}

// horaDeSalida reparte los viajes a lo largo del día con dos horas punta, la de
// la mañana y la de la tarde.
func horaDeSalida(r *rand.Rand, dia time.Time) time.Time {
	var hora float64
	switch u := r.Float64(); {
	case u < 0.28: // punta de mañana
		hora = 8 + r.NormFloat64()*0.8
	case u < 0.62: // punta de tarde
		hora = 17.5 + r.NormFloat64()*1.0
	default: // resto del día
		hora = 6 + r.Float64()*17
	}
	hora = math.Max(5, math.Min(23.5, hora))

	inicio := time.Date(dia.Year(), dia.Month(), dia.Day(), 0, 0, 0, 0, time.UTC)
	return inicio.Add(time.Duration(hora * float64(time.Hour)))
}
