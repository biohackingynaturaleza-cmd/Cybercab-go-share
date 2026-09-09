package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/billing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pagos"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// procesadorFalso cuenta los movimientos y responde lo que se le diga. Sirve
// para comprobar la idempotencia sin depender de ningún servicio externo.
type procesadorFalso struct {
	mu     sync.Mutex
	vistos []pagos.Movimiento
	// falla indica por clave qué movimientos tienen que fallar.
	falla map[string]bool
}

func (p *procesadorFalso) Nombre() string { return "falso" }

func (p *procesadorFalso) Ejecutar(_ context.Context, m pagos.Movimiento) (*pagos.Resultado, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vistos = append(p.vistos, m)
	if p.falla[m.Clave] {
		return &pagos.Resultado{Estado: pagos.EstadoFallido, Motivo: "tarjeta rechazada"}, nil
	}
	return &pagos.Resultado{Estado: pagos.EstadoEjecutado, Ref: "mov_" + m.Clave}, nil
}

func (p *procesadorFalso) claves() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.vistos))
	for _, m := range p.vistos {
		out = append(out, m.Clave)
	}
	return out
}

// periodoDelViaje es el mes al que pertenecen los apuntes de un viaje cerrado.
func periodoDelViaje(entries []billing.Entry) (time.Time, time.Time) {
	return PeriodoDe(entries[0].CreatedAt)
}

func TestLaLiquidacionSeGuardaConSusInstrucciones(t *testing.T) {
	// Antes se calculaba y se perdía: los apuntes quedaban marcados como
	// cobrados y las instrucciones solo existían en la respuesta HTTP de quien
	// la lanzó. Sin guardarlas no hay a qué reintentar ni qué auditar.
	e, entries := viajeCompletado(t, 0)
	desde, hasta := periodoDelViaje(entries)

	liq, err := e.svc.LiquidarPeriodo(desde, hasta)
	if err != nil {
		t.Fatalf("LiquidarPeriodo: %v", err)
	}

	guardada, err := e.svc.GetLiquidacion(liq.ID)
	if err != nil {
		t.Fatalf("GetLiquidacion: %v", err)
	}
	if guardada.Estado != billing.LiquidacionCalculada {
		t.Fatalf("estado = %q, esperaba calculada", guardada.Estado)
	}
	if len(guardada.Instrucciones) != len(liq.Instrucciones) || len(guardada.Instrucciones) == 0 {
		t.Fatalf("instrucciones guardadas = %d", len(guardada.Instrucciones))
	}
	for _, in := range guardada.Instrucciones {
		if in.ID == "" || in.Estado != billing.EstadoPendiente {
			t.Fatalf("instrucción = %+v: sin identificador o sin estado inicial", in)
		}
	}
}

func TestCalcularNoEsCobrar(t *testing.T) {
	// El paso de cobrar es aparte a propósito: calcular es reversible mientras
	// nadie lo haya ejecutado, y cobrar no.
	e, entries := viajeCompletado(t, 0)
	desde, hasta := periodoDelViaje(entries)
	liq, err := e.svc.LiquidarPeriodo(desde, hasta)
	if err != nil {
		t.Fatalf("LiquidarPeriodo: %v", err)
	}
	for _, in := range liq.Instrucciones {
		if in.Estado != billing.EstadoPendiente {
			t.Fatalf("instrucción %s en estado %q: cerrar el periodo no puede mover dinero",
				in.ID, in.Estado)
		}
	}
}

func TestElProveedorPorDefectoNoFingeQueHaCobrado(t *testing.T) {
	// Un proveedor de mentira que devolviera "cobrado" dejaría el libro
	// diciendo que se cobró un dinero que nadie ha visto, y eso solo se
	// descubre cuando alguien reclama.
	e, entries := viajeCompletado(t, 0)
	desde, hasta := periodoDelViaje(entries)
	liq, err := e.svc.LiquidarPeriodo(desde, hasta)
	if err != nil {
		t.Fatalf("LiquidarPeriodo: %v", err)
	}

	ejecutada, err := e.svc.EjecutarLiquidacion(t.Context(), liq.ID)
	if err != nil {
		t.Fatalf("EjecutarLiquidacion: %v", err)
	}
	if ejecutada.Estado != billing.LiquidacionCalculada {
		t.Fatalf("estado = %q: sin procesador no se ha cobrado nada", ejecutada.Estado)
	}
	for _, in := range ejecutada.Instrucciones {
		if in.Estado != string(pagos.EstadoAnotado) {
			t.Fatalf("instrucción en estado %q, esperaba anotado", in.Estado)
		}
		if in.Motivo == "" {
			t.Fatal("no dice por qué se quedó sin ejecutar")
		}
	}
}

// conProcesador monta un escenario cerrado con un procesador de pagos falso.
func conProcesador(t *testing.T) (escenario, []billing.Entry, *procesadorFalso) {
	t.Helper()
	e, entries := viajeCompletado(t, 0)
	p := &procesadorFalso{falla: map[string]bool{}}
	e.svc.cfg.Pagos = p
	return e, entries, p
}

func TestEjecutarMandaUnMovimientoPorPersona(t *testing.T) {
	e, entries, proc := conProcesador(t)
	desde, hasta := periodoDelViaje(entries)
	liq, err := e.svc.LiquidarPeriodo(desde, hasta)
	if err != nil {
		t.Fatalf("LiquidarPeriodo: %v", err)
	}

	ejecutada, err := e.svc.EjecutarLiquidacion(t.Context(), liq.ID)
	if err != nil {
		t.Fatalf("EjecutarLiquidacion: %v", err)
	}
	if ejecutada.Estado != billing.LiquidacionEjecutada {
		t.Fatalf("estado = %q, esperaba ejecutada", ejecutada.Estado)
	}
	if len(proc.claves()) != len(liq.Instrucciones) {
		t.Fatalf("movimientos = %d, instrucciones = %d", len(proc.claves()), len(liq.Instrucciones))
	}
	for _, in := range ejecutada.Instrucciones {
		if in.Estado != string(pagos.EstadoEjecutado) || in.Ref == "" || in.EjecutadaAt.IsZero() {
			t.Fatalf("instrucción sin cerrar del todo: %+v", in)
		}
	}
	// Y el concepto dice de qué periodo es: quien lo lea en su extracto tiene
	// que poder reconocerlo.
	proc.mu.Lock()
	concepto := proc.vistos[0].Concepto
	proc.mu.Unlock()
	if !strings.Contains(concepto, desde.Format("01/2006")) {
		t.Fatalf("concepto = %q, esperaba que dijera el periodo", concepto)
	}
}

func TestReintentarNoCobraDosVeces(t *testing.T) {
	// La clave de idempotencia es lo único que separa un reintento de un cargo
	// duplicado.
	e, entries, proc := conProcesador(t)
	desde, hasta := periodoDelViaje(entries)
	liq, err := e.svc.LiquidarPeriodo(desde, hasta)
	if err != nil {
		t.Fatalf("LiquidarPeriodo: %v", err)
	}

	if _, err := e.svc.EjecutarLiquidacion(t.Context(), liq.ID); err != nil {
		t.Fatalf("primera ejecución: %v", err)
	}
	primeros := len(proc.claves())
	if _, err := e.svc.EjecutarLiquidacion(t.Context(), liq.ID); err != nil {
		t.Fatalf("segunda ejecución: %v", err)
	}
	if n := len(proc.claves()); n != primeros {
		t.Fatalf("movimientos tras reintentar = %d, esperaba los mismos %d", n, primeros)
	}
}

func TestUnMovimientoFallidoNoDetieneALosDemas(t *testing.T) {
	// A los demás hay que pagarles igual, y el que falla se reintenta luego
	// con su misma clave.
	e, entries, proc := conProcesador(t)
	desde, hasta := periodoDelViaje(entries)
	liq, err := e.svc.LiquidarPeriodo(desde, hasta)
	if err != nil {
		t.Fatalf("LiquidarPeriodo: %v", err)
	}
	if len(liq.Instrucciones) < 2 {
		t.Fatalf("esta prueba necesita al menos dos instrucciones, hay %d", len(liq.Instrucciones))
	}
	proc.falla[pagos.ClaveDe(liq.ID, liq.Instrucciones[0].UserID)] = true

	ejecutada, err := e.svc.EjecutarLiquidacion(t.Context(), liq.ID)
	if err != nil {
		t.Fatalf("EjecutarLiquidacion: %v", err)
	}
	if ejecutada.Estado != billing.LiquidacionParcial {
		t.Fatalf("estado = %q, esperaba parcial: alguien está esperando su dinero", ejecutada.Estado)
	}
	if len(proc.claves()) != len(liq.Instrucciones) {
		t.Fatalf("movimientos = %d: el fallo cortó los demás", len(proc.claves()))
	}

	// Y al reintentar solo se vuelve a intentar el que falló.
	proc.falla = map[string]bool{}
	antes := len(proc.claves())
	otraVez, err := e.svc.EjecutarLiquidacion(t.Context(), liq.ID)
	if err != nil {
		t.Fatalf("reintento: %v", err)
	}
	if n := len(proc.claves()) - antes; n != 1 {
		t.Fatalf("movimientos del reintento = %d, esperaba solo el que falló", n)
	}
	if otraVez.Estado != billing.LiquidacionEjecutada {
		t.Fatalf("estado tras el reintento = %q", otraVez.Estado)
	}
}

func TestLaLiquidacionAvisaACadaUnoDeLoSuyo(t *testing.T) {
	svc, identidad, avisos := newTestServiceConAvisos(t)
	e := escenarioCon(t, svc, identidad, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, svc, identidad, e.pasajero.ID, trust.LevelVerificado)
	b, err := e.reservar()
	if err != nil {
		t.Fatalf("RequestBooking: %v", err)
	}
	if _, err := svc.DecideBooking(DecisionInput{
		BookingID: b.ID, HostID: e.host.ID, Accept: true, AceptaResponsabilidad: true,
	}); err != nil {
		t.Fatalf("DecideBooking: %v", err)
	}
	_, entries, err := svc.CompletarViaje(CierreInput{TripID: e.trip.ID, HostID: e.host.ID})
	if err != nil {
		t.Fatalf("CompletarViaje: %v", err)
	}
	desde, hasta := periodoDelViaje(entries)
	if _, err := svc.LiquidarPeriodo(desde, hasta); err != nil {
		t.Fatalf("LiquidarPeriodo: %v", err)
	}

	// Al pasajero se le cobra; a quien organiza se le paga.
	cobro, ok := avisos.Ultimo(notify.SucesoLiquidacionCobro)
	if !ok {
		t.Fatal("no se avisó del cobro")
	}
	if cobro.Para != e.pasajero.Email {
		t.Fatalf("el cobro se avisó a %q", cobro.Para)
	}
	pago, ok := avisos.Ultimo(notify.SucesoLiquidacionPago)
	if !ok {
		t.Fatal("no se avisó del pago")
	}
	if pago.Para != e.host.Email {
		t.Fatalf("el pago se avisó a %q", pago.Para)
	}
	asunto, cuerpo := notify.Componer(cobro)
	if strings.Contains(asunto+cuerpo, "{") {
		t.Fatalf("quedaron huecos sin rellenar:\n%s\n%s", asunto, cuerpo)
	}
	if !strings.Contains(cuerpo, desde.Format("01/2006")) {
		t.Fatalf("el aviso no dice de qué periodo es:\n%s", cuerpo)
	}
}

func TestElProgramadorCierraLosMesesVencidosYNoElEnCurso(t *testing.T) {
	// Si el programador estuvo parado dos meses, al arrancar tiene que ponerse
	// al día. Y el mes en curso no se toca: todavía le pueden entrar apuntes.
	e, entries := viajeCompletado(t, 0)
	desdeViaje, _ := periodoDelViaje(entries)

	// Nos situamos dos meses después del viaje.
	e.svc.cfg.Now = func() time.Time { return desdeViaje.AddDate(0, 2, 15) }

	hechas, err := e.svc.LiquidarPendientes(e.svc.cfg.Now())
	if err != nil {
		t.Fatalf("LiquidarPendientes: %v", err)
	}
	if len(hechas) != 1 {
		t.Fatalf("liquidaciones = %d, esperaba solo la del mes del viaje", len(hechas))
	}
	if !hechas[0].Desde.Equal(desdeViaje) {
		t.Fatalf("periodo = %v, esperaba %v", hechas[0].Desde, desdeViaje)
	}

	// Volver a pasar no cierra nada nuevo.
	otra, err := e.svc.LiquidarPendientes(e.svc.cfg.Now())
	if err != nil {
		t.Fatalf("segunda vuelta: %v", err)
	}
	if len(otra) != 0 {
		t.Fatalf("la segunda vuelta cerró %d periodos", len(otra))
	}
}

func TestElProgramadorNoTocaElMesEnCurso(t *testing.T) {
	e, entries := viajeCompletado(t, 0)
	desdeViaje, _ := periodoDelViaje(entries)
	// Mismo mes que el viaje: todavía puede entrar más.
	e.svc.cfg.Now = func() time.Time { return desdeViaje.AddDate(0, 0, 20) }

	hechas, err := e.svc.LiquidarPendientes(e.svc.cfg.Now())
	if err != nil {
		t.Fatalf("LiquidarPendientes: %v", err)
	}
	if len(hechas) != 0 {
		t.Fatalf("cerró %d periodos: el mes en curso no se toca", len(hechas))
	}
}

func TestNoSePuedeLiquidarDosVecesElMismoPeriodo(t *testing.T) {
	// La barrera que impide que dos ejecuciones del programador cobren el mismo
	// mes dos veces.
	e, entries := viajeCompletado(t, 0)
	desde, hasta := periodoDelViaje(entries)
	if _, err := e.svc.LiquidarPeriodo(desde, hasta); err != nil {
		t.Fatalf("primera: %v", err)
	}

	// Un apunte nuevo del mismo periodo: la segunda vez ya no es "nada que
	// liquidar", es un periodo cerrado.
	if err := e.svc.store.CreateEntries([]billing.Entry{{
		ID: "ent_suelto", UserID: e.pasajero.ID, TripID: e.trip.ID,
		Kind: billing.EntryServiceFee, AmountCents: 100, CreatedAt: desde.Add(time.Hour),
	}}); err != nil {
		t.Fatalf("CreateEntries: %v", err)
	}
	if _, err := e.svc.LiquidarPeriodo(desde, hasta); !errors.Is(err, store.ErrPeriodoYaLiquidado) {
		t.Fatalf("segunda = %v, esperaba ErrPeriodoYaLiquidado", err)
	}
}

func TestElSaldoDesglozaLoQueSeDebeYLoQueTeDeben(t *testing.T) {
	// "Debes 2 €" sin explicar que son 12 menos 10 no se entiende.
	e, _ := viajeCompletado(t, 0)

	delPasajero, err := e.svc.SaldoDe(e.pasajero.ID)
	if err != nil {
		t.Fatalf("SaldoDe: %v", err)
	}
	if delPasajero.DebeCents <= 0 || delPasajero.ComisionCents <= 0 {
		t.Fatalf("saldo del pasajero = %+v", delPasajero)
	}
	if delPasajero.PendienteCents != delPasajero.DebeCents-delPasajero.LeDebenCents {
		t.Fatalf("el neto no cuadra con el desglose: %+v", delPasajero)
	}
	if delPasajero.ProximoCierre.IsZero() {
		t.Fatal("no dice cuándo se cierra el periodo")
	}

	delHost, err := e.svc.SaldoDe(e.host.ID)
	if err != nil {
		t.Fatalf("SaldoDe(host): %v", err)
	}
	if delHost.LeDebenCents <= 0 || delHost.PendienteCents >= 0 {
		t.Fatalf("a quien organiza le deben, no debe: %+v", delHost)
	}
}

func TestElSaldoRecogeLosMovimientosYaLiquidados(t *testing.T) {
	e, entries := viajeCompletado(t, 0)
	desde, hasta := periodoDelViaje(entries)
	if _, err := e.svc.LiquidarPeriodo(desde, hasta); err != nil {
		t.Fatalf("LiquidarPeriodo: %v", err)
	}

	saldo, err := e.svc.SaldoDe(e.pasajero.ID)
	if err != nil {
		t.Fatalf("SaldoDe: %v", err)
	}
	if saldo.PendienteCents != 0 || saldo.Apuntes != 0 {
		t.Fatalf("tras liquidar sigue habiendo pendiente: %+v", saldo)
	}
	if len(saldo.Movimientos) != 1 || saldo.Movimientos[0].Tipo != billing.Cobro {
		t.Fatalf("movimientos = %+v", saldo.Movimientos)
	}
}
