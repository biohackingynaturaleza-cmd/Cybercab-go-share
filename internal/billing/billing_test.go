package billing

import (
	"fmt"
	"testing"
	"time"
)

var (
	inicio = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	fin    = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	ahora  = time.Date(2026, 10, 1, 6, 0, 0, 0, time.UTC)
)

// viaje genera los dos apuntes de un viaje compartido: lo que el pasajero debe
// a quien organiza, y la comisión que nos debe a nosotros.
func viaje(n int, pasajero, host string, parteCents, comisionCents int64, cuando time.Time) []Entry {
	booking := fmt.Sprintf("bkg_%d", n)
	return []Entry{
		{
			ID: fmt.Sprintf("ent_%da", n), UserID: pasajero, TripID: fmt.Sprintf("trip_%d", n),
			BookingID: booking, Kind: EntryCostShare, AmountCents: parteCents,
			CounterpartyID: host, CreatedAt: cuando,
		},
		{
			ID: fmt.Sprintf("ent_%db", n), UserID: pasajero, TripID: fmt.Sprintf("trip_%d", n),
			BookingID: booking, Kind: EntryServiceFee, AmountCents: comisionCents,
			CreatedAt: cuando,
		},
	}
}

func liquidar(t *testing.T, entries []Entry) *Liquidacion {
	t.Helper()
	l, _, err := Liquidar("liq_1", entries, inicio, fin, ahora)
	if err != nil {
		t.Fatalf("Liquidar: %v", err)
	}
	return l
}

func posicionDe(l *Liquidacion, userID string) Posicion {
	for _, p := range l.Posiciones {
		if p.UserID == userID {
			return p
		}
	}
	return Posicion{}
}

func instruccionDe(l *Liquidacion, userID string) (Instruccion, bool) {
	for _, i := range l.Instrucciones {
		if i.UserID == userID {
			return i, true
		}
	}
	return Instruccion{}, false
}

// --- Apuntes ---

func TestValidateRechazaApuntesSinSentido(t *testing.T) {
	casos := map[string]Entry{
		"sin usuario":              {Kind: EntryServiceFee, AmountCents: 100},
		"importe cero":             {UserID: "a", Kind: EntryServiceFee, AmountCents: 0},
		"importe negativo":         {UserID: "a", Kind: EntryServiceFee, AmountCents: -100},
		"coste sin contraparte":    {UserID: "a", Kind: EntryCostShare, AmountCents: 100},
		"comisión con contraparte": {UserID: "a", Kind: EntryServiceFee, AmountCents: 100, CounterpartyID: "b"},
		"se debe a sí mismo":       {UserID: "a", Kind: EntryCostShare, AmountCents: 100, CounterpartyID: "a"},
	}
	for nombre, e := range casos {
		if err := e.Validate(); err == nil {
			t.Errorf("%s: esperaba un error", nombre)
		}
	}
}

// --- La igualdad que no puede fallar ---

func TestLoQueSeCobraMenosLoQueSePagaEsExactamenteLaComision(t *testing.T) {
	// Si esta igualdad se rompiera, o perderíamos dinero de alguien o lo
	// estaríamos inventando.
	casos := map[string][]Entry{
		"un viaje": viaje(1, "bruno", "ana", 337, 67, inicio.Add(time.Hour)),
		"varios viajes con el mismo host": append(
			viaje(1, "bruno", "ana", 337, 67, inicio.Add(time.Hour)),
			viaje(2, "carla", "ana", 512, 102, inicio.Add(48*time.Hour))...),
		"cadena de tres personas": append(append(
			viaje(1, "bruno", "ana", 337, 67, inicio.Add(time.Hour)),
			viaje(2, "carla", "bruno", 500, 100, inicio.Add(24*time.Hour))...),
			viaje(3, "ana", "carla", 700, 140, inicio.Add(48*time.Hour))...),
	}
	for nombre, entries := range casos {
		t.Run(nombre, func(t *testing.T) {
			l := liquidar(t, entries)
			if err := l.Cuadra(); err != nil {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestCuadraDetectaUnaLiquidacionRota(t *testing.T) {
	roto := &Liquidacion{
		Instrucciones: []Instruccion{
			{UserID: "a", Tipo: Cobro, AmountCents: 1000},
			{UserID: "b", Tipo: Pago, AmountCents: 500},
		},
		ComisionTotalCents: 100, // debería ser 500
	}
	if err := roto.Cuadra(); err == nil {
		t.Fatal("esperaba que detectara el descuadre")
	}
}

// --- Compensación ---

func TestQuienAlternaOrganizarYViajarCompensaSusSaldos(t *testing.T) {
	// El caso del commuter: lunes lleva Ana, martes lleva Bruno. Casi todo el
	// dinero se cancela solo y no llega a moverse.
	entries := append(
		viaje(1, "bruno", "ana", 340, 68, inicio.Add(24*time.Hour)),
		viaje(2, "ana", "bruno", 340, 68, inicio.Add(48*time.Hour))...)

	l := liquidar(t, entries)

	// Las partes del coste se anulan; solo queda la comisión de cada cual.
	for _, quien := range []string{"ana", "bruno"} {
		i, ok := instruccionDe(l, quien)
		if !ok {
			t.Fatalf("%s no tiene instrucción", quien)
		}
		if i.Tipo != Cobro || i.AmountCents != 68 {
			t.Errorf("%s: %s de %d, esperaba un cobro de 68 (solo la comisión)",
				quien, i.Tipo, i.AmountCents)
		}
	}
	// Sin compensar habría cuatro movimientos; con compensación, dos.
	if len(l.Instrucciones) != 2 {
		t.Fatalf("instrucciones = %d, esperaba 2", len(l.Instrucciones))
	}
}

func TestUnSaldoQueSeCompensaDelTodoNoGeneraMovimiento(t *testing.T) {
	// Sin comisión, dos viajes idénticos cruzados se cancelan enteros. Cada
	// movimiento que se evita es una comisión de procesador que no se paga.
	entries := append(
		viaje(1, "bruno", "ana", 340, 0, inicio.Add(24*time.Hour))[:1],
		viaje(2, "ana", "bruno", 340, 0, inicio.Add(48*time.Hour))[:1]...)

	l := liquidar(t, entries)
	if len(l.Instrucciones) != 0 {
		t.Fatalf("instrucciones = %v, esperaba ninguna", l.Instrucciones)
	}
	if err := l.Cuadra(); err != nil {
		t.Fatalf("%v", err)
	}
}

func TestQuienSoloOrganizaCobra(t *testing.T) {
	entries := append(
		viaje(1, "bruno", "ana", 337, 67, inicio.Add(time.Hour)),
		viaje(2, "carla", "ana", 512, 102, inicio.Add(48*time.Hour))...)

	l := liquidar(t, entries)

	ana, ok := instruccionDe(l, "ana")
	if !ok || ana.Tipo != Pago {
		t.Fatalf("Ana: %+v, esperaba un pago", ana)
	}
	if ana.AmountCents != 337+512 {
		t.Errorf("a Ana se le pagan %d, esperaba %d", ana.AmountCents, 337+512)
	}

	// A los pasajeros se les cobra su parte más la comisión, en un solo cargo.
	bruno, _ := instruccionDe(l, "bruno")
	if bruno.Tipo != Cobro || bruno.AmountCents != 337+67 {
		t.Errorf("Bruno: %s de %d, esperaba cobro de %d", bruno.Tipo, bruno.AmountCents, 337+67)
	}
}

func TestLaPosicionDesglosaAmbasDirecciones(t *testing.T) {
	entries := append(
		viaje(1, "bruno", "ana", 340, 68, inicio.Add(24*time.Hour)),
		viaje(2, "ana", "bruno", 500, 100, inicio.Add(48*time.Hour))...)

	l := liquidar(t, entries)
	bruno := posicionDe(l, "bruno")

	if bruno.DebeCents != 340+68 {
		t.Errorf("Bruno debe %d, esperaba %d", bruno.DebeCents, 340+68)
	}
	if bruno.LeDebenCents != 500 {
		t.Errorf("a Bruno le deben %d, esperaba 500", bruno.LeDebenCents)
	}
	if bruno.ComisionCents != 68 {
		t.Errorf("comisión de Bruno = %d, esperaba 68", bruno.ComisionCents)
	}
	if bruno.NetoCents != 340+68-500 {
		t.Errorf("neto de Bruno = %d, esperaba %d", bruno.NetoCents, 340+68-500)
	}
}

// --- Periodo y doble cobro ---

func TestSoloEntraLoDelPeriodo(t *testing.T) {
	entries := append(
		viaje(1, "bruno", "ana", 337, 67, inicio.Add(-time.Hour)), // antes
		viaje(2, "carla", "ana", 500, 100, inicio.Add(time.Hour))...)
	entries = append(entries, viaje(3, "dani", "ana", 900, 180, fin.Add(time.Hour))...) // después

	l := liquidar(t, entries)
	if l.ApuntesLiquidados != 2 {
		t.Fatalf("apuntes liquidados = %d, esperaba 2", l.ApuntesLiquidados)
	}
	if _, ok := instruccionDe(l, "bruno"); ok {
		t.Error("entró un apunte anterior al periodo")
	}
	if _, ok := instruccionDe(l, "dani"); ok {
		t.Error("entró un apunte posterior al periodo")
	}
}

func TestLoYaLiquidadoNoSeVuelveACobrar(t *testing.T) {
	entries := viaje(1, "bruno", "ana", 337, 67, inicio.Add(time.Hour))
	for i := range entries {
		entries[i].SettlementID = "liq_anterior"
	}

	l := liquidar(t, entries)
	if l.ApuntesLiquidados != 0 || len(l.Instrucciones) != 0 {
		t.Fatalf("se ha vuelto a liquidar lo ya cobrado: %+v", l)
	}
}

func TestLiquidarDevuelveLosApuntesIncluidos(t *testing.T) {
	// Es lo que permite marcarlos y que no entren en la siguiente liquidación.
	entries := viaje(1, "bruno", "ana", 337, 67, inicio.Add(time.Hour))

	_, incluidos, err := Liquidar("liq_1", entries, inicio, fin, ahora)
	if err != nil {
		t.Fatalf("Liquidar: %v", err)
	}
	if len(incluidos) != 2 {
		t.Fatalf("incluidos = %v, esperaba los dos apuntes", incluidos)
	}
}

func TestUnApunteInvalidoDetieneLaLiquidacion(t *testing.T) {
	// Mejor no liquidar que liquidar mal: el dinero ya movido no se deshace.
	entries := viaje(1, "bruno", "ana", 337, 67, inicio.Add(time.Hour))
	entries[0].CounterpartyID = "" // parte del coste sin acreedor

	if _, _, err := Liquidar("liq_1", entries, inicio, fin, ahora); err == nil {
		t.Fatal("esperaba que la liquidación fallara")
	}
}

// --- La razón de todo esto ---

func TestAgruparSaleMuchoMasBaratoQueCobrarViajeAViaje(t *testing.T) {
	// Un mes de un commuter: 40 viajes compartidos.
	var entries []Entry
	for n := 1; n <= 40; n++ {
		cuando := inicio.Add(time.Duration(n) * 12 * time.Hour)
		if cuando.After(fin) {
			cuando = fin.Add(-time.Hour)
		}
		entries = append(entries, viaje(n, "bruno", "ana", 337, 67, cuando)...)
	}

	l := liquidar(t, entries)
	ahorro := StripeEstandar().Comparar(l, entries)

	if ahorro.AhorroCents <= 0 {
		t.Fatalf("agrupar no ahorra nada: %+v", ahorro)
	}
	if ahorro.Veces < 5 {
		t.Errorf("agrupar sale %.1f veces más barato, esperaba bastante más", ahorro.Veces)
	}
	t.Logf("40 viajes: agrupado %d cts, por viaje %d cts, %.0f veces más barato",
		ahorro.AgrupadoCents, ahorro.PorViajeCents, ahorro.Veces)

	// Y lo que de verdad importa: qué queda de la comisión después del pago.
	netoAgrupado := l.ComisionTotalCents - ahorro.AgrupadoCents
	netoPorViaje := l.ComisionTotalCents - ahorro.PorViajeCents
	t.Logf("comisión bruta %d cts -> neto agrupado %d cts, neto por viaje %d cts",
		l.ComisionTotalCents, netoAgrupado, netoPorViaje)
	if netoAgrupado <= netoPorViaje {
		t.Fatal("agrupar tiene que dejar más ingreso neto")
	}
}

func TestElCosteDelProcesadorCastigaLosImportesPequenos(t *testing.T) {
	p := StripeEstandar()
	pequeno := p.CosteDe(Instruccion{Tipo: Cobro, AmountCents: 404})
	grande := p.CosteDe(Instruccion{Tipo: Cobro, AmountCents: 16160}) // 40 veces más

	// La parte fija hace que 40 cobros pequeños cuesten muchísimo más que uno
	// grande del mismo importe total.
	if 40*pequeno <= grande {
		t.Fatalf("40 cobros de 404 cuestan %d y uno de 16160 cuesta %d", 40*pequeno, grande)
	}
	proporcionPequeno := float64(pequeno) / 404
	proporcionGrande := float64(grande) / 16160
	if proporcionPequeno <= proporcionGrande*2 {
		t.Errorf("el cobro pequeño se lleva %.1f%% y el grande %.1f%%",
			proporcionPequeno*100, proporcionGrande*100)
	}
}
