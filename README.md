# Cybercab Go Share

Compartir un robotaxi en Austin, Texas. Alguien que ya va del centro al
aeropuerto abre las plazas libres de su Cybercab; otra persona que hace ese
mismo camino —entero o solo un tramo— se sube y ambas pagan menos.

La idea es la de BlaBlaCar, con dos diferencias que cambian el diseño: aquí
**nadie conduce** (el vehículo es autónomo, así que quien organiza es un
pasajero más) y el trayecto se comparte **por tramos**, no de punta a punta.

## Estado

MVP funcional del backend: API REST en Go, sin dependencias externas, con el
almacén en memoria. Cubre el ciclo completo — publicar, buscar, reservar,
confirmar y repartir el coste — con pruebas.

## Una nota sobre el Cybercab

El Cybercab de producción es **biplaza**. Compartirlo significa exactamente
una plaza libre. Los robotaxis que Tesla opera hoy en Austin son Model Y, con
más sitio. Por eso el tipo de vehículo es un dato del trayecto y el aforo sale
de él (`cybercab` → 2, `model_y` → 4): la app funciona con la flota de hoy y
con la de mañana sin tocar la lógica.

## Cómo se reparte el coste

Es la pieza central del producto, y no es un simple "dividir entre dos".

La ruta se corta en rodajas por cada subida y cada bajada. El coste de cada
rodaja se divide entre las plazas que van ocupadas **en ese tramo**. Quien
solo hace la mitad del camino paga la mitad del camino, y solo la comparte con
quien iba a bordo en ese momento.

Ejemplo real (trayecto de 10,8 km, coste estimado 10,88 $):

| Persona | Tramo | Paga |
|---|---|---|
| Ana (organiza) | km 0 → 10,8 | 7,51 $ |
| Carla | km 4,1 → 10,8 | 3,37 $ |

Ana iba a pagar 10,88 $ sola. Los primeros 4,1 km los asume entera porque va
sola; a partir de ahí, a medias. El reparto **siempre suma el total exacto**
al céntimo (método del mayor resto), sin dinero perdido en el redondeo.

## Cómo se decide que un viaje "queda de camino"

Sin motor de rutas externo: la ruta es una polilínea (origen, waypoints,
destino) y cada punto se proyecta sobre ella. De ahí salen dos números:

- **`AlongKm`** — en qué kilómetro de la ruta engancha. Ordena las paradas
  entre sí, y así se descarta pedir un viaje en sentido contrario: la bajada
  tiene que ir por detrás de la recogida.
- **`OffRouteKm`** — cuánto se separa de la ruta, es decir, lo que hay que
  caminar o desviarse. Si supera el máximo aceptado, el trayecto no encaja.

Los resultados se ordenan por una puntuación que premia cubrir el viaje pedido
(75 %) y penaliza el paseo hasta el punto de recogida (25 %).

## Arrancar

```bash
make demo      # API en :8080 con un trayecto de ejemplo Austin → AUS
make test      # pruebas con detector de carreras
make cover     # informe de cobertura
make lint      # gofmt + go vet
```

Sin dependencias: solo la biblioteca estándar de Go 1.24.

## Recorrido de ejemplo

```bash
# 1. Dos usuarios
ANA=$(curl -s localhost:8080/api/v1/users -d '{"name":"Ana","email":"ana@example.com"}' | jq -r .id)
CARLA=$(curl -s localhost:8080/api/v1/users -d '{"name":"Carla","email":"carla@example.com"}' | jq -r .id)

# 2. Ana publica su viaje al aeropuerto
TRIP=$(curl -s localhost:8080/api/v1/trips -d "{
  \"host_id\": \"$ANA\",
  \"origin\":      {\"name\":\"Centro\",\"point\":{\"lat\":30.2685,\"lng\":-97.7425}},
  \"destination\": {\"name\":\"AUS\",\"point\":{\"lat\":30.1975,\"lng\":-97.6664}},
  \"departure_time\": \"2026-09-08T15:00:00Z\",
  \"vehicle\": \"model_y\",
  \"max_detour_km\": 2
}" | jq -r .id)

# 3. Carla busca sitio desde Riverside
curl -s localhost:8080/api/v1/search -d '{
  "pickup":  {"lat":30.2380,"lng":-97.7180},
  "dropoff": {"lat":30.1975,"lng":-97.6664}
}' | jq '.matches[] | {shared_km, estimated_price_cents, score}'

# 4. Pide plaza
BKG=$(curl -s localhost:8080/api/v1/trips/$TRIP/bookings -d "{
  \"passenger_id\": \"$CARLA\",
  \"pickup\":  {\"name\":\"Riverside\",\"point\":{\"lat\":30.2380,\"lng\":-97.7180}},
  \"dropoff\": {\"name\":\"AUS\",\"point\":{\"lat\":30.1975,\"lng\":-97.6664}}
}" | jq -r .id)

# 5. Ana acepta
curl -s localhost:8080/api/v1/bookings/$BKG/decision -d "{\"actor_id\":\"$ANA\",\"accept\":true}"

# 6. Reparto final
curl -s localhost:8080/api/v1/trips/$TRIP/fare | jq
```

## API

| Método | Ruta | Qué hace |
|---|---|---|
| `GET` | `/healthz` | Comprobación de vida |
| `POST` | `/api/v1/users` | Alta de usuario |
| `GET` | `/api/v1/users/{id}` | Ficha de usuario |
| `GET` | `/api/v1/users/{id}/bookings` | Reservas de un pasajero |
| `POST` | `/api/v1/trips` | Publicar un trayecto compartido |
| `GET` | `/api/v1/trips` | Trayectos abiertos |
| `GET` | `/api/v1/trips/{id}` | Detalle de un trayecto |
| `POST` | `/api/v1/trips/{id}/cancel` | Anular (solo quien organiza) |
| `GET` | `/api/v1/trips/{id}/fare` | Desglose del reparto del coste |
| `GET` | `/api/v1/trips/{id}/bookings` | Reservas del trayecto |
| `POST` | `/api/v1/trips/{id}/bookings` | Pedir plaza en un tramo |
| `POST` | `/api/v1/bookings/{id}/decision` | Aceptar o rechazar |
| `POST` | `/api/v1/bookings/{id}/cancel` | Anular una reserva |
| `POST` | `/api/v1/search` | Buscar trayectos compatibles |

Códigos de error: `400` JSON o coordenadas no válidas · `404` no existe ·
`409` sin plazas · `422` regla de negocio incumplida.

## Estructura

```
cmd/server/          arranque, configuración por entorno, datos de demo
internal/geo/        distancias y proyección de puntos sobre la ruta
internal/domain/     usuarios, trayectos, reservas y sus reglas
internal/pricing/    tarifa estimada y reparto del coste por tramos
internal/matching/   qué trayectos encajan con una búsqueda, y en qué orden
internal/store/      persistencia (hoy en memoria, tras una interfaz)
internal/service/    lógica de negocio
internal/api/        capa HTTP
```

Las capas van de dentro hacia fuera: `geo` y `domain` no conocen a nadie,
`api` no contiene reglas de negocio. Cambiar el almacén en memoria por
Postgres es implementar `store.Store` y nada más.

## Configuración

| Variable | Por defecto | Para qué |
|---|---|---|
| `PORT` | `8080` | Puerto de escucha |
| `SEED_DEMO` | — | `1` carga el ejemplo de Austin |
| `FARE_BASE_CENTS` | `200` | Banderada |
| `FARE_PER_KM_CENTS` | `62` | Coste por kilómetro (≈ 1,00 $/milla) |
| `FARE_PER_MINUTE_CENTS` | `15` | Coste por minuto |
| `FARE_MINIMUM_CENTS` | `500` | Importe mínimo del viaje |
| `AVG_SPEED_KMH` | `45` | Velocidad media para estimar duraciones |

> Las tarifas son una **estimación de mercado**, no precios oficiales de
> Tesla. Por eso se configuran desde fuera.

## Lo que falta para llevarlo a producción

Por orden de importancia:

1. **Persistencia real** — Postgres con PostGIS, que además resuelve la
   búsqueda geográfica a escala.
2. **Rutas reales** — hoy la ruta es una polilínea recta y la duración una
   estimación por velocidad media. Con un motor de rutas, las distancias y los
   tiempos pasan a ser los de la calle.
3. **Autenticación** — ahora mismo el `actor_id` viaja en el cuerpo de la
   petición y nadie lo verifica. Hace falta identidad real antes de exponer
   nada.
4. **Pagos** — cobrar el reparto y liquidarlo con quien organiza.
5. **Integración con Tesla** — reservar el vehículo desde la app y leer su
   posición en tiempo real.
6. **Confianza y seguridad** — verificación de identidad, valoraciones y
   denuncias. En un coche sin conductor, con quién compartes importa más.
