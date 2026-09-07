# Cuánta más demanda sirve la misma flota si se comparte

Esta es la cifra que le importa a Tesla, no a nosotros. Con una flota pequeña
para un área metropolitana entera, la ocupación por vehículo es la palanca más
fuerte sobre la rentabilidad del robotaxi.

Reproducible con `make simulacion`. La simulación corre sobre **el motor de
emparejamiento del propio producto**, no sobre una aproximación: si el
emparejador rechaza un viaje aquí, lo rechazaría también en producción.

---

## Resultado

1 000 viajes en un día laborable en Austin, con parámetros conservadores
(±10 min de flexibilidad horaria, 1 km de paseo máximo):

| | Cybercab (2 plazas) | Model Y (4 plazas) |
|---|---|---|
| Viajes de vehículo necesarios | 1 000 → **736** | 1 000 → **670** |
| Kilómetros recorridos por la flota | −20,8 % | −25,7 % |
| Ocupación (pasajero-km / vehículo-km) | 1,00 → **1,26** | 1,00 → **1,35** |
| Viajes que llegan a compartirse | 26,4 % | 33,0 % |
| **La misma flota sirve** | **1,26×** más demanda | **1,35×** más demanda |
| Ahorro diario para los pasajeros | 1 929 $ | 2 392 $ |

Traducido: **con 20 vehículos se atiende la demanda que hoy necesitaría 27.**
Sin comprar un coche más.

## El argumento que de verdad importa: el efecto crece con la escala

| Viajes/día | La misma flota sirve | Se comparte |
|---|---|---|
| 200 | 1,10× | 14,5 % |
| 500 | 1,26× | 26,8 % |
| 1 000 | 1,45× | 39,4 % |
| 2 000 | 1,72× | 49,8 % |
| **5 000** | **2,24×** | **62,4 %** |

*(Model Y, ±15 min de flexibilidad)*

**Cuanta más flota y más demanda, más vale compartir.** Hoy, con 20 coches, es
cuando menos aporta. El día que Tesla tenga miles de vehículos en Austin, esta
capa duplica la capacidad efectiva de la flota.

Eso reencuadra el riesgo del proyecto: la relación con Tesla no es solo la
mayor amenaza, es también la mayor oportunidad, y la forma de convertir una en
otra es esta cifra.

## Y el usuario también gana flexibilidad

| Flexibilidad horaria | La misma flota sirve | Se comparte |
|---|---|---|
| ±5 min | 1,21× | 22,0 % |
| ±10 min | 1,35× | 33,0 % |
| ±15 min | 1,45× | 39,4 % |
| ±30 min | 1,71× | 50,3 % |

Media hora de margen casi duplica el efecto. Es la palanca de producto más
barata que existe: no requiere más coches ni más usuarios, solo pedir a la
gente que diga cuándo le viene bien en vez de cuándo quiere salir exactamente.

## Por qué el Cybercab rinde menos que el Model Y

El Cybercab admite un solo acompañante, así que su techo está físicamente más
bajo: 1,26× frente a 1,35×. No es un defecto del emparejador, es el aforo.

Merece la pena tenerlo presente en las dos direcciones. Para el producto,
significa que **la flota mixta rinde más que una flota solo de Cybercabs**. Para
la seguridad, ya sabíamos que el biplaza es la configuración más delicada —cara
a cara sin testigos— y ahora sabemos que además es la que menos ocupación
aporta.

---

## Supuestos, para poder discutirlos

Ninguna de estas cifras es una predicción. Son el mismo día simulado dos veces,
con y sin compartir, y lo que vale es la comparación.

- **Rutas en línea recta**, no por calle. Afecta a los valores absolutos, no a
  la comparación: los dos escenarios usan la misma medida.
- **Los pesos por zona** son una estimación razonada de los patrones de Austin
  (centro, aeropuerto, Domain, Riverside, campus, Mueller, South Congress,
  Pflugerville, Gigafactory, Zilker), no datos medidos.
- **El emparejamiento es el del producto**, con sus reglas reales: sentido de la
  marcha, distancia a la ruta, plazas y ventana horaria.
- **No se modela el reposicionamiento en vacío** entre viajes. En la realidad
  eso hace a compartir todavía más rentable, así que el número real sería mejor.
- **Los parámetros son conservadores.** Son un suelo, no un techo.
- La demanda se genera con semilla fija: cualquiera puede reproducir el
  resultado exacto y discutirlo.

## Cómo reproducirlo

```bash
make simulacion                                    # el informe de arriba
go run ./cmd/simulacion -peticiones=5000 -ventana=15
go run ./cmd/simulacion -json                      # para tratarlo con otras herramientas
```
