# Cybercab Go Share

Compartir un robotaxi en Austin, Texas. Alguien que ya va del centro al
aeropuerto abre las plazas libres de su Cybercab; otra persona que hace ese
mismo camino —entero o solo un tramo— se sube, y ambas pagan menos.

La idea de fondo es la del coche compartido de siempre. Lo que cambia el diseño
por completo es que **aquí no hay conductor**.

## Por qué la seguridad se diseña distinto

En un viaje compartido convencional, quien conduce hace de testigo y de
autoridad dentro del coche: hay alguien al mando, alguien que responde, alguien
que puede parar. En un robotaxi no hay nadie. Y en un **Cybercab, que es
biplaza**, el viaje es un cara a cara: dos desconocidos solos en un vehículo que
ninguno controla.

Ese mecanismo de seguridad que otros servicios tienen gratis, aquí hay que
construirlo. De ahí sale la regla central del producto:

| Vehículo | Plazas | Nivel exigido | Por qué |
|---|---|---|---|
| **Cybercab** | 2 | **Identidad verificada** | Cara a cara sin testigos |
| **Model Y** | 4 | Básico (email + teléfono) | Hay más gente a bordo |

Quien organiza puede **subir** el listón. Nunca rebajarlo. Y el requisito corre
en las dos direcciones: quien publica el viaje tiene que cumplir lo que él mismo
exige, para que nadie sin verificar pueda pedir identidad acreditada a los
demás.

## Verificación de identidad

Cinco comprobaciones, de las que se derivan cuatro niveles:

| Nivel | Qué hace falta |
|---|---|
| **nuevo** | Nada acreditado |
| **básico** | Correo + teléfono |
| **verificado** | + documento oficial **y** selfie con prueba de vida |
| **veterano** | Verificado + 5 viajes y 3 valoraciones ≥ 4,5 |

Tres decisiones que no son de detalle:

**Documento *y* selfie, siempre juntos.** Un documento por sí solo demuestra que
el documento existe, no que quien lo enseña sea su titular. Sin comparación
facial con prueba de vida, cualquiera podría acreditarse con el carné de otra
persona.

**El nivel se calcula, no se guarda.** Cuando caduca el documento que lo
sostiene, el nivel baja solo. Nadie tiene que pasar por la base de datos
marcando documentos vencidos.

**Nunca se guarda el documento ni la fotografía.** Solo la referencia del
proveedor externo y el veredicto. Una filtración de esta base de datos no expone
el documento de identidad de nadie. El proveedor (Stripe Identity, Onfido,
Persona…) está detrás de una interfaz para no atarse a ninguno.

Cuando alguien no alcanza el nivel, el rechazo dice **qué le falta y por qué**,
no solo que no.

### Otras defensas

- **Bloqueos que cortan en ambos sentidos.** Quien bloquea deja de ver a la otra
  persona, y quien es bloqueado tampoco puede buscarla ni reservar.
- **Un rechazo resuelto no se reabre.** Reenviar un webhook no convierte un
  "no" en un "sí".
- **Una sola comprobación viva por tipo.** Un rechazo no se puede enterrar bajo
  intentos repetidos.
- **La confianza se comprueba antes que las plazas**, así que quien no puede
  subirse no llega ni a retener un asiento.
- **El perfil público** dice qué se ha acreditado, nunca el dato acreditado ni
  el contacto de la persona.

## La integración con Tesla: lo que hoy se puede y lo que no

**Tesla no publica ninguna API para que una app de terceros pida un robotaxi.**
La *Fleet API* existe, pero sirve para que el dueño de un coche controle su
propio coche —telemetría, comandos, carga—, no para llamar a un vehículo del
servicio. Los viajes de Austin se piden desde la app de Tesla y punto.

Así que el modelo que funciona hoy es el **traspaso**: esta app empareja a la
gente, calcula el reparto y coordina el encuentro; quien organiza pide el coche
en la app de Tesla y registra aquí la referencia del viaje. Cuando termina,
introduce el importe real y el reparto se cuadra sobre lo que de verdad se
cobró.

Todo eso vive detrás de la interfaz `fleet.Provider`. El día que exista una API
real, se implementa otro proveedor y no cambia nada más. Y mientras tanto, el
proveedor de traspaso **falla explícitamente** si se le pide un coche, en vez de
devolver un viaje inventado que haría creer a la interfaz que hay un vehículo en
camino.

**Los términos de Tesla sí permiten compartir el viaje**, con dos condiciones
que el producto ya cumple: quien pide el coche tiene que ir en él durante todo
el trayecto, y responde de la conducta de quien deja subir. Esa segunda
condición está implementada: no se puede aceptar a nadie sin asumirla
explícitamente.

Ver [`docs/viabilidad-legal.md`](docs/viabilidad-legal.md) para el análisis
completo, incluidas las fuentes.

## Cómo se reparte el coste

La ruta se corta en rodajas por cada subida y cada bajada. El coste de cada
rodaja se divide entre las plazas ocupadas **en ese tramo**. Quien solo hace la
mitad del camino paga la mitad del camino, y solo la comparte con quien iba a
bordo en ese momento.

Ejemplo real (10,8 km, coste estimado 10,88 $):

| Persona | Tramo | Paga |
|---|---|---|
| Ana (organiza) | km 0 → 10,8 | 7,51 $ |
| Carla | km 4,1 → 10,8 | 3,37 $ |

Ana iba a pagar 10,88 $ sola. Los primeros 4,1 km los asume entera porque va
sola; a partir de ahí, a medias. El reparto **siempre suma el total exacto** al
céntimo (método del mayor resto), sin dinero perdido en el redondeo.

### Quien organiza nunca gana dinero, y de eso depende la legalidad

La ley de Texas excluye de la regulación del transporte comercial los acuerdos
de gastos compartidos y aquellos en los que *"la cantidad recibida no excede el
coste de proporcionar el viaje"*. Mientras nadie obtenga beneficio, este
servicio no es una TNC y no necesita permiso estatal.

Es decir: **la legalidad del producto depende de una propiedad del algoritmo de
reparto**. Por eso no se deja al azar — `pricing.VerificarSinLucro` la comprueba
en cada desglose y falla antes de enseñar un reparto que la rompiera.

## Cómo se decide que un viaje "queda de camino"

La ruta la traza un motor de rutas real (OSRM), así que la polilínea que se
guarda es la que el coche va a seguir por la calle. Cada punto se proyecta sobre
ella y salen dos números:

- **`AlongKm`** — en qué kilómetro de la ruta engancha. Ordena las paradas, y
  así se descarta pedir un viaje en sentido contrario: la bajada tiene que ir
  por detrás de la recogida.
- **`OffRouteKm`** — cuánto se separa de la ruta, es decir, lo que hay que
  caminar o desviarse.

Si OSRM falla, se cae a una línea recta marcada como tal (`route_source`), para
no confundir una estimación con una ruta real. Pero un destino sin ruta posible
devuelve error en vez de inventarse una línea recta.

## Arrancar

```bash
make demo      # API en :8080 con un trayecto de ejemplo Austin → AUS
make test      # pruebas con detector de carreras
make test-pg   # incluye las pruebas contra Postgres
make cover     # informe de cobertura
make lint      # gofmt + go vet
```

## API

### Acceso
| Método | Ruta | Qué hace |
|---|---|---|
| `POST` | `/api/v1/auth/register` | Alta con contraseña |
| `POST` | `/api/v1/auth/login` | Inicio de sesión |
| `GET` | `/api/v1/me` | Quién soy |

### Confianza
| Método | Ruta | Qué hace |
|---|---|---|
| `POST` | `/api/v1/me/verificaciones` | Abrir una comprobación |
| `GET` | `/api/v1/me/verificaciones` | Mis comprobaciones (privado) |
| `POST` | `/api/v1/me/verificaciones/{ref}/refrescar` | Consultar el veredicto |
| `GET` | `/api/v1/users/{id}/confianza` | Perfil público de confianza |
| `POST` | `/api/v1/users/{id}/bloquear` | Bloquear a alguien |
| `POST` | `/api/v1/users/{id}/desbloquear` | Retirar el bloqueo |

### Trayectos y reservas
| Método | Ruta | Qué hace |
|---|---|---|
| `POST` | `/api/v1/trips` | Publicar un trayecto |
| `GET` | `/api/v1/trips` | Trayectos abiertos |
| `GET` | `/api/v1/trips/{id}` | Detalle |
| `POST` | `/api/v1/trips/{id}/cancel` | Anular |
| `GET` | `/api/v1/trips/{id}/fare` | Desglose del reparto |
| `POST` | `/api/v1/trips/{id}/bookings` | Pedir plaza en un tramo |
| `POST` | `/api/v1/bookings/{id}/decision` | Aceptar o rechazar |
| `POST` | `/api/v1/bookings/{id}/cancel` | Anular una reserva |
| `POST` | `/api/v1/search` | Buscar trayectos compatibles |

Códigos: `400` JSON o coordenadas inválidas · `401` sin token · `403` sin
permiso, bloqueado o sin nivel de confianza · `404` no existe · `409` sin plazas
o email en uso · `422` regla de negocio incumplida.

La identidad sale siempre del token verificado, nunca del cuerpo de la petición.

## Estructura

```
cmd/server/          arranque, configuración y datos de demo
internal/geo/        distancias y proyección de puntos sobre la ruta
internal/domain/     usuarios, trayectos, reservas y sus reglas
internal/auth/       contraseñas, tokens de sesión y middleware
internal/trust/      verificación de identidad y niveles de confianza
internal/pricing/    tarifa estimada y reparto del coste por tramos
internal/routing/    rutas reales por carretera (OSRM) con respaldo
internal/matching/   qué trayectos encajan con una búsqueda, y en qué orden
internal/fleet/      frontera con la flota de robotaxis
internal/billing/    libro de apuntes, compensación y liquidación
internal/store/      persistencia (memoria o Postgres+PostGIS)
internal/service/    lógica de negocio
internal/api/        capa HTTP
```

Las capas van de dentro hacia fuera: `geo`, `domain` y `trust` no conocen a
nadie; `api` no contiene reglas de negocio. Cambiar de almacén, de motor de
rutas, de proveedor de identidad o de flota es implementar una interfaz.

## Configuración

| Variable | Por defecto | Para qué |
|---|---|---|
| `PORT` | `8080` | Puerto de escucha |
| `ENV` | — | `production` exige `AUTH_SECRET` y `DATABASE_URL` |
| `DATABASE_URL` | — | Postgres. Sin él, memoria (se pierde al reiniciar) |
| `AUTH_SECRET` | — | Secreto de firma de sesiones (mín. 32 bytes) |
| `AUTH_TOKEN_TTL_HOURS` | `24` | Duración de la sesión |
| `OSRM_URL` | — | Motor de rutas. Sin él, línea recta |
| `SEED_DEMO` | — | `1` carga el ejemplo de Austin |
| `FARE_BASE_CENTS` | `200` | Banderada |
| `FARE_PER_KM_CENTS` | `62` | Coste por kilómetro (≈ 1,00 $/milla) |
| `FARE_PER_MINUTE_CENTS` | `15` | Coste por minuto |
| `FARE_MINIMUM_CENTS` | `500` | Importe mínimo del viaje |
| `AVG_SPEED_KMH` | `45` | Velocidad media de respaldo |

> Las tarifas son una **estimación de mercado**, no precios oficiales de Tesla.

## Cómo se cobra

No viaje a viaje. Cobrar cada trayecto por separado exige un cargo al pasajero y
un pago a quien organiza, y entre las dos comisiones fijas del procesador se
llevan **el 100 %** de la comisión de servicio: el ingreso neto sería cero.

En su lugar, completar un viaje solo **anota** en el libro lo que cada cual debe.
Una vez por periodo se liquida: se compensan los saldos —quien comparte a diario
alterna entre organizar y viajar, así que buena parte del dinero se cancela
sola— y se produce **una sola instrucción de cobro o pago por persona**. Sobre
40 viajes al mes eso pasa de 0,00 $ netos a 21,56 $.

Dos propiedades que el código garantiza:

- **Lo que se cobra menos lo que se paga es exactamente nuestra comisión.** Si
  esa igualdad se rompiera, estaríamos perdiendo dinero de alguien o
  inventándolo. Se comprueba antes de devolver la liquidación, no después de
  ejecutarla.
- **No hay monedero.** Lo que se guarda son cuentas pendientes, no fondos
  custodiados. La app produce instrucciones para un procesador ya licenciado;
  nunca retiene dinero. Es lo que la mantiene fuera de la licencia de transmisor
  de dinero.

Ver [`docs/modelo-de-negocio.md`](docs/modelo-de-negocio.md).

## Lo que falta

1. **Proveedor de identidad real.** El que hay es manual y no verifica nada:
   solo sirve para desarrollo. Sin uno real, nadie llega a verificado y ningún
   Cybercab admite pasajeros — que es el fallo seguro correcto.
2. **Valoraciones y denuncias.** El nivel veterano ya las cuenta, pero todavía
   no hay forma de emitirlas.
3. **Pagos.** Cobrar el reparto y liquidarlo con quien organiza.
4. **Compartir el viaje en tiempo real** con un contacto de confianza, y botón
   de emergencia. En un coche sin conductor pesa más que en uno con conductor.
5. **Interfaz de usuario.**
