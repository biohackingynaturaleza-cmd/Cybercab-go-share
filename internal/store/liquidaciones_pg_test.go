package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/billing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
)

// apuntesDeUnViaje escribe la pareja de apuntes que deja un viaje cerrado: la
// parte del coste que el pasajero le debe a quien organiza, y la comisión.
func apuntesDeUnViaje(t *testing.T, pg *store.Postgres, host, pasajero, tripID string, now time.Time) []billing.Entry {
	t.Helper()
	entries := []billing.Entry{
		{
			ID: "ent_coste", UserID: pasajero, TripID: tripID, BookingID: "bkg_1",
			Kind: billing.EntryCostShare, AmountCents: 560, CounterpartyID: host,
			CreatedAt: now,
		},
		{
			ID: "ent_comision", UserID: pasajero, TripID: tripID, BookingID: "bkg_1",
			Kind: billing.EntryServiceFee, AmountCents: 112, CreatedAt: now,
		},
	}
	if err := pg.CreateEntries(entries); err != nil {
		t.Fatalf("CreateEntries: %v", err)
	}
	return entries
}

// liquidacionDePrueba compensa los apuntes pendientes y devuelve la liquidación
// sin guardar, ya con los identificadores de sus instrucciones puestos.
func liquidacionDePrueba(t *testing.T, pg *store.Postgres, id string, desde, hasta time.Time) (*billing.Liquidacion, []string) {
	t.Helper()
	pendientes, err := pg.PendingEntries()
	if err != nil {
		t.Fatalf("PendingEntries: %v", err)
	}
	liq, incluidos, err := billing.Liquidar(id, pendientes, desde, hasta, time.Now().UTC().Truncate(time.Millisecond))
	if err != nil {
		t.Fatalf("Liquidar: %v", err)
	}
	for i := range liq.Instrucciones {
		liq.Instrucciones[i].ID = id + "_ins_" + liq.Instrucciones[i].UserID
	}
	return liq, incluidos
}

func TestPostgresLiquidacionIdaYVuelta(t *testing.T) {
	pg := newPostgres(t)
	host, pasajero, tripID, _ := parejaConReserva(t, pg)
	now := time.Now().UTC().Truncate(time.Millisecond)
	apuntesDeUnViaje(t, pg, host.ID, pasajero.ID, tripID, now)

	desde, hasta := now.Add(-time.Hour), now.Add(time.Hour)
	liq, incluidos := liquidacionDePrueba(t, pg, "liq_1", desde, hasta)
	if err := pg.GuardarLiquidacion(liq, incluidos); err != nil {
		t.Fatalf("GuardarLiquidacion: %v", err)
	}

	leida, err := pg.GetLiquidacion("liq_1")
	if err != nil {
		t.Fatalf("GetLiquidacion: %v", err)
	}
	if leida.ComisionTotalCents != liq.ComisionTotalCents || leida.ApuntesLiquidados != 2 {
		t.Fatalf("leída = %+v", leida)
	}
	if leida.Estado != billing.LiquidacionCalculada {
		t.Fatalf("estado = %q, esperaba calculada", leida.Estado)
	}
	if !leida.EjecutadaAt.IsZero() {
		t.Fatalf("una liquidación sin ejecutar trae fecha de ejecución: %v", leida.EjecutadaAt)
	}
	if len(leida.Instrucciones) != len(liq.Instrucciones) || len(leida.Instrucciones) != 2 {
		t.Fatalf("instrucciones = %d, esperaba una por persona", len(leida.Instrucciones))
	}
	for _, in := range leida.Instrucciones {
		if in.Estado != billing.EstadoPendiente || in.AmountCents <= 0 {
			t.Fatalf("instrucción = %+v", in)
		}
		if !in.EjecutadaAt.IsZero() {
			t.Fatalf("instrucción sin ejecutar con fecha: %+v", in)
		}
	}
}

func TestPostgresGuardarCierraLosApuntesEnLaMismaTransaccion(t *testing.T) {
	// Una liquidación guardada cuyos apuntes siguieran abiertos los cobraría
	// otra vez el mes que viene.
	pg := newPostgres(t)
	host, pasajero, tripID, _ := parejaConReserva(t, pg)
	now := time.Now().UTC().Truncate(time.Millisecond)
	apuntesDeUnViaje(t, pg, host.ID, pasajero.ID, tripID, now)

	desde, hasta := now.Add(-time.Hour), now.Add(time.Hour)
	liq, incluidos := liquidacionDePrueba(t, pg, "liq_1", desde, hasta)
	if err := pg.GuardarLiquidacion(liq, incluidos); err != nil {
		t.Fatalf("GuardarLiquidacion: %v", err)
	}

	pendientes, err := pg.PendingEntries()
	if err != nil {
		t.Fatalf("PendingEntries: %v", err)
	}
	if len(pendientes) != 0 {
		t.Fatalf("quedan %d apuntes abiertos tras liquidar", len(pendientes))
	}
}

func TestPostgresUnPeriodoNoSeLiquidaDosVeces(t *testing.T) {
	// La barrera que impide que dos ejecuciones del programador cobren el mismo
	// mes dos veces.
	pg := newPostgres(t)
	host, pasajero, tripID, _ := parejaConReserva(t, pg)
	now := time.Now().UTC().Truncate(time.Millisecond)
	apuntesDeUnViaje(t, pg, host.ID, pasajero.ID, tripID, now)

	desde, hasta := now.Add(-time.Hour), now.Add(time.Hour)
	liq, incluidos := liquidacionDePrueba(t, pg, "liq_1", desde, hasta)
	if err := pg.GuardarLiquidacion(liq, incluidos); err != nil {
		t.Fatalf("primera: %v", err)
	}

	// Otro identificador, el mismo periodo: el índice único lo rechaza.
	otra, _ := liquidacionDePrueba(t, pg, "liq_2", desde, hasta)
	otra.Desde, otra.Hasta = desde, hasta
	if err := pg.GuardarLiquidacion(otra, nil); !errors.Is(err, store.ErrPeriodoYaLiquidado) {
		t.Fatalf("segunda = %v, esperaba ErrPeriodoYaLiquidado", err)
	}
	todas, err := pg.Liquidaciones()
	if err != nil {
		t.Fatalf("Liquidaciones: %v", err)
	}
	if len(todas) != 1 {
		t.Fatalf("liquidaciones = %d, esperaba que la segunda no llegara a escribirse", len(todas))
	}
}

func TestPostgresUnApunteYaLiquidadoTumbaLaTransaccionEntera(t *testing.T) {
	// Las tres cosas o ninguna: si otro cerró los apuntes mientras tanto, la
	// liquidación no puede quedarse escrita a medias.
	pg := newPostgres(t)
	host, pasajero, tripID, _ := parejaConReserva(t, pg)
	now := time.Now().UTC().Truncate(time.Millisecond)
	apuntesDeUnViaje(t, pg, host.ID, pasajero.ID, tripID, now)

	desde, hasta := now.Add(-time.Hour), now.Add(time.Hour)
	primera, incluidos := liquidacionDePrueba(t, pg, "liq_1", desde, hasta)
	if err := pg.GuardarLiquidacion(primera, incluidos); err != nil {
		t.Fatalf("primera: %v", err)
	}

	// Una segunda liquidación, de otro periodo, que intenta cerrar los mismos
	// apuntes que ya cerró la primera.
	segunda := &billing.Liquidacion{
		ID: "liq_2", Desde: desde.Add(-48 * time.Hour), Hasta: desde.Add(-24 * time.Hour),
		Estado: billing.LiquidacionCalculada, CreatedAt: now,
	}
	if err := pg.GuardarLiquidacion(segunda, incluidos); !errors.Is(err, store.ErrPeriodoYaLiquidado) {
		t.Fatalf("err = %v, esperaba ErrPeriodoYaLiquidado", err)
	}
	if _, err := pg.GetLiquidacion("liq_2"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("la liquidación se escribió pese a fallar el cierre: %v", err)
	}
}

func TestPostgresEjecutarUnaInstruccionYSuLiquidacion(t *testing.T) {
	pg := newPostgres(t)
	host, pasajero, tripID, _ := parejaConReserva(t, pg)
	now := time.Now().UTC().Truncate(time.Millisecond)
	apuntesDeUnViaje(t, pg, host.ID, pasajero.ID, tripID, now)

	desde, hasta := now.Add(-time.Hour), now.Add(time.Hour)
	liq, incluidos := liquidacionDePrueba(t, pg, "liq_1", desde, hasta)
	if err := pg.GuardarLiquidacion(liq, incluidos); err != nil {
		t.Fatalf("GuardarLiquidacion: %v", err)
	}

	in := liq.Instrucciones[0]
	in.Estado, in.Ref, in.EjecutadaAt = "ejecutado", "mov_abc", now.Add(time.Minute)
	if err := pg.ActualizarInstruccion(liq.ID, in); err != nil {
		t.Fatalf("ActualizarInstruccion: %v", err)
	}
	if err := pg.ActualizarEstadoLiquidacion(liq.ID, billing.LiquidacionParcial, now); err != nil {
		t.Fatalf("ActualizarEstadoLiquidacion: %v", err)
	}

	leida, err := pg.GetLiquidacion(liq.ID)
	if err != nil {
		t.Fatalf("GetLiquidacion: %v", err)
	}
	if leida.Estado != billing.LiquidacionParcial {
		t.Fatalf("estado = %q", leida.Estado)
	}
	// Parcial no lleva fecha de ejecución: solo la lleva lo que se movió entero.
	if !leida.EjecutadaAt.IsZero() {
		t.Fatalf("una liquidación parcial trae fecha de ejecución: %v", leida.EjecutadaAt)
	}

	var vista bool
	for _, x := range leida.Instrucciones {
		if x.ID != in.ID {
			continue
		}
		vista = true
		if x.Estado != "ejecutado" || x.Ref != "mov_abc" || x.EjecutadaAt.IsZero() {
			t.Fatalf("instrucción = %+v", x)
		}
	}
	if !vista {
		t.Fatal("la instrucción actualizada no volvió")
	}

	// Y al cerrarla entera sí queda la fecha.
	if err := pg.ActualizarEstadoLiquidacion(liq.ID, billing.LiquidacionEjecutada, now.Add(time.Hour)); err != nil {
		t.Fatalf("ActualizarEstadoLiquidacion: %v", err)
	}
	leida, err = pg.GetLiquidacion(liq.ID)
	if err != nil {
		t.Fatalf("GetLiquidacion: %v", err)
	}
	if leida.EjecutadaAt.IsZero() {
		t.Fatal("una liquidación ejecutada sin fecha de ejecución")
	}
}

func TestPostgresLosMovimientosDeCadaUnoSonSuyos(t *testing.T) {
	pg := newPostgres(t)
	host, pasajero, tripID, _ := parejaConReserva(t, pg)
	now := time.Now().UTC().Truncate(time.Millisecond)
	apuntesDeUnViaje(t, pg, host.ID, pasajero.ID, tripID, now)

	desde, hasta := now.Add(-time.Hour), now.Add(time.Hour)
	liq, incluidos := liquidacionDePrueba(t, pg, "liq_1", desde, hasta)
	if err := pg.GuardarLiquidacion(liq, incluidos); err != nil {
		t.Fatalf("GuardarLiquidacion: %v", err)
	}

	for _, caso := range []struct {
		userID string
		tipo   billing.TipoInstruccion
	}{
		{pasajero.ID, billing.Cobro},
		{host.ID, billing.Pago},
	} {
		movs, err := pg.InstruccionesDe(caso.userID)
		if err != nil {
			t.Fatalf("InstruccionesDe(%s): %v", caso.userID, err)
		}
		if len(movs) != 1 {
			t.Fatalf("%s tiene %d movimientos, esperaba 1", caso.userID, len(movs))
		}
		if movs[0].Tipo != caso.tipo {
			t.Fatalf("a %s se le %s, esperaba %s", caso.userID, movs[0].Tipo, caso.tipo)
		}
		if movs[0].UserID != caso.userID {
			t.Fatalf("el movimiento es de %s", movs[0].UserID)
		}
	}
}

func TestPostgresLaTablaSettlementsYaNoExiste(t *testing.T) {
	// La creó la migración 003 y nunca la usó nadie; la 013 la borra. Una tabla
	// vacía con nombre de algo importante es una trampa para quien venga
	// después.
	pg := newPostgres(t)
	if _, err := pg.Liquidaciones(); err != nil {
		t.Fatalf("Liquidaciones: %v", err)
	}
	if pg.ExisteTabla("settlements") {
		t.Fatal("la tabla settlements sigue ahí")
	}
	if !pg.ExisteTabla("liquidaciones") {
		t.Fatal("falta la tabla liquidaciones, que es la que la sustituye")
	}
}
