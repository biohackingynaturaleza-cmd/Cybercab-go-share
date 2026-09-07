package simulacion

import (
	"fmt"
	"sort"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/matching"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pricing"
)

// Config son los parámetros de una simulación.
type Config struct {
	Vehiculo domain.VehicleType
	// VentanaMin es la flexibilidad horaria de quien viaja: cuántos minutos
	// acepta adelantar o retrasar su salida para poder compartir.
	VentanaMin int
	// MaxCaminarKm es lo que acepta caminar hasta el punto de recogida.
	MaxCaminarKm float64
}

// PorDefecto son unos parámetros conservadores: poca flexibilidad horaria y
// poco paseo. Con más de ambos el emparejamiento mejora, así que estos números
// son un suelo, no un techo.
func PorDefecto(v domain.VehicleType) Config {
	return Config{Vehiculo: v, VentanaMin: 10, MaxCaminarKm: 1.0}
}

// Resultado es lo que sale de simular un día.
type Resultado struct {
	Vehiculo   domain.VehicleType `json:"vehiculo"`
	Peticiones int                `json:"peticiones"`

	// Sin compartir: cada petición ocupa un vehículo para ella sola.
	ViajesSinCompartir     int     `json:"viajes_sin_compartir"`
	KmVehiculoSinCompartir float64 `json:"km_vehiculo_sin_compartir"`

	// Compartiendo.
	ViajesCompartiendo     int     `json:"viajes_compartiendo"`
	KmVehiculoCompartiendo float64 `json:"km_vehiculo_compartiendo"`
	PeticionesEmparejadas  int     `json:"peticiones_emparejadas"`

	// KmPasajero es el transporte útil entregado: no cambia entre escenarios,
	// porque la gente hace los mismos viajes. Lo que cambia es lo que cuesta
	// entregarlo.
	KmPasajero float64 `json:"km_pasajero"`

	// Derivados.
	ReduccionKm           float64 `json:"reduccion_km"`        // fracción de km de vehículo ahorrados
	TasaEmparejamiento    float64 `json:"tasa_emparejamiento"` // fracción de peticiones que comparten
	OcupacionSinCompartir float64 `json:"ocupacion_sin_compartir"`
	OcupacionCompartiendo float64 `json:"ocupacion_compartiendo"`
	// FactorDeCapacidad es cuántas veces más demanda sirve la misma flota.
	FactorDeCapacidad float64 `json:"factor_de_capacidad"`

	// AhorroPasajerosCents es lo que se ahorra la gente en total ese día.
	AhorroPasajerosCents int64 `json:"ahorro_pasajeros_cents"`
}

// Simular corre el mismo día dos veces —sin compartir y compartiendo— y compara.
func Simular(peticiones []Peticion, cfg Config, tarifa pricing.Tariff) *Resultado {
	if cfg.VentanaMin <= 0 {
		cfg.VentanaMin = 10
	}
	if cfg.MaxCaminarKm <= 0 {
		cfg.MaxCaminarKm = 1.0
	}

	res := &Resultado{Vehiculo: cfg.Vehiculo, Peticiones: len(peticiones)}

	// Escenario base: un vehículo por petición.
	for _, p := range peticiones {
		km := p.DistanciaKm()
		res.KmVehiculoSinCompartir += km
		res.KmPasajero += km
	}
	res.ViajesSinCompartir = len(peticiones)

	// Escenario compartido. Las peticiones se procesan por hora de salida, que
	// es como llegarían en la vida real: nadie puede emparejarse con algo que
	// todavía no se ha pedido.
	orden := make([]Peticion, len(peticiones))
	copy(orden, peticiones)
	sort.Slice(orden, func(i, j int) bool { return orden[i].Salida.Before(orden[j].Salida) })

	var abiertos []*domain.Trip
	// ocupacion registra los tramos ocupados de cada trayecto, para valorar el
	// precio del siguiente que se sume.
	ocupacion := matching.Occupancy{}

	for i, p := range orden {
		ventana := time.Duration(cfg.VentanaMin) * time.Minute
		q := matching.Query{
			Pickup:            p.Origen,
			Dropoff:           p.Destino,
			EarliestDeparture: p.Salida.Add(-ventana),
			LatestDeparture:   p.Salida.Add(ventana),
			Seats:             1,
			MaxWalkKm:         cfg.MaxCaminarKm,
		}

		// El mismo emparejador que usa la aplicación.
		matches := matching.Find(abiertos, q, ocupacion, tarifa, 45)
		if len(matches) > 0 {
			m := matches[0]
			t := m.Trip
			t.SeatsTaken++
			if t.SeatsAvailable() == 0 {
				t.Status = domain.TripFull
			}
			ocupacion[t.ID] = append(ocupacion[t.ID], pricing.Occupant{
				ID: p.ID, StartKm: m.PickupAlongKm, EndKm: m.DropoffAlongKm, Seats: 1,
			})
			res.PeticionesEmparejadas++
			// No se añaden kilómetros de vehículo: el coche ya iba a hacer ese
			// camino. Es exactamente el ahorro que produce compartir.
			continue
		}

		// Nadie con quien compartir: sale un vehículo nuevo.
		nuevo := nuevoTrayecto(i, p, cfg.Vehiculo)
		abiertos = append(abiertos, nuevo)
		ocupacion[nuevo.ID] = []pricing.Occupant{
			{ID: p.ID, StartKm: 0, EndKm: nuevo.DistanceKm(), Seats: 1},
		}
		res.ViajesCompartiendo++
		res.KmVehiculoCompartiendo += nuevo.DistanceKm()

		abiertos = purgar(abiertos, p.Salida, ventana)
	}

	res.calcular(tarifa)
	return res
}

// nuevoTrayecto convierte una petición en un trayecto que otros podrán
// compartir.
func nuevoTrayecto(i int, p Peticion, v domain.VehicleType) *domain.Trip {
	ruta := geo.Route{p.Origen, p.Destino}
	return &domain.Trip{
		ID:            fmt.Sprintf("trip_%04d", i),
		HostID:        p.ID,
		Origin:        domain.Place{Name: p.NombreOrigen, Point: p.Origen},
		Destination:   domain.Place{Name: p.NombreDestino, Point: p.Destino},
		Route:         ruta,
		DepartureTime: p.Salida,
		Vehicle:       v,
		// Una plaza es de quien organiza; el resto se comparten.
		SeatsTotal:  v.Seats() - 1,
		MaxDetourKm: 1.5,
		Status:      domain.TripOpen,
	}
}

// purgar descarta los trayectos cuya hora ya pasó: no hace falta seguir
// comparando con coches que ya han salido.
func purgar(abiertos []*domain.Trip, ahora time.Time, ventana time.Duration) []*domain.Trip {
	corte := ahora.Add(-ventana)
	vivos := abiertos[:0]
	for _, t := range abiertos {
		if t.DepartureTime.After(corte) && t.Status == domain.TripOpen {
			vivos = append(vivos, t)
		}
	}
	return vivos
}

func (r *Resultado) calcular(tarifa pricing.Tariff) {
	if r.KmVehiculoSinCompartir > 0 {
		r.ReduccionKm = 1 - r.KmVehiculoCompartiendo/r.KmVehiculoSinCompartir
		r.OcupacionSinCompartir = r.KmPasajero / r.KmVehiculoSinCompartir
	}
	if r.KmVehiculoCompartiendo > 0 {
		r.OcupacionCompartiendo = r.KmPasajero / r.KmVehiculoCompartiendo
		// La misma flota recorre los mismos kilómetros; si cada kilómetro
		// transporta más gente, sirve proporcionalmente más demanda.
		r.FactorDeCapacidad = r.KmVehiculoSinCompartir / r.KmVehiculoCompartiendo
	}
	if r.Peticiones > 0 {
		r.TasaEmparejamiento = float64(r.PeticionesEmparejadas) / float64(r.Peticiones)
	}

	// Lo que la gente deja de gastar: los kilómetros de vehículo que no se
	// recorren, valorados a la tarifa.
	kmAhorrados := r.KmVehiculoSinCompartir - r.KmVehiculoCompartiendo
	viajesAhorrados := r.ViajesSinCompartir - r.ViajesCompartiendo
	r.AhorroPasajerosCents = int64(kmAhorrados*float64(tarifa.PerKmCents)) +
		int64(viajesAhorrados)*tarifa.BaseCents
}
