// Package pricing estima lo que cuesta un trayecto en robotaxi y reparte ese
// coste entre quienes lo comparten.
package pricing

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// Tariff son los parámetros de facturación del robotaxi.
//
// Los valores por defecto son una estimación de mercado, no una tarifa oficial
// de Tesla: se configuran desde fuera para poder ajustarlos sin tocar código.
type Tariff struct {
	BaseCents      int64 `json:"base_cents"`       // banderada
	PerKmCents     int64 `json:"per_km_cents"`     // coste por kilómetro
	PerMinuteCents int64 `json:"per_minute_cents"` // coste por minuto de trayecto
	MinimumCents   int64 `json:"minimum_cents"`    // importe mínimo por viaje
}

// DefaultTariff es la estimación con la que arranca la app en Austin.
func DefaultTariff() Tariff {
	return Tariff{
		BaseCents:      200,
		PerKmCents:     62, // ≈ 1,00 $/milla
		PerMinuteCents: 15,
		MinimumCents:   500,
	}
}

// TripCostCents estima el coste total del trayecto para el vehículo completo.
func (t Tariff) TripCostCents(distanceKm, durationMin float64) int64 {
	if distanceKm < 0 {
		distanceKm = 0
	}
	if durationMin < 0 {
		durationMin = 0
	}
	cost := t.BaseCents +
		int64(math.Round(distanceKm*float64(t.PerKmCents))) +
		int64(math.Round(durationMin*float64(t.PerMinuteCents)))
	if cost < t.MinimumCents {
		return t.MinimumCents
	}
	return cost
}

// Occupant es alguien a bordo entre los kilómetros StartKm y EndKm de la ruta.
// Seats permite que una persona reserve más de una plaza (viaja acompañada) y
// pague en proporción.
type Occupant struct {
	ID      string
	StartKm float64
	EndKm   float64
	Seats   int
}

// SplitFare reparte totalCents entre los ocupantes.
//
// El criterio es el que hace justo compartir un tramo: la ruta se corta en
// rodajas por cada subida y bajada, y el coste de cada rodaja se divide entre
// las plazas ocupadas mientras dura. Quien solo hace la mitad del camino paga
// la mitad del camino, y solo compartida con quien iba a bordo en ese momento.
//
// Las rodajas que nadie ocupa se cargan a fallbackID (quien organiza, que es
// quien reserva el vehículo). El resultado siempre suma exactamente totalCents.
func SplitFare(totalCents int64, routeKm float64, occupants []Occupant, fallbackID string) map[string]int64 {
	shares := map[string]int64{}
	if len(occupants) == 0 || routeKm <= 0 || totalCents <= 0 {
		if fallbackID != "" {
			shares[fallbackID] = totalCents
		}
		return shares
	}

	// Cortes: cada subida y cada bajada abre una rodaja nueva.
	cuts := []float64{0, routeKm}
	for _, o := range occupants {
		cuts = append(cuts, clamp(o.StartKm, 0, routeKm), clamp(o.EndKm, 0, routeKm))
	}
	sort.Float64s(cuts)

	exact := map[string]float64{}
	for i := 1; i < len(cuts); i++ {
		sliceKm := cuts[i] - cuts[i-1]
		if sliceKm <= 0 {
			continue
		}
		mid := (cuts[i-1] + cuts[i]) / 2
		sliceCost := float64(totalCents) * sliceKm / routeKm

		seatsAboard := 0
		for _, o := range occupants {
			if o.StartKm <= mid && mid < o.EndKm {
				seatsAboard += seatsOf(o)
			}
		}
		if seatsAboard == 0 {
			if fallbackID != "" {
				exact[fallbackID] += sliceCost
			}
			continue
		}
		for _, o := range occupants {
			if o.StartKm <= mid && mid < o.EndKm {
				exact[o.ID] += sliceCost * float64(seatsOf(o)) / float64(seatsAboard)
			}
		}
	}

	return roundToTotal(exact, totalCents)
}

// EstimateSeatPrice calcula lo que pagaría un ocupante nuevo si se sumara a un
// trayecto con los ocupantes actuales. Es el precio que se enseña al buscar.
func EstimateSeatPrice(totalCents int64, routeKm float64, current []Occupant, candidate Occupant, fallbackID string) int64 {
	withCandidate := make([]Occupant, 0, len(current)+1)
	withCandidate = append(withCandidate, current...)
	withCandidate = append(withCandidate, candidate)
	return SplitFare(totalCents, routeKm, withCandidate, fallbackID)[candidate.ID]
}

// roundToTotal pasa los importes exactos a céntimos enteros repartiendo los
// restos por el método del mayor resto, de modo que la suma cuadre al céntimo.
func roundToTotal(exact map[string]float64, totalCents int64) map[string]int64 {
	ids := make([]string, 0, len(exact))
	for id := range exact {
		ids = append(ids, id)
	}
	sort.Strings(ids) // reparto determinista ante empates

	out := make(map[string]int64, len(ids))
	var assigned int64
	for _, id := range ids {
		v := int64(math.Floor(exact[id]))
		out[id] = v
		assigned += v
	}

	remainder := totalCents - assigned
	sort.SliceStable(ids, func(i, j int) bool {
		return frac(exact[ids[i]]) > frac(exact[ids[j]])
	})
	for i := 0; remainder > 0 && len(ids) > 0; i++ {
		out[ids[i%len(ids)]]++
		remainder--
	}
	return out
}

func frac(v float64) float64 { return v - math.Floor(v) }

func seatsOf(o Occupant) int {
	if o.Seats < 1 {
		return 1
	}
	return o.Seats
}

func clamp(v, lo, hi float64) float64 { return math.Min(hi, math.Max(lo, v)) }

// ErrLucro indica que el reparto daría beneficio a quien organiza.
var ErrLucro = errors.New("el reparto daría beneficio a quien organiza")

// VerificarSinLucro comprueba que quien organiza no gana dinero con el viaje.
//
// No es un escrúpulo moral: es la línea que separa compartir gastos de prestar
// un servicio de transporte. La ley de Texas excluye expresamente de la
// regulación de las TNC los acuerdos de gastos compartidos y aquellos en los
// que "la cantidad recibida no excede el coste de proporcionar el viaje"
// (Tex. Occ. Code § 2402.001). En cuanto quien organiza gana algo, el servicio
// pasa a ser transporte comercial y necesita permiso estatal.
//
// Por eso esta comprobación se ejecuta sobre cada reparto: la propiedad de la
// que depende la legalidad del producto no puede quedar en manos de que nadie
// toque el algoritmo por descuido.
func VerificarSinLucro(totalCents int64, shares map[string]int64, hostID string) error {
	var recaudado int64
	for id, v := range shares {
		if v < 0 {
			return fmt.Errorf("%w: %s tiene una parte negativa (%d)", ErrLucro, id, v)
		}
		if id != hostID {
			recaudado += v
		}
	}
	// Lo que ponen los demás no puede superar el coste del viaje: si lo
	// superara, la diferencia sería beneficio.
	if recaudado > totalCents {
		return fmt.Errorf("%w: los pasajeros aportan %d sobre un coste de %d",
			ErrLucro, recaudado, totalCents)
	}
	// Y quien organiza tiene que poner algo o, como mucho, nada: nunca cobrar.
	if resto := totalCents - recaudado; resto < 0 {
		return fmt.Errorf("%w: quien organiza cobraría %d", ErrLucro, -resto)
	}
	return nil
}
