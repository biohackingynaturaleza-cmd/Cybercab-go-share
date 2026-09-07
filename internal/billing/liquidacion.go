package billing

import (
	"fmt"
	"sort"
	"time"
)

// Posicion es el neto de una persona en el periodo.
type Posicion struct {
	UserID string `json:"user_id"`
	// DebeCents es lo que esa persona debe a otras; LeDebenCents, lo que otras
	// le deben a ella. Una misma persona suele tener las dos cosas: quien
	// comparte a diario alterna entre organizar y viajar.
	DebeCents     int64 `json:"debe_cents"`
	LeDebenCents  int64 `json:"le_deben_cents"`
	ComisionCents int64 `json:"comision_cents"`
	// NetoCents es lo que de verdad se mueve: positivo se le cobra, negativo
	// se le paga. La compensación es lo que evita cobrar y pagar a la misma
	// persona en el mismo periodo.
	NetoCents int64 `json:"neto_cents"`
	Apuntes   int   `json:"apuntes"`
}

// TipoInstruccion distingue un cobro de un pago.
type TipoInstruccion string

const (
	Cobro TipoInstruccion = "cobro"
	Pago  TipoInstruccion = "pago"
)

// Instruccion es lo que se le pide al procesador. La app no mueve dinero: lo
// describe.
type Instruccion struct {
	UserID      string          `json:"user_id"`
	Tipo        TipoInstruccion `json:"tipo"`
	AmountCents int64           `json:"amount_cents"`
}

// Liquidacion es el resultado de cerrar un periodo.
type Liquidacion struct {
	ID            string        `json:"id"`
	Desde         time.Time     `json:"desde"`
	Hasta         time.Time     `json:"hasta"`
	Posiciones    []Posicion    `json:"posiciones"`
	Instrucciones []Instruccion `json:"instrucciones"`
	// ComisionTotalCents es nuestro ingreso bruto del periodo.
	ComisionTotalCents int64     `json:"comision_total_cents"`
	ApuntesLiquidados  int       `json:"apuntes_liquidados"`
	CreatedAt          time.Time `json:"created_at"`
}

// Liquidar compensa los apuntes pendientes y produce las instrucciones de un
// solo cobro (o pago) por persona.
//
// Devuelve también los identificadores de los apuntes incluidos, para marcarlos
// y que no puedan liquidarse dos veces.
func Liquidar(id string, entries []Entry, desde, hasta time.Time, now time.Time) (*Liquidacion, []string, error) {
	posiciones := map[string]*Posicion{}
	posicionDe := func(userID string) *Posicion {
		if p, ok := posiciones[userID]; ok {
			return p
		}
		p := &Posicion{UserID: userID}
		posiciones[userID] = p
		return p
	}

	var incluidos []string
	var comisionTotal int64

	for _, e := range entries {
		if !e.Pendiente() || e.CreatedAt.Before(desde) || !e.CreatedAt.Before(hasta) {
			continue
		}
		if err := e.Validate(); err != nil {
			return nil, nil, fmt.Errorf("apunte %s no válido: %w", e.ID, err)
		}

		deudor := posicionDe(e.UserID)
		deudor.DebeCents += e.AmountCents
		deudor.Apuntes++

		switch e.Kind {
		case EntryServiceFee:
			deudor.ComisionCents += e.AmountCents
			comisionTotal += e.AmountCents
		default:
			acreedor := posicionDe(e.CounterpartyID)
			acreedor.LeDebenCents += e.AmountCents
		}
		incluidos = append(incluidos, e.ID)
	}

	orden := make([]string, 0, len(posiciones))
	for id := range posiciones {
		orden = append(orden, id)
	}
	sort.Strings(orden) // salida determinista

	out := &Liquidacion{
		ID: id, Desde: desde, Hasta: hasta,
		ComisionTotalCents: comisionTotal,
		ApuntesLiquidados:  len(incluidos),
		CreatedAt:          now,
	}
	for _, userID := range orden {
		p := posiciones[userID]
		p.NetoCents = p.DebeCents - p.LeDebenCents
		out.Posiciones = append(out.Posiciones, *p)

		switch {
		case p.NetoCents > 0:
			out.Instrucciones = append(out.Instrucciones,
				Instruccion{UserID: userID, Tipo: Cobro, AmountCents: p.NetoCents})
		case p.NetoCents < 0:
			out.Instrucciones = append(out.Instrucciones,
				Instruccion{UserID: userID, Tipo: Pago, AmountCents: -p.NetoCents})
			// Neto cero no genera instrucción: lo que se compensa no se mueve,
			// y cada movimiento que se evita es una comisión que no se paga.
		}
	}

	if err := out.Cuadra(); err != nil {
		return nil, nil, err
	}
	return out, incluidos, nil
}

// Cuadra comprueba la única igualdad que no puede fallar: lo que se cobra menos
// lo que se paga tiene que ser exactamente nuestra comisión.
//
// Si no cuadrara, o estaríamos perdiendo dinero de alguien o inventándolo. Se
// verifica antes de devolver la liquidación, no después de ejecutarla.
func (l *Liquidacion) Cuadra() error {
	var cobros, pagos int64
	for _, i := range l.Instrucciones {
		switch i.Tipo {
		case Cobro:
			cobros += i.AmountCents
		case Pago:
			pagos += i.AmountCents
		}
	}
	if cobros-pagos != l.ComisionTotalCents {
		return fmt.Errorf(
			"la liquidación no cuadra: se cobran %d, se pagan %d, diferencia %d, comisión %d",
			cobros, pagos, cobros-pagos, l.ComisionTotalCents)
	}
	return nil
}
