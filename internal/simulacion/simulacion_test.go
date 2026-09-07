package simulacion

import (
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pricing"
)

var dia = time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)

func simular(t *testing.T, n int, v domain.VehicleType, ventana int) *Resultado {
	t.Helper()
	demanda := GenerarDemanda(n, dia, 42)
	cfg := PorDefecto(v)
	cfg.VentanaMin = ventana
	return Simular(demanda, cfg, pricing.DefaultTariff())
}

// --- Demanda ---

func TestLaDemandaEsReproducible(t *testing.T) {
	// Sin esto no se pueden discutir los resultados: cada ejecución daría otro
	// número y la conversación se vuelve imposible.
	a := GenerarDemanda(200, dia, 7)
	b := GenerarDemanda(200, dia, 7)

	if len(a) != len(b) {
		t.Fatalf("longitudes distintas: %d y %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Origen != b[i].Origen || !a[i].Salida.Equal(b[i].Salida) {
			t.Fatalf("la petición %d difiere entre ejecuciones", i)
		}
	}
}

func TestSemillasDistintasDanDemandasDistintas(t *testing.T) {
	a := GenerarDemanda(200, dia, 7)
	b := GenerarDemanda(200, dia, 8)

	iguales := 0
	for i := range a {
		if a[i].Origen == b[i].Origen {
			iguales++
		}
	}
	if iguales == len(a) {
		t.Fatal("dos semillas distintas producen la misma demanda")
	}
}

func TestNadiePideUnViajeAlSitioDondeYaEsta(t *testing.T) {
	for _, p := range GenerarDemanda(500, dia, 3) {
		if p.NombreOrigen == p.NombreDestino {
			t.Fatalf("petición %s va de %s a %s", p.ID, p.NombreOrigen, p.NombreDestino)
		}
		if p.DistanciaKm() <= 0 {
			t.Fatalf("petición %s no recorre distancia", p.ID)
		}
	}
}

func TestLaDemandaCaeDentroDelAreaDeServicio(t *testing.T) {
	// Si la dispersión sacara puntos fuera del área donde opera el robotaxi,
	// la simulación estaría midiendo viajes imposibles.
	centro := geo.Point{Lat: 30.30, Lng: -97.72}
	for _, p := range GenerarDemanda(500, dia, 5) {
		for _, punto := range []geo.Point{p.Origen, p.Destino} {
			if d := geo.DistanceKm(centro, punto); d > 35 {
				t.Fatalf("punto a %.1f km del centro: fuera del área de servicio", d)
			}
		}
	}
}

// --- Simulación ---

func TestCompartirNuncaAumentaLosKilometros(t *testing.T) {
	// La propiedad que no puede fallar: compartir no puede salir peor que no
	// compartir. Si saliera, el emparejador estaría metiendo viajes que no
	// ahorran nada.
	for _, v := range []domain.VehicleType{domain.VehicleCybercab, domain.VehicleModelY} {
		r := simular(t, 400, v, 10)
		if r.KmVehiculoCompartiendo > r.KmVehiculoSinCompartir {
			t.Errorf("%s: compartiendo se recorren %.0f km y sin compartir %.0f",
				v, r.KmVehiculoCompartiendo, r.KmVehiculoSinCompartir)
		}
		if r.ViajesCompartiendo > r.ViajesSinCompartir {
			t.Errorf("%s: compartiendo hacen falta más viajes", v)
		}
		if r.FactorDeCapacidad < 1 {
			t.Errorf("%s: factor de capacidad %.2f, no puede bajar de 1", v, r.FactorDeCapacidad)
		}
	}
}

func TestElTransporteEntregadoNoCambia(t *testing.T) {
	// La gente hace los mismos viajes en los dos escenarios. Lo que cambia es
	// lo que cuesta entregarlos, no lo que se entrega.
	r := simular(t, 300, domain.VehicleModelY, 10)

	if r.KmPasajero != r.KmVehiculoSinCompartir {
		t.Fatalf("km de pasajero = %.2f, sin compartir = %.2f: deberían coincidir",
			r.KmPasajero, r.KmVehiculoSinCompartir)
	}
	if r.OcupacionSinCompartir != 1 {
		t.Errorf("ocupación sin compartir = %.2f, esperaba exactamente 1", r.OcupacionSinCompartir)
	}
	if r.OcupacionCompartiendo <= 1 {
		t.Errorf("ocupación compartiendo = %.2f, esperaba más de 1", r.OcupacionCompartiendo)
	}
}

func TestCadaPeticionSeSirveDeUnaFormaODeOtra(t *testing.T) {
	// Ninguna petición puede perderse: o comparte, o sale un vehículo nuevo.
	r := simular(t, 400, domain.VehicleModelY, 10)
	if r.PeticionesEmparejadas+r.ViajesCompartiendo != r.Peticiones {
		t.Fatalf("emparejadas %d + viajes nuevos %d != peticiones %d",
			r.PeticionesEmparejadas, r.ViajesCompartiendo, r.Peticiones)
	}
}

func TestElVehiculoDeCuatroPlazasCompartesMasQueElBiplaza(t *testing.T) {
	// El Cybercab solo admite un acompañante, así que su techo está más bajo.
	// Es una limitación física, y la simulación tiene que reflejarla.
	cybercab := simular(t, 800, domain.VehicleCybercab, 15)
	modelY := simular(t, 800, domain.VehicleModelY, 15)

	if modelY.FactorDeCapacidad <= cybercab.FactorDeCapacidad {
		t.Fatalf("Model Y %.2fx y Cybercab %.2fx: con más plazas debería compartirse más",
			modelY.FactorDeCapacidad, cybercab.FactorDeCapacidad)
	}
}

func TestMasFlexibilidadHorariaMejoraElEmparejamiento(t *testing.T) {
	estrecha := simular(t, 800, domain.VehicleModelY, 5)
	amplia := simular(t, 800, domain.VehicleModelY, 30)

	if amplia.TasaEmparejamiento <= estrecha.TasaEmparejamiento {
		t.Fatalf("con ±30 min se comparte el %.1f%% y con ±5 min el %.1f%%",
			100*amplia.TasaEmparejamiento, 100*estrecha.TasaEmparejamiento)
	}
}

func TestElEfectoCreceConLaDensidadDeDemanda(t *testing.T) {
	// El argumento central para Tesla: cuanta más flota y más demanda, más
	// vale compartir. Hoy, con pocos coches, es cuando menos aporta.
	poca := simular(t, 200, domain.VehicleModelY, 15)
	mucha := simular(t, 2000, domain.VehicleModelY, 15)

	if mucha.FactorDeCapacidad <= poca.FactorDeCapacidad {
		t.Fatalf("con 2000 viajes el factor es %.2f y con 200 es %.2f: debería crecer",
			mucha.FactorDeCapacidad, poca.FactorDeCapacidad)
	}
}

func TestSinDemandaNoHayNadaQueCompartir(t *testing.T) {
	r := Simular(nil, PorDefecto(domain.VehicleModelY), pricing.DefaultTariff())
	if r.Peticiones != 0 || r.ViajesCompartiendo != 0 {
		t.Fatalf("resultado con demanda vacía: %+v", r)
	}
}

func TestUnaSolaPeticionNoSeCompartesConNadie(t *testing.T) {
	r := Simular(GenerarDemanda(1, dia, 1), PorDefecto(domain.VehicleModelY), pricing.DefaultTariff())
	if r.PeticionesEmparejadas != 0 {
		t.Fatalf("una petición sola no puede emparejarse con nada")
	}
	if r.FactorDeCapacidad != 1 {
		t.Fatalf("factor de capacidad = %.2f, esperaba 1", r.FactorDeCapacidad)
	}
}

func TestElAhorroDeLosPasajerosEsPositivoCuandoSeComparte(t *testing.T) {
	r := simular(t, 500, domain.VehicleModelY, 15)
	if r.PeticionesEmparejadas > 0 && r.AhorroPasajerosCents <= 0 {
		t.Fatalf("se comparten %d viajes y el ahorro es %d",
			r.PeticionesEmparejadas, r.AhorroPasajerosCents)
	}
}
