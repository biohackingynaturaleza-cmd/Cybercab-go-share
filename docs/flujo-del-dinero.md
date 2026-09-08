# Cómo se separa lo que paga Tesla de lo que se paga por compartir

La duda razonable es: si Tesla cobra el viaje, ¿cómo cobramos nosotros nuestra
parte y cómo se separan las dos cosas?

**No hay nada que separar, porque los dos pagos nunca se juntan.** Son dos
transacciones independientes, por vías distintas y entre partes distintas.

## El recorrido del dinero

```
  Tesla  ◄────────── $11.20 ───────────  Ana
                                          (app de Tesla, tarjeta de Ana)
                                          nosotros no estamos aquí

  Nosotros ◄──────── $6.72 ─────────────  Bruno
     │                                    (nuestra app, tarjeta de Bruno)
     ├──────────────► $5.60 ────────────► Ana
     └──────────────► $1.12 ────────────► nosotros
```

| | Paga | Recibe | Le queda |
|---|---|---|---|
| **Ana** | 11,20 $ a Tesla | 5,60 $ nuestros | coste de **5,60 $** |
| **Bruno** | 6,72 $ a nosotros | — | 5,60 $ de viaje + 1,12 $ de comisión |
| **Nosotros** | 5,60 $ a Ana | 6,72 $ de Bruno | **1,12 $** |

Tesla cobra a Ana por su cuenta, con su tarjeta, en su app. Nunca tocamos ese
dinero ni lo vemos. Lo nuestro es un **reembolso entre particulares** más una
comisión encima: otra transacción, con otras partes.

Por eso tampoco hay que dividir ningún cobro: nunca estuvieron juntos.

## El problema que esto abría, y cómo se cierra

Si quien organiza declarase al final lo que costó el viaje, podría inflarlo y
cobrarle de más a quien se subió. La aplicación no tiene forma de consultar lo
que Tesla cobró.

Se resuelve con **dos reglas**, no con vigilancia:

### 1. La tarifa se declara al publicar, no al cerrar

La app de Tesla enseña el precio **antes** de confirmar el viaje. Quien
organiza lo copia al publicar, y quien se plantea subirse ve **su parte exacta
antes de comprometerse**. Nadie acepta un precio que todavía no existe.

### 2. El precio del pasajero nunca sube

Si la flota acaba cobrando más de lo presupuestado, la diferencia la asume quien
organiza — que es quien vio el presupuesto y eligió la ruta.

Y ahí desaparece el incentivo: **inflar la tarifa al cerrar no da un solo
céntimo**. No hace falta comprobar si alguien miente, porque mentir no sirve
para nada.

Comprobado de punta a punta: pactado 5,60 $, la flota cobra 16,00 $ en vez de
11,20 $, y el pasajero sigue pagando 5,60 $.

> **Si el viaje sale más barato, sí baja.** Si los pasajeros pagaran lo pactado
> cuando la flota cobró menos, quien organiza ganaría dinero, y eso rompería la
> excepción de gastos compartidos de la que depende la legalidad del servicio.
> El reparto se reescala a la baja y la suma sigue cuadrando al céntimo.

## Los cargos que llegan después

Tesla cobra **tasas de limpieza después del viaje**: 50 $ por suciedad
moderada, 150 $ por severa. Se las carga **a quien pidió el coche**, aunque el
destrozo lo hiciera su acompañante.

Es la forma concreta de lo que dicen sus términos: *eres responsable de la
conducta de quien dejes subir*. Sin herramienta para repercutirlo, quien
organiza asumiría hasta 150 $ de riesgo a cambio de ahorrarse cinco.

Así que quien organiza puede **repercutir el cargo** a quien lo causó. Con tres
límites, y los tres importan:

- **Declararlo no lo cobra.** Hasta que la otra persona lo acepta, no existe
  como deuda. Cargarle dinero a alguien por la sola palabra de otro sería un
  agujero evidente.
- **Se puede discutir**, y entonces no se cobra nada y lo revisa una persona.
  Cuando dos versiones se contradicen, la aplicación no puede saber cuál es
  cierta, y fingir que sí sería peor que admitirlo.
- **El importe está acotado** en 200 $. Sin tope, esto se convertiría en una
  herramienta de extorsión entre desconocidos.

Un cargo aceptado entra en la liquidación **sin comisión nuestra**: no hemos
hecho nada por ese dinero, solo lo trasladamos.

## Lo que sigue haciendo falta

Todo lo anterior calcula y anota. **Mover el dinero de verdad necesita Stripe
Connect**, que es lo que queda pendiente: nosotros nunca custodiamos fondos, solo
producimos las instrucciones de cobro y pago que ejecuta un procesador con
licencia. Ver [`modelo-de-negocio.md`](modelo-de-negocio.md).

Mientras tanto el piloto funciona igual: la app calcula quién debe qué y las
personas se lo pasan por Venmo o Zelle. Feo, pero valida el producto sin
depender de Stripe.
