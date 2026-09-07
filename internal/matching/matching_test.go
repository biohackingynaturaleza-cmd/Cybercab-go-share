package matching

import (
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pricing"
)

var (
	downtown  = geo.Point{Lat: 30.2685, Lng: -97.7425}
	riverside = geo.Point{Lat: 30.2380, Lng: -97.7180}
	airport   = geo.Point{Lat: 30.1975, Lng: -97.6664}
	salida    = time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC)
)

func tripCentroAeropuerto() *domain.Trip {
	return &domain.Trip{
		ID:            "trip_1",
		HostID:        "ana",
		Origin:        domain.Place{Name: "Centro", Point: downtown},
		Destination:   domain.Place{Name: "AUS", Point: airport},
		Route:         geo.Route{downtown, airport},
		DepartureTime: salida,
		Vehicle:       domain.VehicleModelY,
		SeatsTotal:    3,
		MaxDetourKm:   2,
		Status:        domain.TripOpen,
	}
}

func occupancyDe(t *domain.Trip) Occupancy {
	return Occupancy{t.ID: {{ID: t.HostID, StartKm: 0, EndKm: t.DistanceKm(), Seats: 1}}}
}

func find(trips []*domain.Trip, q Query) []Match {
	occ := Occupancy{}
	for _, t := range trips {
		for k, v := range occupancyDe(t) {
			occ[k] = v
		}
	}
	return Find(trips, q, occ, pricing.DefaultTariff(), 45)
}

func TestFindEncuentraElTrayectoDeCamino(t *testing.T) {
	trip := tripCentroAeropuerto()
	matches := find([]*domain.Trip{trip}, Query{
		Pickup:  riverside, // queda dentro del corredor centro→aeropuerto
		Dropoff: airport,
	})

	if len(matches) != 1 {
		t.Fatalf("resultados = %d, esperaba 1", len(matches))
	}
	m := matches[0]
	if m.SharedKm <= 0 {
		t.Errorf("SharedKm = %.2f, esperaba un tramo compartido positivo", m.SharedKm)
	}
	if m.EstimatedPriceCents <= 0 {
		t.Errorf("precio estimado = %d, esperaba un importe positivo", m.EstimatedPriceCents)
	}
	if m.DropoffAlongKm <= m.PickupAlongKm {
		t.Errorf("la bajada (%.2f) debe ir después de la recogida (%.2f)", m.DropoffAlongKm, m.PickupAlongKm)
	}
}

func TestFindDescartaElSentidoContrario(t *testing.T) {
	trip := tripCentroAeropuerto()
	// Pedimos aeropuerto → centro: el coche va justo al revés.
	matches := find([]*domain.Trip{trip}, Query{Pickup: airport, Dropoff: downtown})

	if len(matches) != 0 {
		t.Fatalf("resultados = %d, esperaba 0: el trayecto va en sentido contrario", len(matches))
	}
}

func TestFindDescartaPuntosLejosDeLaRuta(t *testing.T) {
	trip := tripCentroAeropuerto()
	lejos := geo.Point{Lat: 30.5100, Lng: -97.6800} // ~28 km al norte

	matches := find([]*domain.Trip{trip}, Query{Pickup: lejos, Dropoff: airport})
	if len(matches) != 0 {
		t.Fatalf("resultados = %d, esperaba 0: la recogida queda fuera de ruta", len(matches))
	}
}

func TestFindRespetaLaHorquillaHoraria(t *testing.T) {
	trip := tripCentroAeropuerto()

	tarde := find([]*domain.Trip{trip}, Query{
		Pickup: riverside, Dropoff: airport,
		EarliestDeparture: salida.Add(time.Hour),
	})
	if len(tarde) != 0 {
		t.Errorf("un trayecto que sale antes de la horquilla no debería aparecer")
	}

	dentro := find([]*domain.Trip{trip}, Query{
		Pickup: riverside, Dropoff: airport,
		EarliestDeparture: salida.Add(-time.Hour),
		LatestDeparture:   salida.Add(time.Hour),
	})
	if len(dentro) != 1 {
		t.Errorf("resultados dentro de la horquilla = %d, esperaba 1", len(dentro))
	}
}

func TestFindDescartaTrayectosSinPlazasSuficientes(t *testing.T) {
	trip := tripCentroAeropuerto()
	trip.SeatsTotal = 1

	if got := find([]*domain.Trip{trip}, Query{Pickup: riverside, Dropoff: airport, Seats: 2}); len(got) != 0 {
		t.Fatalf("resultados = %d, esperaba 0: piden 2 plazas y solo hay 1", len(got))
	}
}

func TestFindDescartaTrayectosCerradosOCompletos(t *testing.T) {
	for _, status := range []domain.TripStatus{domain.TripCancelled, domain.TripFull, domain.TripCompleted} {
		trip := tripCentroAeropuerto()
		trip.Status = status
		if got := find([]*domain.Trip{trip}, Query{Pickup: riverside, Dropoff: airport}); len(got) != 0 {
			t.Errorf("estado %q: resultados = %d, esperaba 0", status, len(got))
		}
	}
}

func TestFindOrdenaPorMejorEncaje(t *testing.T) {
	// El primero cubre el viaje entero; el segundo obliga a caminar bastante.
	bueno := tripCentroAeropuerto()
	bueno.ID = "bueno"

	regular := tripCentroAeropuerto()
	regular.ID = "regular"
	// Ruta paralela desplazada ~1,5 km al norte.
	regular.Route = geo.Route{
		{Lat: downtown.Lat + 0.0135, Lng: downtown.Lng},
		{Lat: airport.Lat + 0.0135, Lng: airport.Lng},
	}
	regular.MaxDetourKm = 3

	matches := find([]*domain.Trip{regular, bueno}, Query{
		Pickup: riverside, Dropoff: airport, MaxWalkKm: 3,
	})
	if len(matches) != 2 {
		t.Fatalf("resultados = %d, esperaba 2", len(matches))
	}
	if matches[0].Trip.ID != "bueno" {
		t.Fatalf("primer resultado = %q, esperaba \"bueno\" (menos que caminar)", matches[0].Trip.ID)
	}
	if matches[0].Score < matches[1].Score {
		t.Errorf("la puntuación no está ordenada de mayor a menor")
	}
}

func TestFindDescartaTramosDemasiadoCortos(t *testing.T) {
	trip := tripCentroAeropuerto()
	// Recogida y bajada prácticamente en el mismo sitio.
	cerca := geo.Point{Lat: riverside.Lat + 0.0005, Lng: riverside.Lng}

	if got := find([]*domain.Trip{trip}, Query{Pickup: riverside, Dropoff: cerca}); len(got) != 0 {
		t.Fatalf("resultados = %d, esperaba 0: el tramo compartido es insignificante", len(got))
	}
}
