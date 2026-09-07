package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/auth"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/matching"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

var (
	downtown  = domain.Place{Name: "Centro", Point: geo.Point{Lat: 30.2685, Lng: -97.7425}}
	riverside = domain.Place{Name: "Riverside", Point: geo.Point{Lat: 30.2380, Lng: -97.7180}}
	airport   = domain.Place{Name: "AUS", Point: geo.Point{Lat: 30.1975, Lng: -97.6664}}
)

type fixture struct {
	svc       *Service
	identidad *trust.Manual
	host      *domain.User
	rider     *domain.User
	trip      *domain.Trip
}

const testPassword = "contraseña-de-prueba"

// newTestService construye un servicio listo para pruebas: emisor de tokens
// propio, rutas en línea recta y proveedor de identidad manual, sin depender
// de la red.
func newTestService(t *testing.T) (*Service, *trust.Manual) {
	t.Helper()
	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	tokens, err := auth.NewTokenIssuer(secret, time.Hour)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	identidad := trust.NewManual("http://test")
	return New(store.NewMemory(), Config{Tokens: tokens, Identidad: identidad}), identidad
}

// acreditar hace pasar a alguien por el flujo real de verificación hasta
// alcanzar el nivel pedido. No inyecta comprobaciones a mano a propósito: así
// las pruebas recorren el mismo camino que la aplicación.
func acreditar(t *testing.T, svc *Service, m *trust.Manual, userID string, nivel trust.Level) {
	t.Helper()
	var tipos []trust.CheckKind
	switch {
	case nivel >= trust.LevelVerificado:
		tipos = []trust.CheckKind{trust.CheckEmail, trust.CheckPhone, trust.CheckGovernmentID, trust.CheckSelfie}
	case nivel >= trust.LevelBasico:
		tipos = []trust.CheckKind{trust.CheckEmail, trust.CheckPhone}
	default:
		return
	}

	ctx := context.Background()
	for _, kind := range tipos {
		sess, _, err := svc.IniciarVerificacion(ctx, userID, kind)
		if err != nil {
			t.Fatalf("IniciarVerificacion(%s, %s): %v", userID, kind, err)
		}
		if err := m.Resolve(sess.Ref, trust.Outcome{Status: trust.StatusVerified}); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if _, err := svc.RefrescarVerificacion(ctx, sess.Ref); err != nil {
			t.Fatalf("RefrescarVerificacion: %v", err)
		}
	}

	got, err := svc.NivelDe(userID)
	if err != nil {
		t.Fatalf("NivelDe: %v", err)
	}
	if got < nivel {
		t.Fatalf("tras acreditar, nivel = %v, esperaba al menos %v", got, nivel)
	}
}

func newFixture(t *testing.T, vehicle domain.VehicleType) fixture {
	t.Helper()
	svc, identidad := newTestService(t)

	hostSess, err := svc.Register("Ana", "ana@example.com", testPassword)
	if err != nil {
		t.Fatalf("Register(host): %v", err)
	}
	host := hostSess.User
	riderSess, err := svc.Register("Bruno", "bruno@example.com", testPassword)
	if err != nil {
		t.Fatalf("Register(rider): %v", err)
	}
	rider := riderSess.User

	// Ambas partes con identidad acreditada: es lo que exige compartir coche,
	// y estas pruebas van de la mecánica del viaje, no de la verificación.
	acreditar(t, svc, identidad, host.ID, trust.LevelVerificado)
	acreditar(t, svc, identidad, rider.ID, trust.LevelVerificado)

	trip, err := svc.CreateTrip(context.Background(), NewTripInput{
		HostID:        host.ID,
		Origin:        downtown,
		Destination:   airport,
		DepartureTime: time.Now().UTC().Add(2 * time.Hour),
		Vehicle:       vehicle,
		MaxDetourKm:   2,
	})
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	return fixture{svc: svc, identidad: identidad, host: host, rider: rider, trip: trip}
}

func TestCreateTripCybercabOfreceUnaSolaPlaza(t *testing.T) {
	f := newFixture(t, domain.VehicleCybercab)
	// El Cybercab es biplaza: una plaza es de quien organiza, la otra se comparte.
	if f.trip.SeatsTotal != 1 {
		t.Fatalf("plazas ofertadas = %d, esperaba 1", f.trip.SeatsTotal)
	}
}

func TestCreateTripRechazaMasPlazasQueElAforo(t *testing.T) {
	f := newFixture(t, domain.VehicleCybercab)
	_, err := f.svc.CreateTrip(context.Background(), NewTripInput{
		HostID:        f.host.ID,
		Origin:        downtown,
		Destination:   airport,
		DepartureTime: time.Now().UTC().Add(2 * time.Hour),
		Vehicle:       domain.VehicleCybercab,
		SeatsOffered:  2, // no cabe: quien organiza también viaja
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error = %v, esperaba un error de validación", err)
	}
}

func TestCreateTripRechazaSalidasPasadas(t *testing.T) {
	f := newFixture(t, domain.VehicleModelY)
	_, err := f.svc.CreateTrip(context.Background(), NewTripInput{
		HostID:        f.host.ID,
		Origin:        downtown,
		Destination:   airport,
		DepartureTime: time.Now().UTC().Add(-time.Hour),
		Vehicle:       domain.VehicleModelY,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error = %v, esperaba un error de validación", err)
	}
}

func TestFlujoCompletoDeReserva(t *testing.T) {
	f := newFixture(t, domain.VehicleModelY)

	b, err := f.svc.RequestBooking(BookInput{
		TripID: f.trip.ID, PassengerID: f.rider.ID,
		Pickup: riverside, Dropoff: airport,
	})
	if err != nil {
		t.Fatalf("RequestBooking: %v", err)
	}
	if b.Status != domain.BookingPending {
		t.Fatalf("estado = %q, esperaba pending", b.Status)
	}
	if b.PriceCents <= 0 {
		t.Fatalf("precio = %d, esperaba un importe positivo", b.PriceCents)
	}

	// La plaza queda retenida desde la petición.
	trip, _ := f.svc.GetTrip(f.trip.ID)
	if trip.SeatsTaken != 1 {
		t.Fatalf("plazas ocupadas = %d, esperaba 1", trip.SeatsTaken)
	}

	confirmed, err := f.svc.DecideBooking(DecisionInput{BookingID: b.ID, HostID: f.host.ID, Accept: true, AceptaResponsabilidad: true})
	if err != nil {
		t.Fatalf("DecideBooking: %v", err)
	}
	if confirmed.Status != domain.BookingConfirmed {
		t.Fatalf("estado = %q, esperaba confirmed", confirmed.Status)
	}
}

func TestRechazarUnaReservaLiberaLaPlaza(t *testing.T) {
	f := newFixture(t, domain.VehicleCybercab)

	b, err := f.svc.RequestBooking(BookInput{
		TripID: f.trip.ID, PassengerID: f.rider.ID,
		Pickup: riverside, Dropoff: airport,
	})
	if err != nil {
		t.Fatalf("RequestBooking: %v", err)
	}

	// Con la única plaza retenida, el trayecto se marca completo.
	if trip, _ := f.svc.GetTrip(f.trip.ID); trip.Status != domain.TripFull {
		t.Fatalf("estado del trayecto = %q, esperaba full", trip.Status)
	}

	if _, err := f.svc.DecideBooking(DecisionInput{BookingID: b.ID, HostID: f.host.ID, Accept: false, AceptaResponsabilidad: true}); err != nil {
		t.Fatalf("DecideBooking(rechazo): %v", err)
	}

	trip, _ := f.svc.GetTrip(f.trip.ID)
	if trip.Status != domain.TripOpen || trip.SeatsAvailable() != 1 {
		t.Fatalf("tras el rechazo: estado=%q libres=%d, esperaba open/1", trip.Status, trip.SeatsAvailable())
	}
}

func TestCancelarUnaReservaLiberaLaPlaza(t *testing.T) {
	f := newFixture(t, domain.VehicleModelY)

	b, _ := f.svc.RequestBooking(BookInput{
		TripID: f.trip.ID, PassengerID: f.rider.ID,
		Pickup: riverside, Dropoff: airport,
	})
	if _, err := f.svc.CancelBooking(b.ID, f.rider.ID); err != nil {
		t.Fatalf("CancelBooking: %v", err)
	}
	if trip, _ := f.svc.GetTrip(f.trip.ID); trip.SeatsTaken != 0 {
		t.Fatalf("plazas ocupadas = %d, esperaba 0", trip.SeatsTaken)
	}
}

func TestNoSePuedeReservarSinPlazas(t *testing.T) {
	f := newFixture(t, domain.VehicleCybercab)
	terceroSess, _ := f.svc.Register("Clara", "clara@example.com", testPassword)
	tercero := terceroSess.User
	acreditar(t, f.svc, f.identidad, tercero.ID, trust.LevelVerificado)

	if _, err := f.svc.RequestBooking(BookInput{
		TripID: f.trip.ID, PassengerID: f.rider.ID,
		Pickup: riverside, Dropoff: airport,
	}); err != nil {
		t.Fatalf("primera reserva: %v", err)
	}

	_, err := f.svc.RequestBooking(BookInput{
		TripID: f.trip.ID, PassengerID: tercero.ID,
		Pickup: riverside, Dropoff: airport,
	})
	if !errors.Is(err, ErrNoSeats) {
		t.Fatalf("error = %v, esperaba ErrNoSeats", err)
	}
}

func TestQuienOrganizaNoPuedeReservarseASiMismo(t *testing.T) {
	f := newFixture(t, domain.VehicleModelY)
	_, err := f.svc.RequestBooking(BookInput{
		TripID: f.trip.ID, PassengerID: f.host.ID,
		Pickup: riverside, Dropoff: airport,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error = %v, esperaba un error de validación", err)
	}
}

func TestSoloQuienOrganizaDecideSobreLaReserva(t *testing.T) {
	f := newFixture(t, domain.VehicleModelY)
	b, _ := f.svc.RequestBooking(BookInput{
		TripID: f.trip.ID, PassengerID: f.rider.ID,
		Pickup: riverside, Dropoff: airport,
	})

	if _, err := f.svc.DecideBooking(DecisionInput{BookingID: b.ID, HostID: f.rider.ID, Accept: true, AceptaResponsabilidad: true}); !errors.Is(err, ErrNoAutorizado) {
		t.Fatalf("error = %v, esperaba ErrNoAutorizado", err)
	}
}

func TestReservaFueraDeRutaSeRechaza(t *testing.T) {
	f := newFixture(t, domain.VehicleModelY)
	lejos := domain.Place{Name: "Round Rock", Point: geo.Point{Lat: 30.5083, Lng: -97.6789}}

	_, err := f.svc.RequestBooking(BookInput{
		TripID: f.trip.ID, PassengerID: f.rider.ID,
		Pickup: lejos, Dropoff: airport,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error = %v, esperaba un error de validación", err)
	}
}

func TestCancelarElTrayectoAnulaSusReservas(t *testing.T) {
	f := newFixture(t, domain.VehicleModelY)
	b, _ := f.svc.RequestBooking(BookInput{
		TripID: f.trip.ID, PassengerID: f.rider.ID,
		Pickup: riverside, Dropoff: airport,
	})

	if _, err := f.svc.CancelTrip(f.trip.ID, f.host.ID); err != nil {
		t.Fatalf("CancelTrip: %v", err)
	}
	after, _ := f.svc.BookingsByTrip(f.trip.ID)
	if len(after) != 1 || after[0].Status != domain.BookingCancelled {
		t.Fatalf("reserva %s no quedó anulada: %+v", b.ID, after)
	}
	if trip, _ := f.svc.GetTrip(f.trip.ID); trip.Status != domain.TripCancelled {
		t.Fatalf("estado del trayecto = %q, esperaba cancelled", trip.Status)
	}
}

func TestElRepartoAhorraDineroAQuienOrganiza(t *testing.T) {
	f := newFixture(t, domain.VehicleModelY)

	antes, err := f.svc.FareBreakdownFor(f.trip.ID)
	if err != nil {
		t.Fatalf("FareBreakdownFor: %v", err)
	}
	if antes.Shares[0].AmountCents != antes.TotalCents {
		t.Fatalf("sin pasajeros quien organiza paga %d, esperaba el total %d",
			antes.Shares[0].AmountCents, antes.TotalCents)
	}

	if _, err := f.svc.RequestBooking(BookInput{
		TripID: f.trip.ID, PassengerID: f.rider.ID,
		Pickup: downtown, Dropoff: airport,
	}); err != nil {
		t.Fatalf("RequestBooking: %v", err)
	}

	despues, err := f.svc.FareBreakdownFor(f.trip.ID)
	if err != nil {
		t.Fatalf("FareBreakdownFor: %v", err)
	}

	var total int64
	for _, s := range despues.Shares {
		total += s.AmountCents
	}
	if total != despues.TotalCents {
		t.Fatalf("las partes suman %d, esperaba %d", total, despues.TotalCents)
	}
	if despues.Shares[0].AmountCents >= antes.Shares[0].AmountCents {
		t.Fatalf("quien organiza paga %d compartiendo y %d en solitario: debería ahorrar",
			despues.Shares[0].AmountCents, antes.Shares[0].AmountCents)
	}
}

func TestSearchEncuentraElTrayectoPublicado(t *testing.T) {
	f := newFixture(t, domain.VehicleModelY)

	matches, err := f.svc.Search(context.Background(), matching.Query{
		Pickup:  riverside.Point,
		Dropoff: airport.Point,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(matches) != 1 || matches[0].Trip.ID != f.trip.ID {
		t.Fatalf("resultados = %+v, esperaba el trayecto %s", matches, f.trip.ID)
	}
}

func TestSearchNoDevuelveTrayectosAnulados(t *testing.T) {
	f := newFixture(t, domain.VehicleModelY)
	if _, err := f.svc.CancelTrip(f.trip.ID, f.host.ID); err != nil {
		t.Fatalf("CancelTrip: %v", err)
	}

	matches, _ := f.svc.Search(context.Background(), matching.Query{Pickup: riverside.Point, Dropoff: airport.Point})
	if len(matches) != 0 {
		t.Fatalf("resultados = %d, esperaba 0", len(matches))
	}
}

func TestGetTripInexistenteDevuelveNotFound(t *testing.T) {
	f := newFixture(t, domain.VehicleModelY)
	if _, err := f.svc.GetTrip("trip_no_existe"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("error = %v, esperaba ErrNotFound", err)
	}
}
