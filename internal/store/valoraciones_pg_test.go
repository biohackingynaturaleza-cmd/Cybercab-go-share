package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
)

// parejaConReserva deja dos personas, un trayecto y una reserva confirmada:
// el mínimo para que exista una valoración o una denuncia.
func parejaConReserva(t *testing.T, pg *store.Postgres) (host, pasajero *domain.User, tripID, bookingID string) {
	t.Helper()
	host = nuevoUsuario(t, pg, "usr_host", "ana@example.com")
	pasajero = nuevoUsuario(t, pg, "usr_pas", "bruno@example.com")
	ruta := geo.Route{{Lat: 30.2672, Lng: -97.7431}, {Lat: 30.1975, Lng: -97.6664}}
	tr := nuevoTrayecto(t, pg, "trp_1", host.ID, ruta, 4)

	b := &domain.Booking{
		ID: "bkg_1", TripID: tr.ID, PassengerID: pasajero.ID,
		Pickup:        domain.Place{Name: "Centro", Point: ruta[0]},
		Dropoff:       domain.Place{Name: "AUS", Point: ruta[1]},
		PickupAlongKm: 0, DropoffAlongKm: 12.4,
		Seats: 1, PriceCents: 560, Status: domain.BookingConfirmed,
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := pg.CreateBooking(b); err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}
	return host, pasajero, tr.ID, b.ID
}

func TestPostgresValoracionIdaYVuelta(t *testing.T) {
	pg := newPostgres(t)
	host, pasajero, tripID, bookingID := parejaConReserva(t, pg)

	v := &domain.Valoracion{
		ID: "val_1", BookingID: bookingID, TripID: tripID,
		AutorID: host.ID, SobreID: pasajero.ID, Estrellas: 4,
		Comentario: "Puntual y agradable",
		CreatedAt:  time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := pg.CrearValoracion(v); err != nil {
		t.Fatalf("CrearValoracion: %v", err)
	}

	recibidas, err := pg.ValoracionesRecibidas(pasajero.ID)
	if err != nil {
		t.Fatalf("ValoracionesRecibidas: %v", err)
	}
	if len(recibidas) != 1 || recibidas[0].Estrellas != 4 || recibidas[0].Comentario != v.Comentario {
		t.Fatalf("recibidas = %+v", recibidas)
	}
	emitidas, err := pg.ValoracionesEmitidas(host.ID)
	if err != nil {
		t.Fatalf("ValoracionesEmitidas: %v", err)
	}
	if len(emitidas) != 1 {
		t.Fatalf("emitidas = %d, esperaba 1", len(emitidas))
	}
	// Y a quien la escribió no le consta como recibida.
	suyas, err := pg.ValoracionesRecibidas(host.ID)
	if err != nil {
		t.Fatalf("ValoracionesRecibidas(host): %v", err)
	}
	if len(suyas) != 0 {
		t.Fatalf("el autor tiene %d valoraciones recibidas, esperaba 0", len(suyas))
	}
}

func TestPostgresUnaValoracionPorParteYReserva(t *testing.T) {
	pg := newPostgres(t)
	host, pasajero, tripID, bookingID := parejaConReserva(t, pg)
	base := domain.Valoracion{
		BookingID: bookingID, TripID: tripID, AutorID: host.ID, SobreID: pasajero.ID,
		Estrellas: 5, CreatedAt: time.Now().UTC(),
	}
	primera := base
	primera.ID = "val_1"
	if err := pg.CrearValoracion(&primera); err != nil {
		t.Fatalf("primera: %v", err)
	}

	segunda := base
	segunda.ID = "val_2"
	segunda.Estrellas = 1
	if err := pg.CrearValoracion(&segunda); !errors.Is(err, store.ErrYaValorado) {
		t.Fatalf("segunda = %v, esperaba ErrYaValorado", err)
	}

	// La otra parte sí puede valorar la misma reserva: es la recíproca.
	reciproca := domain.Valoracion{
		ID: "val_3", BookingID: bookingID, TripID: tripID,
		AutorID: pasajero.ID, SobreID: host.ID, Estrellas: 5,
		CreatedAt: time.Now().UTC(),
	}
	if err := pg.CrearValoracion(&reciproca); err != nil {
		t.Fatalf("recíproca: %v", err)
	}
	ambas, err := pg.ValoracionesDeBooking(bookingID)
	if err != nil {
		t.Fatalf("ValoracionesDeBooking: %v", err)
	}
	if len(ambas) != 2 {
		t.Fatalf("valoraciones de la reserva = %d, esperaba 2", len(ambas))
	}
}

func TestPostgresIncrementarViajes(t *testing.T) {
	pg := newPostgres(t)
	host, pasajero, _, _ := parejaConReserva(t, pg)

	if err := pg.IncrementarViajes([]string{host.ID, pasajero.ID}); err != nil {
		t.Fatalf("IncrementarViajes: %v", err)
	}
	if err := pg.IncrementarViajes([]string{host.ID}); err != nil {
		t.Fatalf("IncrementarViajes: %v", err)
	}

	for id, esperado := range map[string]int{host.ID: 2, pasajero.ID: 1} {
		u, err := pg.GetUser(id)
		if err != nil {
			t.Fatalf("GetUser(%s): %v", id, err)
		}
		if u.RideCount != esperado {
			t.Errorf("%s tiene %d viajes, esperaba %d", id, u.RideCount, esperado)
		}
	}
}

func TestPostgresDenunciaYSuspension(t *testing.T) {
	pg := newPostgres(t)
	host, pasajero, tripID, _ := parejaConReserva(t, pg)

	d := &domain.Denuncia{
		ID: "den_1", TripID: tripID,
		DenuncianteID: host.ID, DenunciadoID: pasajero.ID,
		Motivo: domain.DenunciaSeguridad, Descripcion: "Amenazó con agredirme durante el trayecto.",
		Estado: domain.DenunciaAbierta, CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := pg.CrearDenuncia(d); err != nil {
		t.Fatalf("CrearDenuncia: %v", err)
	}

	leida, err := pg.GetDenuncia("den_1")
	if err != nil {
		t.Fatalf("GetDenuncia: %v", err)
	}
	if leida.Motivo != domain.DenunciaSeguridad || leida.Descripcion != d.Descripcion {
		t.Fatalf("leída = %+v", leida)
	}
	if !leida.ResueltaAt.IsZero() {
		t.Fatalf("una denuncia abierta trae fecha de resolución: %v", leida.ResueltaAt)
	}

	abiertas, err := pg.DenunciasAbiertas()
	if err != nil {
		t.Fatalf("DenunciasAbiertas: %v", err)
	}
	if len(abiertas) != 1 {
		t.Fatalf("abiertas = %d, esperaba 1", len(abiertas))
	}

	// Resolverla la saca de la cola y deja fecha.
	leida.Estado = domain.DenunciaConfirmada
	leida.ResueltaAt = time.Now().UTC().Truncate(time.Millisecond)
	leida.Resolucion = "Confirmada tras hablar con las dos partes."
	if err := pg.UpdateDenuncia(leida); err != nil {
		t.Fatalf("UpdateDenuncia: %v", err)
	}
	abiertas, err = pg.DenunciasAbiertas()
	if err != nil {
		t.Fatalf("DenunciasAbiertas: %v", err)
	}
	if len(abiertas) != 0 {
		t.Fatalf("abiertas tras resolver = %d, esperaba 0", len(abiertas))
	}
	resuelta, err := pg.GetDenuncia("den_1")
	if err != nil {
		t.Fatalf("GetDenuncia: %v", err)
	}
	if resuelta.ResueltaAt.IsZero() || resuelta.Resolucion == "" {
		t.Fatalf("la resolución no sobrevivió: %+v", resuelta)
	}

	// Y la suspensión.
	hasta := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Millisecond)
	if err := pg.SuspenderUsuario(pasajero.ID, &hasta); err != nil {
		t.Fatalf("SuspenderUsuario: %v", err)
	}
	u, err := pg.GetUser(pasajero.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !u.Suspendido(time.Now().UTC()) {
		t.Fatal("la suspensión no sobrevivió al viaje de ida y vuelta")
	}
	if err := pg.SuspenderUsuario(pasajero.ID, nil); err != nil {
		t.Fatalf("levantar la suspensión: %v", err)
	}
	u, err = pg.GetUser(pasajero.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.SuspendidoHasta != nil {
		t.Fatalf("la suspensión sigue puesta: %v", u.SuspendidoHasta)
	}
}

func TestPostgresLaColaPoneLoUrgentePrimero(t *testing.T) {
	pg := newPostgres(t)
	host, pasajero, tripID, _ := parejaConReserva(t, pg)
	ahora := time.Now().UTC()

	// La de conducta se pone antes en el tiempo: si la cola ordenara solo por
	// fecha, saldría primero.
	conducta := &domain.Denuncia{
		ID: "den_conducta", TripID: tripID, DenuncianteID: host.ID, DenunciadoID: pasajero.ID,
		Motivo: domain.DenunciaConducta, Descripcion: "Fue bastante desagradable todo el camino.",
		Estado: domain.DenunciaAbierta, CreatedAt: ahora.Add(-time.Hour),
	}
	seguridad := &domain.Denuncia{
		ID: "den_seguridad", TripID: tripID, DenuncianteID: pasajero.ID, DenunciadoID: host.ID,
		Motivo: domain.DenunciaSeguridad, Descripcion: "Me amenazó al bajarme del vehículo.",
		Estado: domain.DenunciaAbierta, CreatedAt: ahora,
	}
	for _, d := range []*domain.Denuncia{conducta, seguridad} {
		if err := pg.CrearDenuncia(d); err != nil {
			t.Fatalf("CrearDenuncia(%s): %v", d.ID, err)
		}
	}

	cola, err := pg.DenunciasAbiertas()
	if err != nil {
		t.Fatalf("DenunciasAbiertas: %v", err)
	}
	if len(cola) != 2 || cola[0].ID != "den_seguridad" {
		t.Fatalf("cola = %v, esperaba la de seguridad primero", nombresDe(cola))
	}
}

func nombresDe(ds []*domain.Denuncia) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.ID)
	}
	return out
}
