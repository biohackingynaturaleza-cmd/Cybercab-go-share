package pricing

import (
	"errors"
	"testing"
)

func sum(m map[string]int64) int64 {
	var total int64
	for _, v := range m {
		total += v
	}
	return total
}

func TestTripCostAplicaElMinimo(t *testing.T) {
	tariff := Tariff{BaseCents: 100, PerKmCents: 50, PerMinuteCents: 10, MinimumCents: 500}
	if got := tariff.TripCostCents(0.2, 1); got != 500 {
		t.Fatalf("coste de un trayecto mínimo = %d, esperaba 500", got)
	}
}

func TestTripCostSumaBaseDistanciaYTiempo(t *testing.T) {
	tariff := Tariff{BaseCents: 200, PerKmCents: 60, PerMinuteCents: 15}
	// 200 + 10 km * 60 + 20 min * 15 = 1100
	if got := tariff.TripCostCents(10, 20); got != 1100 {
		t.Fatalf("coste = %d, esperaba 1100", got)
	}
}

func TestSplitFareTodoElCaminoCompartidoEsMitadYMitad(t *testing.T) {
	shares := SplitFare(1000, 10, []Occupant{
		{ID: "ana", StartKm: 0, EndKm: 10},
		{ID: "bruno", StartKm: 0, EndKm: 10},
	}, "ana")

	if shares["ana"] != 500 || shares["bruno"] != 500 {
		t.Fatalf("reparto = %v, esperaba 500/500", shares)
	}
}

func TestSplitFareQuienHaceMedioCaminoPagaMenos(t *testing.T) {
	// Ana va de 0 a 10 km; Bruno solo comparte la segunda mitad.
	// Tramo 0-5 km (500 cts): solo Ana → 500 para Ana.
	// Tramo 5-10 km (500 cts): las dos → 250 cada una.
	shares := SplitFare(1000, 10, []Occupant{
		{ID: "ana", StartKm: 0, EndKm: 10},
		{ID: "bruno", StartKm: 5, EndKm: 10},
	}, "ana")

	if shares["ana"] != 750 {
		t.Errorf("Ana paga %d, esperaba 750", shares["ana"])
	}
	if shares["bruno"] != 250 {
		t.Errorf("Bruno paga %d, esperaba 250", shares["bruno"])
	}
}

func TestSplitFareCuadraSiempreAlCentimo(t *testing.T) {
	cases := []struct {
		name      string
		total     int64
		routeKm   float64
		occupants []Occupant
	}{
		{"importe indivisible entre tres", 1000, 9, []Occupant{
			{ID: "a", StartKm: 0, EndKm: 9},
			{ID: "b", StartKm: 0, EndKm: 9},
			{ID: "c", StartKm: 0, EndKm: 9},
		}},
		{"tramos solapados irregulares", 4737, 23.4, []Occupant{
			{ID: "a", StartKm: 0, EndKm: 23.4},
			{ID: "b", StartKm: 1.7, EndKm: 19.2},
			{ID: "c", StartKm: 8.1, EndKm: 23.4},
			{ID: "d", StartKm: 12.5, EndKm: 14.9},
		}},
		{"un solo ocupante", 999, 5, []Occupant{{ID: "a", StartKm: 0, EndKm: 5}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shares := SplitFare(tc.total, tc.routeKm, tc.occupants, "a")
			if got := sum(shares); got != tc.total {
				t.Fatalf("las partes suman %d, esperaba %d (%v)", got, tc.total, shares)
			}
			for id, v := range shares {
				if v < 0 {
					t.Errorf("%s tiene una parte negativa: %d", id, v)
				}
			}
		})
	}
}

func TestSplitFareCargaAlAnfitrionLosTramosVacios(t *testing.T) {
	// Nadie viaja en los primeros 5 km: ese coste no se puede perder.
	shares := SplitFare(1000, 10, []Occupant{
		{ID: "bruno", StartKm: 5, EndKm: 10},
	}, "ana")

	if shares["ana"] != 500 || shares["bruno"] != 500 {
		t.Fatalf("reparto = %v, esperaba que Ana asumiera el tramo vacío", shares)
	}
	if sum(shares) != 1000 {
		t.Fatalf("las partes suman %d, esperaba 1000", sum(shares))
	}
}

func TestSplitFareCuentaLasPlazasOcupadas(t *testing.T) {
	// Bruno viaja acompañado: ocupa dos plazas y paga por dos.
	shares := SplitFare(900, 10, []Occupant{
		{ID: "ana", StartKm: 0, EndKm: 10, Seats: 1},
		{ID: "bruno", StartKm: 0, EndKm: 10, Seats: 2},
	}, "ana")

	if shares["ana"] != 300 || shares["bruno"] != 600 {
		t.Fatalf("reparto = %v, esperaba 300/600", shares)
	}
}

func TestEstimateSeatPriceEsLoQuePagariaAlSumarse(t *testing.T) {
	current := []Occupant{{ID: "ana", StartKm: 0, EndKm: 10}}
	got := EstimateSeatPrice(1000, 10, current,
		Occupant{ID: "nuevo", StartKm: 5, EndKm: 10}, "ana")

	if got != 250 {
		t.Fatalf("precio estimado = %d, esperaba 250", got)
	}
}

func TestSplitFareSinOcupantesNoPierdeDinero(t *testing.T) {
	shares := SplitFare(1000, 10, nil, "ana")
	if shares["ana"] != 1000 {
		t.Fatalf("reparto = %v, esperaba todo a Ana", shares)
	}
}

// --- La invariante de la que depende la legalidad ---

func TestElRepartoNormalNuncaDaBeneficio(t *testing.T) {
	// Cualquier reparto que salga de SplitFare tiene que pasar la comprobación:
	// es lo que mantiene el servicio dentro de la excepción de gastos
	// compartidos y fuera de la regulación del transporte comercial.
	casos := []struct {
		name      string
		total     int64
		routeKm   float64
		occupants []Occupant
	}{
		{"todo el camino a medias", 1000, 10, []Occupant{
			{ID: "host", StartKm: 0, EndKm: 10},
			{ID: "b", StartKm: 0, EndKm: 10},
		}},
		{"tres pasajeros en tramos distintos", 4737, 23.4, []Occupant{
			{ID: "host", StartKm: 0, EndKm: 23.4},
			{ID: "b", StartKm: 1.7, EndKm: 19.2},
			{ID: "c", StartKm: 8.1, EndKm: 23.4},
			{ID: "d", StartKm: 12.5, EndKm: 14.9},
		}},
		{"el coche lleno todo el trayecto", 999, 5, []Occupant{
			{ID: "host", StartKm: 0, EndKm: 5},
			{ID: "b", StartKm: 0, EndKm: 5, Seats: 3},
		}},
	}
	for _, tc := range casos {
		t.Run(tc.name, func(t *testing.T) {
			shares := SplitFare(tc.total, tc.routeKm, tc.occupants, "host")
			if err := VerificarSinLucro(tc.total, shares, "host"); err != nil {
				t.Fatalf("%v (reparto: %v)", err, shares)
			}
		})
	}
}

func TestVerificarSinLucroDetectaElBeneficio(t *testing.T) {
	casos := map[string]struct {
		total  int64
		shares map[string]int64
	}{
		"los pasajeros pagan más que el coste": {
			1000, map[string]int64{"host": 0, "b": 700, "c": 700},
		},
		"quien organiza cobra": {
			1000, map[string]int64{"host": -200, "b": 1200},
		},
		"una parte negativa": {
			1000, map[string]int64{"host": 1100, "b": -100},
		},
	}
	for name, tc := range casos {
		if err := VerificarSinLucro(tc.total, tc.shares, "host"); !errors.Is(err, ErrLucro) {
			t.Errorf("%s: error = %v, esperaba ErrLucro", name, err)
		}
	}
}

func TestQuienOrganizaPuedeAcabarPagandoCero(t *testing.T) {
	// El caso límite legítimo: el coche va lleno y los demás cubren el coste
	// entero. Quien organiza viaja gratis, pero no gana nada, así que sigue
	// siendo gasto compartido.
	if err := VerificarSinLucro(1000, map[string]int64{"host": 0, "b": 1000}, "host"); err != nil {
		t.Fatalf("viajar gratis sin ganar nada es legítimo: %v", err)
	}
}
