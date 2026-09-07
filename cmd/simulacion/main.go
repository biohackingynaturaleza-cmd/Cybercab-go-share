// Command simulacion mide cuánta más demanda sirve la misma flota de robotaxis
// cuando los viajes se comparten.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pricing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/simulacion"
)

func main() {
	peticiones := flag.Int("peticiones", 1000, "peticiones de viaje al día")
	ventana := flag.Int("ventana", 10, "flexibilidad horaria en minutos")
	caminar := flag.Float64("caminar", 1.0, "cuánto se acepta caminar, en km")
	semilla := flag.Int64("semilla", 20260907, "semilla, para poder reproducir el resultado")
	comoJSON := flag.Bool("json", false, "salida en JSON")
	flag.Parse()

	dia := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	demanda := simulacion.GenerarDemanda(*peticiones, dia, *semilla)
	tarifa := pricing.DefaultTariff()

	var resultados []*simulacion.Resultado
	for _, v := range []domain.VehicleType{domain.VehicleCybercab, domain.VehicleModelY} {
		cfg := simulacion.Config{Vehiculo: v, VentanaMin: *ventana, MaxCaminarKm: *caminar}
		resultados = append(resultados, simulacion.Simular(demanda, cfg, tarifa))
	}

	if *comoJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resultados)
		return
	}
	informe(resultados, *ventana, *caminar, *semilla)
}

func informe(rs []*simulacion.Resultado, ventana int, caminar float64, semilla int64) {
	fmt.Printf("\n  OCUPACIÓN DE LA FLOTA CON Y SIN COMPARTIR\n")
	fmt.Printf("  %s\n\n", linea(74))
	fmt.Printf("  Demanda simulada: %d viajes en un día laborable en Austin\n", rs[0].Peticiones)
	fmt.Printf("  Flexibilidad horaria: ±%d min   ·   Se acepta caminar: %.1f km\n", ventana, caminar)
	fmt.Printf("  Semilla: %d (reproducible)\n\n", semilla)

	for _, r := range rs {
		nombre := "Cybercab (2 plazas)"
		if r.Vehiculo == domain.VehicleModelY {
			nombre = "Model Y (4 plazas)"
		}
		fmt.Printf("  %s\n", nombre)
		fmt.Printf("  %s\n", linea(74))
		fmt.Printf("    Viajes de vehículo necesarios     %6d  ->  %6d   (%+.1f %%)\n",
			r.ViajesSinCompartir, r.ViajesCompartiendo,
			-100*(1-float64(r.ViajesCompartiendo)/float64(r.ViajesSinCompartir)))
		fmt.Printf("    Kilómetros recorridos por la flota %6.0f  ->  %6.0f   (%+.1f %%)\n",
			r.KmVehiculoSinCompartir, r.KmVehiculoCompartiendo, -100*r.ReduccionKm)
		fmt.Printf("    Ocupación (pasajero-km / vehículo-km) %5.2f  ->  %5.2f\n",
			r.OcupacionSinCompartir, r.OcupacionCompartiendo)
		fmt.Printf("    Viajes que llegan a compartirse   %6.1f %%\n", 100*r.TasaEmparejamiento)
		fmt.Printf("\n    LA MISMA FLOTA SIRVE  %.2f veces más demanda\n", r.FactorDeCapacidad)
		fmt.Printf("    Los pasajeros se ahorran  %.2f $ al día\n\n",
			float64(r.AhorroPasajerosCents)/100)
	}

	fmt.Printf("  %s\n", linea(74))
	fmt.Printf("  Supuestos, para poder discutirlos:\n")
	fmt.Printf("   · Rutas en línea recta, no por calle. Afecta a los valores absolutos,\n")
	fmt.Printf("     no a la comparación: ambos escenarios usan la misma medida.\n")
	fmt.Printf("   · Los pesos por zona son una estimación razonada de los patrones de\n")
	fmt.Printf("     Austin, no datos medidos.\n")
	fmt.Printf("   · El emparejamiento es el del propio producto, no una aproximación.\n")
	fmt.Printf("   · No se modela el reposicionamiento en vacío entre viajes, que en la\n")
	fmt.Printf("     realidad hace a compartir aún más rentable.\n")
	fmt.Printf("   · Parámetros conservadores: con más flexibilidad horaria los números\n")
	fmt.Printf("     mejoran. Son un suelo, no un techo.\n\n")
}

func linea(n int) string {
	s := make([]byte, n)
	for i := range s {
		s[i] = '-'
	}
	return string(s)
}
