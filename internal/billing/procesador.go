package billing

import "math"

// Procesador son las tarifas del pasarela de pago.
//
// Lo que hace daño no es el porcentaje: es la parte fija. Sobre un cobro de
// tres euros, 0,30 $ son un 10 %; sobre uno de treinta, un 1 %. De ahí que
// agrupar sea la palanca más fuerte del modelo de ingresos.
type Procesador struct {
	// PorcentajeBps es la comisión variable en puntos básicos (290 = 2,9 %).
	PorcentajeBps int64
	// FijoCents es la parte fija de cada cobro.
	FijoCents int64
	// PagoFijoCents es lo que cuesta transferir dinero a alguien.
	PagoFijoCents int64
}

// StripeEstandar son las tarifas publicadas de referencia.
func StripeEstandar() Procesador {
	return Procesador{PorcentajeBps: 290, FijoCents: 30, PagoFijoCents: 25}
}

// CosteDe calcula lo que cuesta ejecutar una instrucción.
func (p Procesador) CosteDe(i Instruccion) int64 {
	if i.Tipo == Pago {
		return p.PagoFijoCents
	}
	return int64(math.Round(float64(i.AmountCents)*float64(p.PorcentajeBps)/10000)) + p.FijoCents
}

// CosteTotal es lo que cuesta ejecutar una liquidación entera.
func (p Procesador) CosteTotal(l *Liquidacion) int64 {
	var total int64
	for _, i := range l.Instrucciones {
		total += p.CosteDe(i)
	}
	return total
}

// CosteSiSeCobraraPorViaje estima lo que costaría el mismo periodo cobrando
// cada apunte por separado, sin compensar ni agrupar.
//
// Sirve para poder enseñar la diferencia con un número, no con una promesa.
func (p Procesador) CosteSiSeCobraraPorViaje(entries []Entry) int64 {
	// Un viaje genera dos apuntes para el mismo pasajero —su parte del coste y
	// la comisión—, que se cobrarían juntos. Se agrupan por reserva.
	porReserva := map[string]int64{}
	var sueltos int64
	for _, e := range entries {
		if e.BookingID == "" {
			sueltos += p.CosteDe(Instruccion{Tipo: Cobro, AmountCents: e.AmountCents})
			continue
		}
		porReserva[e.BookingID] += e.AmountCents
	}

	total := sueltos
	for _, importe := range porReserva {
		// Un cobro al pasajero y un pago a quien organiza, por cada viaje.
		total += p.CosteDe(Instruccion{Tipo: Cobro, AmountCents: importe})
		total += p.CosteDe(Instruccion{Tipo: Pago, AmountCents: importe})
	}
	return total
}

// Ahorro es la comparación entre liquidar agrupado y cobrar viaje a viaje.
type Ahorro struct {
	AgrupadoCents int64 `json:"agrupado_cents"`
	PorViajeCents int64 `json:"por_viaje_cents"`
	AhorroCents   int64 `json:"ahorro_cents"`
	// Veces es cuántas veces más barato sale agrupar.
	Veces float64 `json:"veces"`
}

// Comparar mide lo que se ahorra agrupando.
func (p Procesador) Comparar(l *Liquidacion, entries []Entry) Ahorro {
	agrupado := p.CosteTotal(l)
	porViaje := p.CosteSiSeCobraraPorViaje(entries)

	a := Ahorro{
		AgrupadoCents: agrupado,
		PorViajeCents: porViaje,
		AhorroCents:   porViaje - agrupado,
	}
	if agrupado > 0 {
		a.Veces = float64(porViaje) / float64(agrupado)
	}
	return a
}
