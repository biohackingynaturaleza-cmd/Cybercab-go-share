package store_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
)

var (
	downtown  = geo.Point{Lat: 30.2685, Lng: -97.7425}
	riverside = geo.Point{Lat: 30.2380, Lng: -97.7180}
	airport   = geo.Point{Lat: 30.1975, Lng: -97.6664}
	roundRock = geo.Point{Lat: 30.5083, Lng: -97.6789} // ~28 km al norte
)

// newPostgres abre el almacén contra la base de datos de pruebas y deja las
// tablas vacías. Sin TEST_DATABASE_URL las pruebas se saltan, para que quien
// no tenga Postgres levantado pueda seguir trabajando.
func newPostgres(t *testing.T) *store.Postgres {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL sin definir: se omiten las pruebas de Postgres")
	}
	ctx := context.Background()
	pg, err := store.NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	if err := pg.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := pg.TruncateAll(ctx); err != nil {
		t.Fatalf("limpiando las tablas: %v", err)
	}
	t.Cleanup(pg.Close)
	return pg
}

func nuevoUsuario(t *testing.T, pg *store.Postgres, id, email string) *domain.User {
	t.Helper()
	u := &domain.User{
		ID: id, Name: "Prueba " + id, Email: email,
		PasswordHash: "$2a$10$hash-de-prueba", Rating: 5,
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := pg.CreateUser(u); err != nil {
		t.Fatalf("CreateUser(%s): %v", id, err)
	}
	return u
}

func nuevoTrayecto(t *testing.T, pg *store.Postgres, id, hostID string, route geo.Route, plazas int) *domain.Trip {
	t.Helper()
	tr := &domain.Trip{
		ID: id, HostID: hostID,
		Origin:        domain.Place{Name: "Centro", Point: route[0]},
		Destination:   domain.Place{Name: "AUS", Point: route[len(route)-1]},
		Route:         route,
		DurationMin:   17.4,
		RouteSource:   "osrm",
		DepartureTime: time.Now().UTC().Add(3 * time.Hour).Truncate(time.Millisecond),
		Vehicle:       domain.VehicleModelY,
		SeatsTotal:    plazas,
		MaxDetourKm:   2,
		Status:        domain.TripOpen,
		CreatedAt:     time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := pg.CreateTrip(tr); err != nil {
		t.Fatalf("CreateTrip(%s): %v", id, err)
	}
	return tr
}

// --- Usuarios ---

func TestPostgresUsuarioIdaYVuelta(t *testing.T) {
	pg := newPostgres(t)
	creado := nuevoUsuario(t, pg, "usr_1", "ana@example.com")

	leido, err := pg.GetUser("usr_1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if leido.Name != creado.Name || leido.Email != creado.Email {
		t.Errorf("leído = %+v, esperaba %+v", leido, creado)
	}
	if leido.PasswordHash != creado.PasswordHash {
		t.Error("el hash de contraseña no sobrevivió al viaje de ida y vuelta")
	}
	if !leido.CreatedAt.Equal(creado.CreatedAt) {
		t.Errorf("created_at = %v, esperaba %v", leido.CreatedAt, creado.CreatedAt)
	}
}

func TestPostgresEmailUnicoSinDistinguirMayusculas(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")

	err := pg.CreateUser(&domain.User{
		ID: "usr_2", Name: "Otra", Email: "ANA@Example.com",
		PasswordHash: "x", CreatedAt: time.Now().UTC(),
	})
	if !errors.Is(err, store.ErrEmailEnUso) {
		t.Fatalf("error = %v, esperaba ErrEmailEnUso", err)
	}
}

func TestPostgresBuscarPorEmailIgnoraMayusculas(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")

	u, err := pg.GetUserByEmail("  ANA@EXAMPLE.COM  ")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if u.ID != "usr_1" {
		t.Fatalf("usuario = %q, esperaba usr_1", u.ID)
	}
}

func TestPostgresUsuarioInexistente(t *testing.T) {
	pg := newPostgres(t)
	if _, err := pg.GetUser("usr_fantasma"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("error = %v, esperaba ErrNotFound", err)
	}
}

// --- Trayectos y geometría ---

func TestPostgresRutaSobreviveAPostGIS(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	ruta := geo.Route{downtown, riverside, airport}
	nuevoTrayecto(t, pg, "trip_1", "usr_1", ruta, 3)

	leido, err := pg.GetTrip("trip_1")
	if err != nil {
		t.Fatalf("GetTrip: %v", err)
	}
	if len(leido.Route) != 3 {
		t.Fatalf("puntos de la ruta = %d, esperaba 3", len(leido.Route))
	}
	// PostGIS guarda en orden lng/lat: si se invirtiera, la ruta acabaría en
	// mitad del océano Índico.
	for i, want := range ruta {
		got := leido.Route[i]
		if abs(got.Lat-want.Lat) > 1e-9 || abs(got.Lng-want.Lng) > 1e-9 {
			t.Errorf("punto %d = %+v, esperaba %+v", i, got, want)
		}
	}
	if leido.Origin.Point != ruta[0] || leido.Destination.Point != ruta[2] {
		t.Error("origen y destino no coinciden con los extremos de la ruta")
	}
	if leido.RouteSource != "osrm" || leido.DurationMin != 17.4 {
		t.Errorf("fuente = %q, duración = %v", leido.RouteSource, leido.DurationMin)
	}
}

func TestPostgresListOpenTripsSoloDevuelveAbiertos(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevoTrayecto(t, pg, "trip_abierto", "usr_1", geo.Route{downtown, airport}, 3)

	cerrado := nuevoTrayecto(t, pg, "trip_cerrado", "usr_1", geo.Route{downtown, airport}, 3)
	cerrado.Status = domain.TripCancelled
	if err := pg.UpdateTrip(cerrado); err != nil {
		t.Fatalf("UpdateTrip: %v", err)
	}

	trips, err := pg.ListOpenTrips()
	if err != nil {
		t.Fatalf("ListOpenTrips: %v", err)
	}
	if len(trips) != 1 || trips[0].ID != "trip_abierto" {
		t.Fatalf("trayectos = %d, esperaba solo el abierto", len(trips))
	}
}

// --- Reserva atómica de plazas ---

func TestPostgresReserveSeatsRespetaElAforo(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevoTrayecto(t, pg, "trip_1", "usr_1", geo.Route{downtown, airport}, 2)

	if err := pg.ReserveSeats("trip_1", 2); err != nil {
		t.Fatalf("primera reserva: %v", err)
	}
	// El trayecto queda completo y no admite ni una plaza más.
	if err := pg.ReserveSeats("trip_1", 1); !errors.Is(err, store.ErrSinPlazas) {
		t.Fatalf("error = %v, esperaba ErrSinPlazas", err)
	}

	tr, _ := pg.GetTrip("trip_1")
	if tr.Status != domain.TripFull {
		t.Errorf("estado = %q, esperaba full", tr.Status)
	}
}

func TestPostgresReserveSeatsEsAtomicoBajoConcurrencia(t *testing.T) {
	// La prueba que justifica la sentencia única: veinte peticiones a la vez
	// sobre tres plazas. Con lectura y escritura separadas, varias verían las
	// mismas plazas libres y el trayecto acabaría sobrevendido.
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevoTrayecto(t, pg, "trip_1", "usr_1", geo.Route{downtown, airport}, 3)

	const intentos = 20
	resultados := make(chan error, intentos)
	inicio := make(chan struct{})
	for i := 0; i < intentos; i++ {
		go func() {
			<-inicio // que salgan todas a la vez
			resultados <- pg.ReserveSeats("trip_1", 1)
		}()
	}
	close(inicio)

	var conseguidas int
	for i := 0; i < intentos; i++ {
		if err := <-resultados; err == nil {
			conseguidas++
		} else if !errors.Is(err, store.ErrSinPlazas) {
			t.Fatalf("error inesperado: %v", err)
		}
	}

	if conseguidas != 3 {
		t.Fatalf("reservas concedidas = %d, esperaba exactamente 3", conseguidas)
	}
	tr, _ := pg.GetTrip("trip_1")
	if tr.SeatsTaken != 3 {
		t.Fatalf("plazas ocupadas = %d, esperaba 3", tr.SeatsTaken)
	}
}

func TestPostgresReleaseSeatsReabreElTrayecto(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevoTrayecto(t, pg, "trip_1", "usr_1", geo.Route{downtown, airport}, 1)

	if err := pg.ReserveSeats("trip_1", 1); err != nil {
		t.Fatalf("ReserveSeats: %v", err)
	}
	if err := pg.ReleaseSeats("trip_1", 1); err != nil {
		t.Fatalf("ReleaseSeats: %v", err)
	}

	tr, _ := pg.GetTrip("trip_1")
	if tr.Status != domain.TripOpen || tr.SeatsTaken != 0 {
		t.Fatalf("estado = %q, ocupadas = %d; esperaba open/0", tr.Status, tr.SeatsTaken)
	}
}

// --- Reservas ---

func TestPostgresReservaIdaYVuelta(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevoUsuario(t, pg, "usr_2", "bruno@example.com")
	nuevoTrayecto(t, pg, "trip_1", "usr_1", geo.Route{downtown, airport}, 3)

	b := &domain.Booking{
		ID: "bkg_1", TripID: "trip_1", PassengerID: "usr_2",
		Pickup:        domain.Place{Name: "Riverside", Point: riverside},
		Dropoff:       domain.Place{Name: "AUS", Point: airport},
		PickupAlongKm: 4.1, DropoffAlongKm: 10.8,
		Seats: 1, PriceCents: 337, Status: domain.BookingPending,
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := pg.CreateBooking(b); err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}

	leida, err := pg.GetBooking("bkg_1")
	if err != nil {
		t.Fatalf("GetBooking: %v", err)
	}
	if leida.PriceCents != 337 || leida.Seats != 1 {
		t.Errorf("leída = %+v", leida)
	}
	if abs(leida.Pickup.Point.Lat-riverside.Lat) > 1e-9 {
		t.Errorf("punto de recogida = %+v, esperaba %+v", leida.Pickup.Point, riverside)
	}
	if leida.Pickup.Name != "Riverside" {
		t.Errorf("nombre de recogida = %q", leida.Pickup.Name)
	}
}

func TestPostgresNoSePuedeReservarDosVecesElMismoTrayecto(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevoUsuario(t, pg, "usr_2", "bruno@example.com")
	nuevoTrayecto(t, pg, "trip_1", "usr_1", geo.Route{downtown, airport}, 3)

	reserva := func(id string) error {
		return pg.CreateBooking(&domain.Booking{
			ID: id, TripID: "trip_1", PassengerID: "usr_2",
			Pickup:        domain.Place{Name: "Riverside", Point: riverside},
			Dropoff:       domain.Place{Name: "AUS", Point: airport},
			PickupAlongKm: 4.1, DropoffAlongKm: 10.8,
			Seats: 1, PriceCents: 337, Status: domain.BookingPending,
			CreatedAt: time.Now().UTC(),
		})
	}
	if err := reserva("bkg_1"); err != nil {
		t.Fatalf("primera reserva: %v", err)
	}
	if err := reserva("bkg_2"); !errors.Is(err, store.ErrReservaDuplicada) {
		t.Fatalf("error = %v, esperaba ErrReservaDuplicada", err)
	}
}

func TestPostgresBaseDeDatosImpideBajarseAntesDeSubir(t *testing.T) {
	// La restricción vive en el esquema, no solo en la aplicación.
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevoUsuario(t, pg, "usr_2", "bruno@example.com")
	nuevoTrayecto(t, pg, "trip_1", "usr_1", geo.Route{downtown, airport}, 3)

	err := pg.CreateBooking(&domain.Booking{
		ID: "bkg_1", TripID: "trip_1", PassengerID: "usr_2",
		Pickup:        domain.Place{Name: "AUS", Point: airport},
		Dropoff:       domain.Place{Name: "Riverside", Point: riverside},
		PickupAlongKm: 10.8, DropoffAlongKm: 4.1, // al revés
		Seats: 1, PriceCents: 100, Status: domain.BookingPending,
		CreatedAt: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("la base de datos aceptó una reserva con el tramo invertido")
	}
}

// --- Búsqueda espacial ---

func TestPostgresCandidateTripsUsaLaProximidadDeLaRuta(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	// Uno pasa por el corredor centro→aeropuerto; el otro va al norte.
	nuevoTrayecto(t, pg, "trip_corredor", "usr_1", geo.Route{downtown, airport}, 3)
	nuevoTrayecto(t, pg, "trip_norte", "usr_1", geo.Route{downtown, roundRock}, 3)

	got, err := pg.CandidateTrips(context.Background(), store.GeoQuery{
		Pickup: riverside, Dropoff: airport, Seats: 1, MaxDistanceKm: 2,
	})
	if err != nil {
		t.Fatalf("CandidateTrips: %v", err)
	}
	if len(got) != 1 || got[0].ID != "trip_corredor" {
		t.Fatalf("candidatos = %v, esperaba solo trip_corredor", ids(got))
	}
}

func TestPostgresCandidateTripsRespetaPlazasYHorario(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	tr := nuevoTrayecto(t, pg, "trip_1", "usr_1", geo.Route{downtown, airport}, 1)

	// Piden dos plazas y solo hay una.
	got, err := pg.CandidateTrips(context.Background(), store.GeoQuery{
		Pickup: riverside, Dropoff: airport, Seats: 2, MaxDistanceKm: 2,
	})
	if err != nil {
		t.Fatalf("CandidateTrips: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("candidatos = %v, esperaba ninguno por falta de plazas", ids(got))
	}

	// Fuera de la horquilla horaria.
	got, err = pg.CandidateTrips(context.Background(), store.GeoQuery{
		Pickup: riverside, Dropoff: airport, Seats: 1, MaxDistanceKm: 2,
		EarliestDeparture: tr.DepartureTime.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CandidateTrips: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("candidatos = %v, esperaba ninguno fuera de horario", ids(got))
	}
}

func TestPostgresCandidateTripsIgnoraElSentidoDeLaMarcha(t *testing.T) {
	// El filtro espacial es grueso a propósito: solo mira proximidad. Que la
	// bajada vaya después de la recogida lo decide el paquete matching.
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevoTrayecto(t, pg, "trip_1", "usr_1", geo.Route{downtown, airport}, 3)

	got, err := pg.CandidateTrips(context.Background(), store.GeoQuery{
		Pickup: airport, Dropoff: riverside, Seats: 1, MaxDistanceKm: 2,
	})
	if err != nil {
		t.Fatalf("CandidateTrips: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidatos = %v, esperaba 1: el filtro grueso no ordena", ids(got))
	}
}

func ids(trips []*domain.Trip) []string {
	out := make([]string, 0, len(trips))
	for _, t := range trips {
		out = append(out, t.ID)
	}
	return out
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
