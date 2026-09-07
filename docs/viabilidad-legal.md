# Viabilidad legal y comercial

Investigación sobre las dos preguntas que bloqueaban la decisión de invertir.
Fecha: septiembre de 2026. **No es asesoramiento jurídico**: es la base para
que un abogado de Texas confirme o corrija, y para decidir con datos en vez de
con intuiciones.

---

## 1. ¿Permiten los términos de Tesla compartir así un viaje?

**Sí, con dos condiciones que el producto ya cumple, y una consecuencia que
había que reflejar en el código.**

Los términos del servicio de robotaxi establecen:

1. **Hay que estar presente durante todo el viaje que se pide.** No se puede
   pedir un viaje para otra persona sin ir en él.
   → Nuestro modelo lo cumple por construcción: quien organiza es un pasajero
   más y ocupa la ruta entera (del km 0 al final). No existe la figura de
   "pedir un coche para otro".

2. **Se puede llevar acompañantes.** Los términos contemplan expresamente
   "cualquier otra persona a la que permitas entrar en el vehículo".
   → Compartir no está prohibido. Es justo lo que hacemos.

3. **Quien pide el viaje responde de la conducta de quien deja subir.**
   → **Esta es la consecuencia que había que implementar.** Aceptar a un
   desconocido significa asumir responsabilidad por lo que haga y por los daños
   que cause. Dejar que alguien lo hiciera sin saberlo sería ocultarle una
   obligación real frente a Tesla.

   Implementado: `service.DecisionInput.AceptaResponsabilidad`. Sin esa
   aceptación explícita no se puede confirmar a nadie, y la API devuelve el
   aviso junto al error. Rechazar no exige asumir nada.

**Aforo confirmado:** el Cybercab admite hasta dos personas. Nuestro modelo de
vehículos era correcto, y refuerza el suelo de identidad verificada para el
biplaza.

**Lo que queda por confirmar con un abogado:** si cobrar a un acompañante,
aunque sea a precio de coste, entra en alguna prohibición de uso comercial de
la cuenta. Los términos publicados no la mencionan, pero el silencio no es
permiso.

**Riesgo real:** Tesla puede suspender o cancelar cuentas por incumplir sus
términos. Si decidiera que esto no le gusta, el producto depende de cuentas
que Tesla puede cerrar. Es un riesgo de plataforma, no legal, y no tiene
mitigación técnica: solo un acuerdo comercial con Tesla.

---

## 2. ¿Qué implica que la app liquide pagos entre personas?

Dos regulaciones distintas. Una nos exime; la otra se esquiva con arquitectura.

### 2.1. Transporte comercial: estamos exentos, y hay que seguir estándolo

La ley de Texas excluye expresamente de la regulación de las *Transportation
Network Companies* los **acuerdos de gastos compartidos** y aquellos en los que
**"la cantidad recibida no excede el coste de proporcionar el viaje"**.

Esto significa:

- Mientras quien organiza **no gane dinero**, no somos una TNC.
- No necesitamos el permiso estatal de TNC (cuota de 10 500 $).
- En cuanto quien organiza obtenga beneficio, el servicio pasa a ser transporte
  comercial y todo el marco regulatorio cae encima.

**La legalidad del producto depende, literalmente, de una propiedad del
algoritmo de reparto.** Por eso ya no es una propiedad incidental:

`pricing.VerificarSinLucro` comprueba en cada desglose que lo aportado por los
pasajeros nunca supera el coste del viaje y que ninguna parte es negativa. Se
ejecuta en `FareBreakdownFor` y falla antes de enseñar un reparto que rompiera
la línea. Hay pruebas que lo fijan, incluido el caso límite legítimo: el coche
va lleno, quien organiza viaja gratis, pero no gana nada.

> **Cuidado con la comisión de la plataforma.** Que *nosotros* cobremos una
> comisión no hace que quien organiza gane dinero, así que no rompe la excepción
> por sí mismo. Pero sí cambia lo que paga el pasajero y merece revisión
> jurídica antes de introducirla.

### 2.2. Transmisión de dinero: no toquemos el dinero

Mover fondos entre personas puede exigir licencia de transmisor de dinero
—federal (FinCEN) y estatal, una por estado—. Es caro, lento y desproporcionado
para una app en validación.

**La solución estándar es no ser nunca el intermediario.** Con un facilitador de
pagos ya licenciado (Stripe Connect, Adyen para plataformas), los fondos van del
pasajero a quien organiza a través del procesador; la app nunca los custodia ni
los mueve. Stripe está licenciada como transmisor de dinero en todos los estados
donde hace falta.

Existe además la exención de *agent of the payee* en varios estados, pero es
estrecha y depende de los hechos: no cubre a quien agrupa fondos o mantiene
saldo. **No conviene apoyarse en ella**; conviene no tocar el dinero.

**Consecuencia de diseño:** no implementar un monedero, ni saldos, ni retención
de fondos. El reparto que calcula la app es una *instrucción de cobro* para el
procesador, no un movimiento de dinero nuestro.

---

## 3. Qué implica contratar un proveedor de identidad

**Cero coste hasta tener tracción real.**

| Proveedor | Coste | Compromiso |
|---|---|---|
| **Persona** | **500 verificaciones de documento al mes gratis**, con selfie opcional | Sin contrato. Plan de pago desde 250 $/mes |
| **Stripe Identity** | 1,50 $ por verificación **superada** (las fallidas no se cobran) | Sin mínimos ni contrato |

Lo que esto significa para la decisión de invertir:

- Se puede validar el producto entero con **500 personas verificadas al mes
  sin pagar nada**. Para un piloto en Austin, sobra.
- Superado ese umbral, Stripe Identity cuesta 1,50 $ por persona verificada,
  **una sola vez** (la verificación no se repite en cada viaje). Con 1 000
  usuarios verificados al mes: 1 500 $/mes.
- No hay contratos anuales ni mínimos en ninguno de los dos. **No se asume
  gasto recurrente hasta que hay volumen.**

Lo que sí hay que asumir antes:

- **Integración**: la interfaz `trust.Provider` ya está hecha. Conectar un
  proveedor real es implementarla, no rediseñar nada.
- **Protección de datos**: aunque no guardemos documentos, somos responsables
  del tratamiento. Hace falta política de privacidad y acuerdo de encargado del
  tratamiento con el proveedor.
- **Trámite**: los proveedores serios piden datos de la empresa. Hace falta
  entidad constituida antes de firmar.

---

## Resumen para decidir

| Pregunta | Respuesta | Qué queda |
|---|---|---|
| ¿Lo permiten los términos de Tesla? | **Sí**, si quien organiza va en el viaje y asume responsabilidad por su acompañante | Confirmar que cobrar a precio de coste no es "uso comercial" |
| ¿Hace falta permiso de TNC? | **No**, mientras nadie gane dinero. Ya está garantizado en el código | Revisión jurídica antes de introducir comisión |
| ¿Hace falta licencia de transmisor de dinero? | **No**, si no tocamos los fondos (Stripe Connect) | Diseñar el cobro sin monedero ni saldos |
| ¿Cuánto cuesta verificar identidades? | **0 € hasta 500/mes** (Persona), luego 1,50 $ por persona | Constituir entidad antes de firmar |

**Riesgo mayor, y no es legal:** que Tesla decida que no le gusta y cierre las
cuentas. No tiene mitigación técnica.

---

## Fuentes

- [Robotaxi | Tesla Support](https://www.tesla.com/support/robotaxi)
- [Cybercab FAQ | Tesla Support](https://www.tesla.com/support/robotaxi/cybercab)
- [Tesla Outlines Rules for Using Its Robotaxi Service](https://www.notateslaapp.com/news/2849/tesla-outlines-rules-for-using-its-robotaxi-service)
- [Tesla Robotaxi Terms Now Cover Cybercabs](https://teslanorth.com/2026/09/03/tesla-robotaxi-cybercab-terms/)
- [Information for the Public About Ride-sharing and Delivery Network Companies — TDLR](https://www.tdlr.texas.gov/tnc/info.htm)
- [TNC at a Glance — TDLR](https://www.tdlr.texas.gov/tnc/TNC%20at%20a%20Glance.pdf)
- [Apply for a New TNC Permit — TDLR](https://www.tdlr.texas.gov/tnc/apply.htm)
- [What is a money transmitter? — Stripe](https://stripe.com/resources/more/what-is-a-money-transmitter)
- [Money Transmission in the Payment Facilitator Model — Venable LLP](https://www.venable.com/insights/publications/2018/06/money-transmission-in-the-payment-facilitator-mode)
- [When Does a Marketplace Need a Money Transmitter License? — ComplyOne](https://complyone.tech/blog/when-does-a-marketplace-need-a-money-transmitter-license)
- [Free Identity Verification with Persona's Starter Plan](https://withpersona.com/blog/free-identity-verification/)
- [Identity Verification Pricing Comparison 2026 — Trust Swiftly](https://trustswiftly.com/blog/identity-verification-pricing-comparison-and-alternatives/)
