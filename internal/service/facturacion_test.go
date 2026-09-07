package service

import (
	"errors"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/billing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// viajeCompletado monta un trayecto con una reserva confirmada y lo cierra.
func viajeCompletado(t *testing.T, importeReal int64) (escenario, []billing.Entry) {
	t.Helper()
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

	_, entries, err := e.svc.CompletarViaje(e.trip.ID, e.host.ID, importeReal)
	if err != nil {
		t.Fatalf("CompletarViaje: %v", err)
	}
	return e, entries
}

func TestCompletarUnViajeAnotaPeroNoCobra(t *testing.T) {
	// Cerrar un viaje escribe en el libro; el dinero se mueve en la
	// liquidación, no aquí.
	e, entries := viajeCompletado(t, 0)

	if len(entries) != 2 {
		t.Fatalf("apuntes = %d, esperaba la parte del coste y la comisión", len(entries))
	}

	var coste, fee billing.Entry
	for _, en := range entries {
		switch en.Kind {
		case billing.EntryCostShare:
			coste = en
		case billing.EntryServiceFee:
			fee = en
		}
	}
	if coste.UserID != e.pasajero.ID || coste.CounterpartyID != e.host.ID {
		t.Errorf("la parte del coste va de %s a %s", coste.UserID, coste.CounterpartyID)
	}
	if fee.CounterpartyID != "" {
		t.Error("la comisión no debe tener contraparte: es de la plataforma")
	}
	// 20 % sobre la parte del pasajero.
	if esperado := comision(coste.AmountCents, ComisionPorDefectoBps); fee.AmountCents != esperado {
		t.Errorf("comisión = %d, esperaba %d", fee.AmountCents, esperado)
	}

	trip, _ := e.svc.GetTrip(e.trip.ID)
	if trip.Status != domain.TripCompleted {
		t.Errorf("estado = %q, esperaba completed", trip.Status)
	}
}

func TestLaComisionNoHaceGanarDineroAQuienOrganiza(t *testing.T) {
	// La comisión es nuestra, no suya: el reparto sigue cumpliendo la
	// excepción de gastos compartidos.
	e, entries := viajeCompletado(t, 0)

	fb, err := e.svc.FareBreakdownFor(e.trip.ID)
	if err != nil {
		t.Fatalf("FareBreakdownFor: %v", err)
	}
	var recibeElHost int64
	for _, en := range entries {
		if en.CounterpartyID == e.host.ID {
			recibeElHost += en.AmountCents
		}
	}
	if recibeElHost > fb.TotalCents {
		t.Fatalf("quien organiza recibe %d sobre un coste de %d", recibeElHost, fb.TotalCents)
	}
}

func TestElImporteRealDeLaFlotaSustituyeALaEstimacion(t *testing.T) {
	// Lo que de verdad cobró Tesla manda sobre nuestra estimación.
	estimado := escenarioEstimado(t)
	real := estimado + 500

	e, entries := viajeCompletado(t, real)

	fb, err := e.svc.FareBreakdownFor(e.trip.ID)
	if err != nil {
		t.Fatalf("FareBreakdownFor: %v", err)
	}
	_ = fb
	var coste int64
	for _, en := range entries {
		if en.Kind == billing.EntryCostShare {
			coste = en.AmountCents
		}
	}
	// Con un viaje más caro, la parte del pasajero sube.
	e2, entriesBaratos := viajeCompletado(t, 0)
	_ = e2
	var costeBarato int64
	for _, en := range entriesBaratos {
		if en.Kind == billing.EntryCostShare {
			costeBarato = en.AmountCents
		}
	}
	if coste <= costeBarato {
		t.Fatalf("con importe real %d la parte es %d, y con la estimación %d es %d: debería subir",
			real, coste, estimado, costeBarato)
	}
}

// escenarioEstimado devuelve el coste estimado de un trayecto del escenario.
func escenarioEstimado(t *testing.T) int64 {
	t.Helper()
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	fb, err := e.svc.FareBreakdownFor(e.trip.ID)
	if err != nil {
		t.Fatalf("FareBreakdownFor: %v", err)
	}
	return fb.TotalCents
}

func TestNoSePuedeCerrarDosVecesElMismoViaje(t *testing.T) {
	// Cerrar dos veces duplicaría los apuntes y cobraría dos veces.
	e, _ := viajeCompletado(t, 0)

	if _, _, err := e.svc.CompletarViaje(e.trip.ID, e.host.ID, 0); !errors.Is(err, ErrViajeNoCompletable) {
		t.Fatalf("error = %v, esperaba ErrViajeNoCompletable", err)
	}
}

func TestSoloQuienOrganizaCierraElViaje(t *testing.T) {
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	if _, _, err := e.svc.CompletarViaje(e.trip.ID, e.pasajero.ID, 0); !errors.Is(err, ErrNoAutorizado) {
		t.Fatalf("error = %v, esperaba ErrNoAutorizado", err)
	}
}

func TestNoSeCobraAQuienNoLlegoASubirse(t *testing.T) {
	// Una reserva pendiente o rechazada no genera deuda.
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelVerificado)
	if _, err := e.reservar(); err != nil {
		t.Fatalf("RequestBooking: %v", err)
	}
	// Se queda en pendiente: nadie la confirma.

	_, entries, err := e.svc.CompletarViaje(e.trip.ID, e.host.ID, 0)
	if err != nil {
		t.Fatalf("CompletarViaje: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("apuntes = %d, esperaba ninguno: la plaza no llegó a confirmarse", len(entries))
	}
}

func TestLiquidarProduceUnSoloMovimientoPorPersona(t *testing.T) {
	e, entries := viajeCompletado(t, 0)
	desde := entries[0].CreatedAt.Add(-time.Hour)
	hasta := entries[0].CreatedAt.Add(time.Hour)

	liq, err := e.svc.LiquidarPeriodo(desde, hasta)
	if err != nil {
		t.Fatalf("LiquidarPeriodo: %v", err)
	}
	if err := liq.Cuadra(); err != nil {
		t.Fatalf("%v", err)
	}
	if len(liq.Instrucciones) != 2 {
		t.Fatalf("instrucciones = %d, esperaba una por persona", len(liq.Instrucciones))
	}
	// Al pasajero un solo cargo con su parte y la comisión juntas.
	for _, i := range liq.Instrucciones {
		if i.UserID == e.pasajero.ID && i.Tipo != billing.Cobro {
			t.Errorf("al pasajero se le %s, esperaba un cobro", i.Tipo)
		}
		if i.UserID == e.host.ID && i.Tipo != billing.Pago {
			t.Errorf("a quien organiza se le %s, esperaba un pago", i.Tipo)
		}
	}
}

func TestLoLiquidadoNoEntraEnElSiguienteCierre(t *testing.T) {
	e, entries := viajeCompletado(t, 0)
	desde := entries[0].CreatedAt.Add(-time.Hour)
	hasta := entries[0].CreatedAt.Add(time.Hour)

	if _, err := e.svc.LiquidarPeriodo(desde, hasta); err != nil {
		t.Fatalf("primer cierre: %v", err)
	}
	segunda, err := e.svc.LiquidarPeriodo(desde, hasta)
	if err != nil {
		t.Fatalf("segundo cierre: %v", err)
	}
	if segunda.ApuntesLiquidados != 0 || len(segunda.Instrucciones) != 0 {
		t.Fatalf("el segundo cierre volvería a cobrar: %+v", segunda)
	}
}

func TestUnPeriodoDelReVesSeRechaza(t *testing.T) {
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	ahora := time.Now().UTC()
	if _, err := e.svc.LiquidarPeriodo(ahora, ahora.Add(-time.Hour)); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error = %v, esperaba un error de validación", err)
	}
}
