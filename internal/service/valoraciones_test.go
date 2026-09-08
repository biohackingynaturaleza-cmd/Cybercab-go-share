package service

import (
	"errors"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// reservaDel devuelve la reserva confirmada de un escenario ya cerrado.
func reservaDel(t *testing.T, e escenario) *domain.Booking {
	t.Helper()
	bs, err := e.svc.store.BookingsByTrip(e.trip.ID)
	if err != nil || len(bs) == 0 {
		t.Fatalf("no hay reservas en el trayecto: %v", err)
	}
	return bs[0]
}

func TestUnaValoracionSolaNoCuentaTodavia(t *testing.T) {
	// El corazón del diseño: si mi nota se publicara al escribirla, la otra
	// parte podría leerla y responder con una represalia en vez de con una
	// opinión.
	e, _ := viajeCompletado(t, 0)
	b := reservaDel(t, e)

	if _, err := e.svc.Valorar(ValorarInput{
		BookingID: b.ID, AutorID: e.host.ID, Estrellas: 2, Comentario: "Llegó tarde",
	}); err != nil {
		t.Fatalf("Valorar: %v", err)
	}

	vs, err := e.svc.ValoracionesSobre(e.pasajero.ID)
	if err != nil {
		t.Fatalf("ValoracionesSobre: %v", err)
	}
	if len(vs) != 0 {
		t.Fatalf("valoraciones visibles = %d, esperaba ninguna hasta que valoren los dos", len(vs))
	}

	// Y tampoco mueve su reputación.
	p, err := e.svc.PerfilDe(e.pasajero.ID)
	if err != nil {
		t.Fatalf("PerfilDe: %v", err)
	}
	if p.Stats.RatingCount != 0 {
		t.Fatalf("valoraciones contadas = %d, esperaba 0", p.Stats.RatingCount)
	}
}

func TestCuandoValoranLosDosSePublicanLasDos(t *testing.T) {
	e, _ := viajeCompletado(t, 0)
	b := reservaDel(t, e)

	if _, err := e.svc.Valorar(ValorarInput{
		BookingID: b.ID, AutorID: e.host.ID, Estrellas: 2, Comentario: "Llegó tarde",
	}); err != nil {
		t.Fatalf("Valorar(host): %v", err)
	}
	if _, err := e.svc.Valorar(ValorarInput{
		BookingID: b.ID, AutorID: e.pasajero.ID, Estrellas: 5, Comentario: "Todo bien",
	}); err != nil {
		t.Fatalf("Valorar(pasajero): %v", err)
	}

	delPasajero, err := e.svc.ValoracionesSobre(e.pasajero.ID)
	if err != nil {
		t.Fatalf("ValoracionesSobre(pasajero): %v", err)
	}
	if len(delPasajero) != 1 || delPasajero[0].Estrellas != 2 {
		t.Fatalf("valoraciones del pasajero = %+v", delPasajero)
	}
	if delPasajero[0].Autor != e.host.Name {
		t.Fatalf("autor = %q, esperaba %q", delPasajero[0].Autor, e.host.Name)
	}

	delHost, err := e.svc.ValoracionesSobre(e.host.ID)
	if err != nil {
		t.Fatalf("ValoracionesSobre(host): %v", err)
	}
	if len(delHost) != 1 || delHost[0].Estrellas != 5 {
		t.Fatalf("valoraciones del host = %+v", delHost)
	}
}

func TestUnaValoracionSinRespuestaSePublicaAlVencerElPlazo(t *testing.T) {
	// Si esperar callado bastara para esconder una mala nota para siempre,
	// bastaría con no valorar nunca. El plazo cierra ese agujero.
	e, _ := viajeCompletado(t, 0)
	b := reservaDel(t, e)
	if _, err := e.svc.Valorar(ValorarInput{
		BookingID: b.ID, AutorID: e.host.ID, Estrellas: 2,
	}); err != nil {
		t.Fatalf("Valorar: %v", err)
	}

	base := e.svc.cfg.Now()
	e.svc.cfg.Now = func() time.Time { return base.Add(domain.PlazoValoracion + time.Hour) }

	vs, err := e.svc.ValoracionesSobre(e.pasajero.ID)
	if err != nil {
		t.Fatalf("ValoracionesSobre: %v", err)
	}
	if len(vs) != 1 {
		t.Fatalf("valoraciones = %d, esperaba que el plazo publicara la única que hay", len(vs))
	}
}

func TestNoSePuedeValorarDosVecesElMismoViaje(t *testing.T) {
	// Poder reescribir la nota la convierte en moneda de cambio: "súbeme la
	// mía y te subo la tuya".
	e, _ := viajeCompletado(t, 0)
	b := reservaDel(t, e)
	entrada := ValorarInput{BookingID: b.ID, AutorID: e.host.ID, Estrellas: 5}

	if _, err := e.svc.Valorar(entrada); err != nil {
		t.Fatalf("primera valoración: %v", err)
	}
	if _, err := e.svc.Valorar(entrada); err == nil {
		t.Fatal("la segunda valoración del mismo viaje pasó")
	}
}

func TestNadieAjenoAlViajePuedeValorar(t *testing.T) {
	e, _ := viajeCompletado(t, 0)
	b := reservaDel(t, e)
	otro, err := e.svc.Register(RegisterInput{
		Name: "Eva", Email: "eva@example.com", Password: testPassword,
		Idioma: "es", AceptaTerminos: true,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err = e.svc.Valorar(ValorarInput{BookingID: b.ID, AutorID: otro.User.ID, Estrellas: 1})
	if !errors.Is(err, ErrNoAutorizado) {
		t.Fatalf("err = %v, esperaba ErrNoAutorizado", err)
	}
}

func TestNoSePuedeValorarUnViajeQueNoSeHaHecho(t *testing.T) {
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelVerificado)
	b, err := e.reservar()
	if err != nil {
		t.Fatalf("RequestBooking: %v", err)
	}
	if _, err := e.svc.DecideBooking(DecisionInput{
		BookingID: b.ID, HostID: e.host.ID, Accept: true, AceptaResponsabilidad: true,
	}); err != nil {
		t.Fatalf("DecideBooking: %v", err)
	}

	// El trayecto está confirmado pero todavía no cerrado.
	_, err = e.svc.Valorar(ValorarInput{BookingID: b.ID, AutorID: e.host.ID, Estrellas: 5})
	if !errors.Is(err, ErrViajeNoValorable) {
		t.Fatalf("err = %v, esperaba ErrViajeNoValorable", err)
	}
}

func TestLaNotaTieneQueEstarEntreUnaYCincoEstrellas(t *testing.T) {
	e, _ := viajeCompletado(t, 0)
	b := reservaDel(t, e)
	for _, estrellas := range []int{0, -1, 6, 100} {
		_, err := e.svc.Valorar(ValorarInput{BookingID: b.ID, AutorID: e.host.ID, Estrellas: estrellas})
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("%d estrellas = %v, esperaba un fallo de validación", estrellas, err)
		}
	}
}

func TestLaMediaSaleDeLasValoracionesPublicadas(t *testing.T) {
	// La media se calcula, no se guarda: una valoración pasa sola de invisible
	// a visible al vencer el plazo, y ningún contador guardado se entera.
	e, _ := viajeCompletado(t, 0)
	b := reservaDel(t, e)
	if _, err := e.svc.Valorar(ValorarInput{BookingID: b.ID, AutorID: e.host.ID, Estrellas: 4}); err != nil {
		t.Fatalf("Valorar(host): %v", err)
	}
	if _, err := e.svc.Valorar(ValorarInput{BookingID: b.ID, AutorID: e.pasajero.ID, Estrellas: 3}); err != nil {
		t.Fatalf("Valorar(pasajero): %v", err)
	}

	p, err := e.svc.PerfilDe(e.pasajero.ID)
	if err != nil {
		t.Fatalf("PerfilDe: %v", err)
	}
	if p.Stats.RatingCount != 1 || p.Stats.Rating != 4 {
		t.Fatalf("estadísticas = %+v, esperaba una valoración de 4", p.Stats)
	}
}

func TestCerrarElViajeCuentaEnElHistorialDeTodos(t *testing.T) {
	// Sin esto, RideCount no subía nunca y el nivel veterano era inalcanzable
	// por mucho que alguien viajara.
	e, _ := viajeCompletado(t, 0)

	for _, quien := range []*domain.User{e.host, e.pasajero} {
		p, err := e.svc.PerfilDe(quien.ID)
		if err != nil {
			t.Fatalf("PerfilDe(%s): %v", quien.Name, err)
		}
		if p.Stats.CompletedTrips != 1 {
			t.Errorf("%s tiene %d viajes completados, esperaba 1", quien.Name, p.Stats.CompletedTrips)
		}
	}
}

func TestElViajeCerradoApareceComoPendienteDeValorar(t *testing.T) {
	e, _ := viajeCompletado(t, 0)

	pendientes, err := e.svc.PendientesDeValorar(e.host.ID)
	if err != nil {
		t.Fatalf("PendientesDeValorar: %v", err)
	}
	if len(pendientes) != 1 {
		t.Fatalf("pendientes = %d, esperaba 1", len(pendientes))
	}
	if pendientes[0].SobreID != e.pasajero.ID || pendientes[0].Nombre != e.pasajero.Name {
		t.Fatalf("pendiente = %+v, esperaba al pasajero", pendientes[0])
	}

	b := reservaDel(t, e)
	if _, err := e.svc.Valorar(ValorarInput{BookingID: b.ID, AutorID: e.host.ID, Estrellas: 5}); err != nil {
		t.Fatalf("Valorar: %v", err)
	}
	despues, err := e.svc.PendientesDeValorar(e.host.ID)
	if err != nil {
		t.Fatalf("PendientesDeValorar: %v", err)
	}
	if len(despues) != 0 {
		t.Fatalf("pendientes tras valorar = %d, esperaba 0", len(despues))
	}
}

func TestElPasajeroTambienTieneSuViajePendienteDeValorar(t *testing.T) {
	e, _ := viajeCompletado(t, 0)
	pendientes, err := e.svc.PendientesDeValorar(e.pasajero.ID)
	if err != nil {
		t.Fatalf("PendientesDeValorar: %v", err)
	}
	if len(pendientes) != 1 || pendientes[0].SobreID != e.host.ID {
		t.Fatalf("pendientes = %+v, esperaba al host", pendientes)
	}
}
