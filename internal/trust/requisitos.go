package trust

import "fmt"

// SueloPorVehiculo devuelve el nivel de confianza mínimo que exige compartir un
// vehículo, según cuánta gente vaya a bordo.
//
// La regla no es arbitraria. En un Cybercab biplaza el viaje es un cara a cara:
// dos personas solas, sin conductor y sin nadie más que pueda dar fe de lo que
// pasa. Es la configuración de mayor riesgo del servicio, y por eso exige que
// ambas partes tengan la identidad acreditada. Con cuatro plazas hay testigos,
// y el suelo puede bajar sin que el viaje deje de ser razonablemente seguro.
//
// Quien organiza puede subir el listón por encima de este suelo, nunca bajarlo.
func SueloPorVehiculo(plazasTotales int) Level {
	if plazasTotales <= 2 {
		return LevelVerificado
	}
	return LevelBasico
}

// Requisito es el nivel que un trayecto exige realmente: el mayor entre lo que
// pide quien organiza y el suelo del vehículo.
func Requisito(pedidoPorElHost Level, plazasTotales int) Level {
	suelo := SueloPorVehiculo(plazasTotales)
	if pedidoPorElHost > suelo {
		return pedidoPorElHost
	}
	return suelo
}

// ExplicarSuelo describe por qué un vehículo exige el nivel que exige, para
// poder mostrarlo en la interfaz en vez de que parezca un capricho.
func ExplicarSuelo(plazasTotales int) string {
	if plazasTotales <= 2 {
		return "Es un vehículo de dos plazas: viajaréis solos y sin conductor, " +
			"así que ambas partes necesitan la identidad verificada."
	}
	return fmt.Sprintf("Vehículo de %d plazas: se exige al menos teléfono y correo verificados.", plazasTotales)
}
