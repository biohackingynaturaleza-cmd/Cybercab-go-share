package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/matching"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// escenario monta dos personas y un trayecto con el vehículo indicado, sin
// acreditar a nadie: cada prueba decide qué nivel tiene cada cual.
type escenario struct {
	svc       *Service
	identidad *trust.Manual
	host      *domain.User
	pasajero  *domain.User
	trip      *domain.Trip
}

func nuevoEscenario(t *testing.T, vehicle domain.VehicleType, exige trust.Level) escenario {
	t.Helper()
	svc, identidad := newTestService(t)

	hostSess, err := svc.Register(RegisterInput{Name: "Ana", Email: "ana@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})
	if err != nil {
		t.Fatalf("Register(host): %v", err)
	}
	pasajeroSess, err := svc.Register(RegisterInput{Name: "Bruno", Email: "bruno@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})
	if err != nil {
		t.Fatalf("Register(pasajero): %v", err)
	}

	// Quien organiza siempre acreditado: así el fallo de cada prueba señala al
	// pasajero y no se confunden las dos causas.
	acreditar(t, svc, identidad, hostSess.User.ID, trust.LevelVerificado)

	trip, err := svc.CreateTrip(context.Background(), NewTripInput{
		HostID:        hostSess.User.ID,
		Origin:        downtown,
		Destination:   airport,
		DepartureTime: time.Now().UTC().Add(2 * time.Hour),
		Vehicle:       vehicle,
		MaxDetourKm:   2,
		MinTrustLevel: exige,
	})
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	return escenario{svc: svc, identidad: identidad, host: hostSess.User, pasajero: pasajeroSess.User, trip: trip}
}

func (e escenario) reservar() (*domain.Booking, error) {
	return e.svc.RequestBooking(BookInput{
		TripID: e.trip.ID, PassengerID: e.pasajero.ID,
		Pickup: riverside, Dropoff: airport,
	})
}

// --- El suelo del biplaza ---

func TestElCybercabExigeIdentidadVerificadaAunqueNadieLaPida(t *testing.T) {
	// La regla de seguridad central. Quien organiza no pidió nada
	// (MinTrustLevel = nuevo), pero el vehículo es biplaza: dos personas solas
	// sin conductor. El suelo se aplica igual.
	e := nuevoEscenario(t, domain.VehicleCybercab, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelBasico)

	_, err := e.reservar()
	if !errors.Is(err, trust.ErrConfianzaInsuficiente) {
		t.Fatalf("error = %v, esperaba que el suelo del biplaza lo impidiera", err)
	}
}

func TestElCybercabAdmiteAQuienSiTieneIdentidadVerificada(t *testing.T) {
	e := nuevoEscenario(t, domain.VehicleCybercab, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelVerificado)

	if _, err := e.reservar(); err != nil {
		t.Fatalf("RequestBooking: %v", err)
	}
}

func TestElVehiculoDeCuatroPlazasEsMasPermisivo(t *testing.T) {
	// Con más gente a bordo hay testigos, y el suelo puede bajar sin que el
	// viaje deje de ser razonablemente seguro.
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelBasico)

	if _, err := e.reservar(); err != nil {
		t.Fatalf("RequestBooking: %v", err)
	}
}

func TestSinNingunaVerificacionNoSeSubeANingunVehiculo(t *testing.T) {
	for _, vehiculo := range []domain.VehicleType{domain.VehicleCybercab, domain.VehicleModelY} {
		e := nuevoEscenario(t, vehiculo, trust.LevelNuevo)
		// El pasajero no acredita nada.
		if _, err := e.reservar(); !errors.Is(err, trust.ErrConfianzaInsuficiente) {
			t.Errorf("%s: error = %v, esperaba confianza insuficiente", vehiculo, err)
		}
	}
}

func TestQuienOrganizaPuedeExigirMasQueElSuelo(t *testing.T) {
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelVerificado)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelBasico)

	if _, err := e.reservar(); !errors.Is(err, trust.ErrConfianzaInsuficiente) {
		t.Fatalf("error = %v, esperaba que se respetara el nivel exigido", err)
	}
}

func TestElRequisitoCorreEnLasDosDirecciones(t *testing.T) {
	// Quien organiza tiene que cumplir el nivel que él mismo exige: si no,
	// alguien sin verificar podría publicar un viaje y pedir identidad
	// acreditada a los demás.
	svc, identidad := newTestService(t)
	hostSess, _ := svc.Register(RegisterInput{Name: "Ana", Email: "ana@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})
	pasajeroSess, _ := svc.Register(RegisterInput{Name: "Bruno", Email: "bruno@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})

	// Quien organiza solo llega a básico; el pasajero está verificado.
	acreditar(t, svc, identidad, hostSess.User.ID, trust.LevelBasico)
	acreditar(t, svc, identidad, pasajeroSess.User.ID, trust.LevelVerificado)

	trip, err := svc.CreateTrip(context.Background(), NewTripInput{
		HostID:        hostSess.User.ID,
		Origin:        downtown,
		Destination:   airport,
		DepartureTime: time.Now().UTC().Add(2 * time.Hour),
		Vehicle:       domain.VehicleCybercab,
	})
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}

	_, err = svc.RequestBooking(BookInput{
		TripID: trip.ID, PassengerID: pasajeroSess.User.ID,
		Pickup: riverside, Dropoff: airport,
	})
	if !errors.Is(err, trust.ErrConfianzaInsuficiente) {
		t.Fatalf("error = %v: quien organiza tampoco alcanza el nivel exigido", err)
	}
}

func TestElRechazoExplicaQueFalta(t *testing.T) {
	// Negar el acceso sin decir qué hacer para conseguirlo solo genera
	// abandono.
	e := nuevoEscenario(t, domain.VehicleCybercab, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelBasico)

	_, err := e.reservar()
	var req *RequisitoNoCumplido
	if !errors.As(err, &req) {
		t.Fatalf("error = %v, esperaba un RequisitoNoCumplido", err)
	}
	if req.Exigido != trust.LevelVerificado || req.Actual != trust.LevelBasico {
		t.Errorf("exigido = %v, actual = %v", req.Exigido, req.Actual)
	}
	quiero := map[trust.CheckKind]bool{trust.CheckGovernmentID: true, trust.CheckSelfie: true}
	if len(req.TeFaltan) != 2 {
		t.Fatalf("te faltan = %v, esperaba documento y selfie", req.TeFaltan)
	}
	for _, k := range req.TeFaltan {
		if !quiero[k] {
			t.Errorf("no esperaba que faltara %s", k)
		}
	}
	if req.Motivo == "" {
		t.Error("el rechazo no explica el motivo")
	}
}

func TestLaPlazaNoSeRetieneSiFallaLaConfianza(t *testing.T) {
	// Quien no puede subirse no debe llegar siquiera a ocupar un asiento.
	e := nuevoEscenario(t, domain.VehicleCybercab, trust.LevelNuevo)

	if _, err := e.reservar(); err == nil {
		t.Fatal("esperaba que la reserva fuera rechazada")
	}
	trip, _ := e.svc.GetTrip(e.trip.ID)
	if trip.SeatsTaken != 0 {
		t.Fatalf("plazas ocupadas = %d, esperaba 0", trip.SeatsTaken)
	}
	if trip.Status != domain.TripOpen {
		t.Fatalf("estado = %q, esperaba que siguiera abierto", trip.Status)
	}
}

// --- Verificación ---

func TestFlujoDeVerificacion(t *testing.T) {
	svc, identidad := newTestService(t)
	sess, _ := svc.Register(RegisterInput{Name: "Ana", Email: "ana@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})
	ctx := context.Background()

	abierta, check, err := svc.IniciarVerificacion(ctx, sess.User.ID, trust.CheckGovernmentID)
	if err != nil {
		t.Fatalf("IniciarVerificacion: %v", err)
	}
	if check.Status != trust.StatusPending || abierta.RedirectURL == "" {
		t.Fatalf("comprobación = %+v, sesión = %+v", check, abierta)
	}

	// Mientras el proveedor no resuelva, nada cambia.
	sinResolver, err := svc.RefrescarVerificacion(ctx, abierta.Ref)
	if err != nil {
		t.Fatalf("RefrescarVerificacion: %v", err)
	}
	if sinResolver.Status != trust.StatusPending {
		t.Fatalf("estado = %q, esperaba que siguiera pendiente", sinResolver.Status)
	}

	caduca := time.Now().UTC().Add(365 * 24 * time.Hour)
	if err := identidad.Resolve(abierta.Ref, trust.Outcome{
		Status: trust.StatusVerified, DocumentExpiresAt: caduca,
	}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	resuelta, err := svc.RefrescarVerificacion(ctx, abierta.Ref)
	if err != nil {
		t.Fatalf("RefrescarVerificacion: %v", err)
	}
	if resuelta.Status != trust.StatusVerified {
		t.Fatalf("estado = %q, esperaba verificada", resuelta.Status)
	}
	if !resuelta.ExpiresAt.Equal(caduca) {
		t.Errorf("caducidad = %v, esperaba %v", resuelta.ExpiresAt, caduca)
	}
}

func TestUnRechazoNoSePuedePisarConUnVeredictoPosterior(t *testing.T) {
	svc, identidad := newTestService(t)
	sess, _ := svc.Register(RegisterInput{Name: "Ana", Email: "ana@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})
	ctx := context.Background()

	abierta, _, _ := svc.IniciarVerificacion(ctx, sess.User.ID, trust.CheckGovernmentID)
	_ = identidad.Resolve(abierta.Ref, trust.Outcome{Status: trust.StatusRejected, Reason: "documento ilegible"})
	if _, err := svc.RefrescarVerificacion(ctx, abierta.Ref); err != nil {
		t.Fatalf("RefrescarVerificacion: %v", err)
	}

	// El proveedor cambia de opinión (o alguien reenvía un webhook falso).
	_ = identidad.Resolve(abierta.Ref, trust.Outcome{Status: trust.StatusVerified})
	check, err := svc.RefrescarVerificacion(ctx, abierta.Ref)
	if err != nil {
		t.Fatalf("RefrescarVerificacion: %v", err)
	}
	if check.Status != trust.StatusRejected {
		t.Fatalf("estado = %q: un rechazo resuelto no se puede reabrir", check.Status)
	}
}

func TestNoSePuedeAbrirDosVecesLaMismaComprobacion(t *testing.T) {
	svc, _ := newTestService(t)
	sess, _ := svc.Register(RegisterInput{Name: "Ana", Email: "ana@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})
	ctx := context.Background()

	if _, _, err := svc.IniciarVerificacion(ctx, sess.User.ID, trust.CheckPhone); err != nil {
		t.Fatalf("primera: %v", err)
	}
	if _, _, err := svc.IniciarVerificacion(ctx, sess.User.ID, trust.CheckPhone); err == nil {
		t.Fatal("esperaba un error al abrir una segunda comprobación del mismo tipo")
	}
}

func TestElPerfilNoFiltraDatosSensibles(t *testing.T) {
	svc, identidad := newTestService(t)
	sess, _ := svc.Register(RegisterInput{Name: "Ana", Email: "ana@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})
	acreditar(t, svc, identidad, sess.User.ID, trust.LevelVerificado)

	p, err := svc.PerfilDe(sess.User.ID)
	if err != nil {
		t.Fatalf("PerfilDe: %v", err)
	}
	if p.Nivel != trust.LevelVerificado {
		t.Errorf("nivel = %v, esperaba verificado", p.Nivel)
	}
	if len(p.Verificaciones) != 4 {
		t.Errorf("verificaciones = %v, esperaba 4", p.Verificaciones)
	}
	// El perfil dice qué se ha acreditado, nunca el dato acreditado.
	if p.Nombre != "Ana" {
		t.Errorf("nombre = %q", p.Nombre)
	}
}

// --- Bloqueos ---

func TestBloquearImpideCompartirTrayecto(t *testing.T) {
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelVerificado)

	if err := e.svc.Bloquear(e.pasajero.ID, e.host.ID); err != nil {
		t.Fatalf("Bloquear: %v", err)
	}
	if _, err := e.reservar(); !errors.Is(err, ErrBloqueado) {
		t.Fatalf("error = %v, esperaba ErrBloqueado", err)
	}
}

func TestElBloqueoCortaEnLasDosDirecciones(t *testing.T) {
	// Quien bloquea no quiere ver a la otra persona, y quien es bloqueado
	// tampoco debe poder buscarla.
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelVerificado)

	// Bloquea quien organiza, no el pasajero.
	if err := e.svc.Bloquear(e.host.ID, e.pasajero.ID); err != nil {
		t.Fatalf("Bloquear: %v", err)
	}
	if _, err := e.reservar(); !errors.Is(err, ErrBloqueado) {
		t.Fatalf("error = %v, esperaba ErrBloqueado en el sentido contrario", err)
	}
}

func TestLaBusquedaOcultaLosTrayectosBloqueados(t *testing.T) {
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)

	visibles, err := e.svc.Search(context.Background(), queryDePrueba(e.pasajero.ID))
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(visibles) != 1 {
		t.Fatalf("resultados antes de bloquear = %d, esperaba 1", len(visibles))
	}

	if err := e.svc.Bloquear(e.pasajero.ID, e.host.ID); err != nil {
		t.Fatalf("Bloquear: %v", err)
	}
	ocultos, err := e.svc.Search(context.Background(), queryDePrueba(e.pasajero.ID))
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ocultos) != 0 {
		t.Fatalf("resultados tras bloquear = %d, esperaba 0", len(ocultos))
	}
}

func TestDesbloquearDevuelveLaVisibilidad(t *testing.T) {
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	_ = e.svc.Bloquear(e.pasajero.ID, e.host.ID)

	if err := e.svc.Desbloquear(e.pasajero.ID, e.host.ID); err != nil {
		t.Fatalf("Desbloquear: %v", err)
	}
	visibles, _ := e.svc.Search(context.Background(), queryDePrueba(e.pasajero.ID))
	if len(visibles) != 1 {
		t.Fatalf("resultados tras desbloquear = %d, esperaba 1", len(visibles))
	}
}

// queryDePrueba es la búsqueda Riverside → aeropuerto que casa con el
// trayecto del escenario.
func queryDePrueba(viajeroID string) matching.Query {
	return matching.Query{
		Pickup:    riverside.Point,
		Dropoff:   airport.Point,
		ViajeroID: viajeroID,
	}
}

func TestNoTePuedesBloquearATiMismo(t *testing.T) {
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	if err := e.svc.Bloquear(e.host.ID, e.host.ID); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error = %v, esperaba un error de validación", err)
	}
}

// --- Reglas que vienen de fuera del código ---

func TestNoSePuedeAceptarSinAsumirLaResponsabilidad(t *testing.T) {
	// Los términos del robotaxi hacen a quien organiza responsable de la
	// conducta de quien deja subir. Aceptar sin saberlo sería ocultarle una
	// obligación real frente a la flota.
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelVerificado)
	b, err := e.reservar()
	if err != nil {
		t.Fatalf("RequestBooking: %v", err)
	}

	_, err = e.svc.DecideBooking(DecisionInput{
		BookingID: b.ID, HostID: e.host.ID, Accept: true,
		AceptaResponsabilidad: false,
	})
	if !errors.Is(err, ErrResponsabilidadNoAceptada) {
		t.Fatalf("error = %v, esperaba ErrResponsabilidadNoAceptada", err)
	}

	// La reserva sigue pendiente: no se ha aceptado a medias.
	sigue, _ := e.svc.BookingsByTrip(e.trip.ID)
	if sigue[0].Status != domain.BookingPending {
		t.Fatalf("estado = %q, esperaba que siguiera pendiente", sigue[0].Status)
	}
}

func TestRechazarNoExigeAsumirNada(t *testing.T) {
	// Solo aceptar a alguien conlleva responsabilidad; decir que no, no.
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelVerificado)
	b, _ := e.reservar()

	rechazada, err := e.svc.DecideBooking(DecisionInput{
		BookingID: b.ID, HostID: e.host.ID, Accept: false,
	})
	if err != nil {
		t.Fatalf("DecideBooking(rechazo): %v", err)
	}
	if rechazada.Status != domain.BookingRejected {
		t.Fatalf("estado = %q, esperaba rejected", rechazada.Status)
	}
}

func TestNingunRepartoDaBeneficioAQuienOrganiza(t *testing.T) {
	// La propiedad de la que depende que esto sea gasto compartido y no
	// transporte comercial: quien organiza nunca recauda más de lo que cuesta
	// el viaje. Se comprueba sobre el desglose real del servicio.
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelVerificado)
	if _, err := e.reservar(); err != nil {
		t.Fatalf("RequestBooking: %v", err)
	}

	fb, err := e.svc.FareBreakdownFor(e.trip.ID)
	if err != nil {
		t.Fatalf("FareBreakdownFor: %v", err)
	}

	var recaudadoPorLosDemas, totalRepartido int64
	for _, s := range fb.Shares {
		totalRepartido += s.AmountCents
		if s.UserID != e.host.ID {
			recaudadoPorLosDemas += s.AmountCents
		}
	}
	if totalRepartido != fb.TotalCents {
		t.Fatalf("el reparto suma %d y el coste es %d", totalRepartido, fb.TotalCents)
	}
	if recaudadoPorLosDemas > fb.TotalCents {
		t.Fatalf("los pasajeros aportan %d sobre un coste de %d: eso sería beneficio",
			recaudadoPorLosDemas, fb.TotalCents)
	}
	// Y la parte de quien organiza nunca es negativa: no cobra, comparte.
	if fb.Shares[0].AmountCents < 0 {
		t.Fatalf("quien organiza cobraría %d", -fb.Shares[0].AmountCents)
	}
}
