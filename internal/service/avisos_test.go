package service

import (
	"context"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// escenarioConAvisos monta anfitrión, pasajero y trayecto, y devuelve el
// grabador para poder comprobar a quién se avisa.
type conAvisos struct {
	svc      *Service
	avisos   *notify.Grabador
	host     *domain.User
	pasajero *domain.User
	trip     *domain.Trip
}

func nuevoConAvisos(t *testing.T) conAvisos {
	t.Helper()
	svc, identidad, grabador := newTestServiceConAvisos(t)

	hostSess, err := svc.Register(RegisterInput{Name: "Ana", Email: "ana@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})
	if err != nil {
		t.Fatalf("Register(host): %v", err)
	}
	pasSess, err := svc.Register(RegisterInput{Name: "Bruno", Email: "bruno@example.com", Password: testPassword, Idioma: "en", AceptaTerminos: true})
	if err != nil {
		t.Fatalf("Register(pasajero): %v", err)
	}
	acreditar(t, svc, identidad, hostSess.User.ID, trust.LevelVerificado)
	acreditar(t, svc, identidad, pasSess.User.ID, trust.LevelVerificado)

	trip, err := svc.CreateTrip(context.Background(), NewTripInput{
		HostID:        hostSess.User.ID,
		Origin:        downtown,
		Destination:   airport,
		DepartureTime: time.Now().UTC().Add(3 * time.Hour),
		Vehicle:       domain.VehicleModelY,
		MaxDetourKm:   2,
	})
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}

	grabador.Limpiar() // los avisos de verificación no interesan aquí
	return conAvisos{svc: svc, avisos: grabador, host: hostSess.User,
		pasajero: pasSess.User, trip: trip}
}

func (e conAvisos) reservar(t *testing.T) *domain.Booking {
	t.Helper()
	b, err := e.svc.RequestBooking(BookInput{
		TripID: e.trip.ID, PassengerID: e.pasajero.ID,
		Pickup: riverside, Dropoff: airport,
	})
	if err != nil {
		t.Fatalf("RequestBooking: %v", err)
	}
	return b
}

func TestPedirPlazaAvisaAQuienOrganiza(t *testing.T) {
	// La promesa que la interfaz hace y que antes no cumplía nadie: quien
	// organiza solo se enteraba si recargaba la página por casualidad.
	e := nuevoConAvisos(t)
	e.reservar(t)

	a, ok := e.avisos.Ultimo(notify.SucesoPlazaPedida)
	if !ok {
		t.Fatal("nadie ha avisado a quien organiza")
	}
	if a.Para != e.host.Email {
		t.Errorf("aviso a %q, esperaba a %q", a.Para, e.host.Email)
	}
	if a.Idioma != "es" {
		t.Errorf("idioma = %q: Ana usa la app en español", a.Idioma)
	}
	// El correo trae lo necesario para decidir sin ir a buscarlo.
	for _, campo := range []string{"pasajero", "nivel", "km", "importe", "destino"} {
		if a.Datos[campo] == "" {
			t.Errorf("falta %q en el aviso: %v", campo, a.Datos)
		}
	}
}

func TestAceptarYRechazarAvisanAlPasajero(t *testing.T) {
	for _, aceptar := range []bool{true, false} {
		e := nuevoConAvisos(t)
		b := e.reservar(t)
		e.avisos.Limpiar()

		if _, err := e.svc.DecideBooking(DecisionInput{
			BookingID: b.ID, HostID: e.host.ID, Accept: aceptar,
			AceptaResponsabilidad: aceptar,
		}); err != nil {
			t.Fatalf("DecideBooking(%v): %v", aceptar, err)
		}

		suceso := notify.SucesoPlazaRechazada
		if aceptar {
			suceso = notify.SucesoPlazaAceptada
		}
		a, ok := e.avisos.Ultimo(suceso)
		if !ok {
			t.Fatalf("aceptar=%v: no se avisó al pasajero", aceptar)
		}
		if a.Para != e.pasajero.Email {
			t.Errorf("aviso a %q, esperaba al pasajero", a.Para)
		}
		if a.Idioma != "en" {
			t.Errorf("idioma = %q: Bruno usa la app en inglés", a.Idioma)
		}
		if aceptar && a.Datos["solo"] == "" {
			t.Error("el aviso de aceptación debería recordar lo que costaría ir solo")
		}
	}
}

func TestAnularElTrayectoAvisaAQuienSeQuedaSinPlaza(t *testing.T) {
	e := nuevoConAvisos(t)
	b := e.reservar(t)
	if _, err := e.svc.DecideBooking(DecisionInput{
		BookingID: b.ID, HostID: e.host.ID, Accept: true, AceptaResponsabilidad: true,
	}); err != nil {
		t.Fatalf("DecideBooking: %v", err)
	}
	e.avisos.Limpiar()

	if _, err := e.svc.CancelTrip(e.trip.ID, e.host.ID); err != nil {
		t.Fatalf("CancelTrip: %v", err)
	}

	a, ok := e.avisos.Ultimo(notify.SucesoTrayectoAnulado)
	if !ok {
		t.Fatal("no se avisó a quien se queda sin plaza")
	}
	if a.Para != e.pasajero.Email {
		t.Errorf("aviso a %q, esperaba al pasajero", a.Para)
	}
}

func TestAnularUnaReservaAvisaALaOtraParte(t *testing.T) {
	e := nuevoConAvisos(t)
	b := e.reservar(t)
	e.avisos.Limpiar()

	// Anula el pasajero: el aviso va a quien organiza.
	if _, err := e.svc.CancelBooking(b.ID, e.pasajero.ID); err != nil {
		t.Fatalf("CancelBooking: %v", err)
	}
	a, ok := e.avisos.Ultimo(notify.SucesoReservaAnulada)
	if !ok || a.Para != e.host.Email {
		t.Fatalf("aviso = %+v, esperaba que fuera a quien organiza", a)
	}
}

func TestNadieSeAvisaASiMismo(t *testing.T) {
	// Recibir un correo por algo que acabas de hacer tú es ruido.
	e := nuevoConAvisos(t)
	b := e.reservar(t)

	// Al pedir plaza solo se avisa a quien organiza.
	for _, a := range e.avisos.Para(e.pasajero.Email) {
		if a.Suceso == notify.SucesoPlazaPedida {
			t.Error("el pasajero se ha avisado a sí mismo de su propia petición")
		}
	}

	e.avisos.Limpiar()
	_, _ = e.svc.CancelBooking(b.ID, e.pasajero.ID)
	if n := len(e.avisos.Para(e.pasajero.Email)); n != 0 {
		t.Errorf("el pasajero recibe %d avisos de su propia anulación", n)
	}
}

func TestSeAvisaUnaSolaVezAlAlcanzarElNivelVerificado(t *testing.T) {
	// Un correo por cada comprobación superada son cuatro correos idénticos a
	// quien acaba de verificarse. El aviso es por cruzar el umbral, no por
	// cada paso.
	svc, identidad, grabador := newTestServiceConAvisos(t)
	sess, err := svc.Register(RegisterInput{Name: "Ana", Email: "ana@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	acreditar(t, svc, identidad, sess.User.ID, trust.LevelVerificado)

	var avisos int
	for _, a := range grabador.Avisos() {
		if a.Suceso == notify.SucesoIdentidadVerificada {
			avisos++
		}
	}
	if avisos != 1 {
		t.Fatalf("avisos de identidad verificada = %d, esperaba exactamente 1", avisos)
	}
}

func TestUnaComprobacionSueltaNoAnunciaNada(t *testing.T) {
	// Con el documento solo no se llega a verificado, así que no hay nada que
	// celebrar todavía.
	svc, identidad, grabador := newTestServiceConAvisos(t)
	sess, _ := svc.Register(RegisterInput{Name: "Ana", Email: "ana@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})

	ctx := context.Background()
	abierta, _, err := svc.IniciarVerificacion(ctx, sess.User.ID, trust.CheckGovernmentID)
	if err != nil {
		t.Fatalf("IniciarVerificacion: %v", err)
	}
	_ = identidad.Resolve(abierta.Ref, trust.Outcome{Status: trust.StatusVerified})
	if _, err := svc.RefrescarVerificacion(ctx, abierta.Ref); err != nil {
		t.Fatalf("RefrescarVerificacion: %v", err)
	}

	if _, ok := grabador.Ultimo(notify.SucesoIdentidadVerificada); ok {
		t.Fatal("se anunció la identidad acreditada con una sola comprobación")
	}
}

func TestUnRechazoDeIdentidadTambienSeAvisa(t *testing.T) {
	// Quedarse sin saber por qué no puedes usar la app es la peor experiencia
	// posible.
	svc, identidad, grabador := newTestServiceConAvisos(t)
	sess, _ := svc.Register(RegisterInput{Name: "Ana", Email: "ana@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})

	ctx := context.Background()
	abierta, _, _ := svc.IniciarVerificacion(ctx, sess.User.ID, trust.CheckGovernmentID)
	_ = identidad.Resolve(abierta.Ref, trust.Outcome{Status: trust.StatusRejected})
	if _, err := svc.RefrescarVerificacion(ctx, abierta.Ref); err != nil {
		t.Fatalf("RefrescarVerificacion: %v", err)
	}

	if _, ok := grabador.Ultimo(notify.SucesoIdentidadRechazada); !ok {
		t.Fatal("no se avisó del rechazo")
	}
}

func TestUnCorreoCaidoNoDeshaceLaReserva(t *testing.T) {
	// La garantía que sostiene el diseño: los avisos son aparte.
	svc, identidad, _ := newTestServiceConAvisos(t)
	hostSess, _ := svc.Register(RegisterInput{Name: "Ana", Email: "ana@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})
	pasSess, _ := svc.Register(RegisterInput{Name: "Bruno", Email: "bruno@example.com", Password: testPassword, Idioma: "en", AceptaTerminos: true})
	acreditar(t, svc, identidad, hostSess.User.ID, trust.LevelVerificado)
	acreditar(t, svc, identidad, pasSess.User.ID, trust.LevelVerificado)

	trip, _ := svc.CreateTrip(context.Background(), NewTripInput{
		HostID: hostSess.User.ID, Origin: downtown, Destination: airport,
		DepartureTime: time.Now().UTC().Add(3 * time.Hour),
		Vehicle:       domain.VehicleModelY, MaxDetourKm: 2,
	})

	// Se sustituye el notificador por uno que no hace nada, como si el correo
	// estuviera caído.
	svc.cfg.Avisos = notify.Silencio{}

	b, err := svc.RequestBooking(BookInput{
		TripID: trip.ID, PassengerID: pasSess.User.ID,
		Pickup: riverside, Dropoff: airport,
	})
	if err != nil {
		t.Fatalf("la reserva falló por culpa del correo: %v", err)
	}
	if b.Status != domain.BookingPending {
		t.Fatalf("estado = %q", b.Status)
	}
}
