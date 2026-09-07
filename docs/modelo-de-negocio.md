# ¿De dónde salen los ingresos?

Análisis con la tarifa que ya usa la app y datos reales del servicio en Austin
(septiembre de 2026). La conclusión corta: **el valor que crea el producto es
grande y real; el problema es capturarlo, y la respuesta intuitiva —una comisión
por viaje— es la peor de las opciones.**

---

## 1. El punto de partida: quien organiza no puede ganar dinero

La excepción de gastos compartidos de Texas (ver
[`viabilidad-legal.md`](viabilidad-legal.md)) exige que **quien organiza** no
obtenga beneficio. No dice nada de la plataforma.

Así que la vía es la de BlaBlaCar: **el pasajero paga su parte del coste más una
comisión de servicio**. Quien organiza recibe solo su parte del coste, la
excepción sigue intacta, y la comisión es ingreso nuestro.

Legalmente funciona. Aritméticamente, no.

## 2. El valor que se crea es grande

Trayecto Centro de Austin → aeropuerto, con una pasajera que se sube a mitad de
camino:

| | Coste |
|---|---|
| Ana sola | 10,88 $ |
| Carla sola | 7,49 $ |
| **Por separado** | **18,37 $** |
| **Compartiendo** | **10,88 $** |
| **Valor creado** | **7,49 $** |

Cada viaje compartido elimina un trayecto entero. Eso es dinero real que antes
se gastaba y ahora no.

## 3. Pero la comisión por viaje no lo captura

| Concepto | Importe |
|---|---|
| Parte de Carla | 3,37 $ |
| Comisión 20 % | 0,67 $ |
| Comisión de Stripe (2,9 % + **0,30 $ fijos**) | −0,42 $ |
| **Neto** | **0,25 $** |

**Creamos 7,49 $ de valor y capturamos 0,25 $.** La comisión fija de 0,30 $ se
come el 63 % de nuestro ingreso, porque el ticket es diminuto.

Los trayectos largos sí funcionarían (Austin → Houston dejaría 12,49 $ netos),
**pero no son posibles**: el servicio está geovallado al área metropolitana de
Austin. Aeropuerto sí, otra ciudad no.

---

## 4. La palanca que nadie mira: no cobrar viaje a viaje

La fuga no está en la comisión, está en **cobrar 40 veces al mes en vez de una**.
Acumulando los viajes y cobrando periódicamente, con la misma comisión del 20 %:

Medido sobre la implementación real (40 viajes, comisión bruta de 26,80 $):

| Frecuencia de cobro | Coste del procesador | **Ingreso neto** |
|---|---|---|
| Por viaje | 26,80 $ | **0,00 $** |
| **Mensual, compensando saldos** | 5,24 $ | **21,56 $** |

**Cobrar viaje a viaje no deja nada.** Ni poco: cero. La primera estimación se
quedó corta porque solo contaba el cobro al pasajero; cada viaje exige *además*
un pago a quien organiza, con su propia comisión fija. Entre las dos se llevan
la comisión entera.

Agrupar no es una optimización. Es la diferencia entre tener ingresos y no
tenerlos.

> Consecuencia de diseño: hace falta acumular saldo pendiente por usuario y
> liquidarlo periódicamente. Sin monedero propio — el saldo es una cuenta
> pendiente, y el cobro lo ejecuta el procesador.

## 5. La suscripción es mejor producto, pero peor negocio

Un usuario que va y viene a diario (40 viajes/mes) se ahorra **149,80 $ al mes**.

| Modelo | Paga | Nos queda | % de su ahorro |
|---|---|---|---|
| Comisión 20 % agrupada | 26,80 $ | **21,81 $** | 17,9 % |
| Suscripción 9,99 $/mes | 9,99 $ | 9,40 $ | 6,7 % |

La suscripción hace que el usuario pague un **63 % menos**, y a nosotros nos deja
**menos de la mitad**. Es una herramienta de conversión y retención, no el motor
de ingresos. Solo compensa si sube mucho el uso o si sin ella la gente no entra.

---

## 6. El problema de verdad: el mercado todavía no existe

Tesla opera **unos 20 vehículos** para 288 millas cuadradas de área
metropolitana. Con una cuota optimista:

| Flota | Viajes compartidos/día | Ingreso/mes (comisión) |
|---|---|---|
| **20 (hoy)** | 100 | **750 $** |
| 200 | 1 000 | 7 500 $ |
| 2 000 | 10 000 | 75 000 $ |

**Hoy el mercado entero son 750 $ al mes.** Esto no es un negocio todavía: es una
apuesta a que la flota crezca dos órdenes de magnitud. Conviene decirlo en voz
alta antes de invertir, no después.

---

## 7. Por dónde saldrían los ingresos de verdad

Por orden de lo que yo haría:

### a) Cobro agrupado — hecho
Implementado en `internal/billing`. Sin él no hay ingresos en absoluto.

Además de agrupar, **compensa saldos**: quien comparte a diario alterna entre
organizar y viajar, así que la mayor parte del dinero se cancela sola y ni
siquiera llega a moverse. Cada movimiento evitado es una comisión que no se
paga.

### b) Empresas, no consumidores — aquí está el dinero
Las empresas de Austin ya subvencionan el desplazamiento de sus empleados, y el
aparcamiento en el centro es caro. Vender por empleado y mes:

- Ticket grande: una factura al mes, no 40 microcobros.
- Sin coste de adquisición de usuario: se vende una vez y entran cientos.
- Densidad instantánea: los empleados de una misma empresa **comparten origen o
  destino**, que es justo lo que el emparejamiento necesita para funcionar.

El último punto es el importante: una app de compartir coche fracasa por falta de
densidad. Una empresa te la regala.

### c) Cuota de protección — monetiza el riesgo que ya descubrimos
Los términos de Tesla hacen a quien organiza **responsable de la conducta de
quien deja subir**, daños incluidos. Es un riesgo real, nombrado y que el usuario
entiende sin que se lo expliquen.

Una cuota de 1–2 $ por viaje que cubra exactamente eso tendría una aceptación
alta, porque no se vende miedo: se cubre una obligación que el usuario ya tiene.
Requiere socio asegurador.

### d) Tesla — la jugada estratégica
**El mayor riesgo del proyecto y su mayor oportunidad son la misma relación.**

Con 20 vehículos para un área metropolitana entera, la ocupación por coche es la
palanca más fuerte sobre la rentabilidad del robotaxi. Cada viaje compartido es
un coche haciendo el trabajo de dos.

Si se demuestra con datos que esta app sube la ocupación, se pasa de ser algo que
Tesla puede cerrar a ser algo que a Tesla le interesa mantener. **Esa
demostración es el activo más valioso del proyecto**, por encima del código.

---

## 8. Lo que no hay que hacer

**Comisión suelta por viaje.** 0,25 $ netos no pagan ni el soporte.

**Vender los datos de confianza.** Sería contradecir el producto: la arquitectura
está hecha a propósito para no custodiar documentos, y monetizar la confianza
destruye la confianza, que es lo único que aquí se vende.

**Cobrar a quien organiza un porcentaje del trayecto.** Rompería la excepción de
gastos compartidos y convertiría el servicio en transporte comercial.

---

## Resumen

| | |
|---|---|
| Valor creado por viaje compartido | **7,49 $** |
| Capturado cobrando viaje a viaje | **0,00 $** |
| Capturado agrupando y compensando | 0,54 $ |
| Mercado total hoy (20 vehículos) | **~750 $/mes** |
| Dónde está el negocio | Empresas + protección + acuerdo con Tesla |
| Cuándo es un negocio | Cuando la flota pase de 20 a varios cientos |

**La pregunta no es cómo monetizar. Es si la flota va a crecer.** Si crece, la
posición de estar ya construido cuando eso pase vale mucho. Si no crece, ningún
modelo de ingresos salva el proyecto.
