# Conectar Persona

Es lo único que separa la app de poder admitir usuarios de verdad. Sin un
proveedor real nadie alcanza el nivel *verificado*, y ningún Cybercab admite
pasajeros — el fallo seguro correcto, pero también significa que no convierte a
nadie.

**500 verificaciones de documento al mes gratis, sin contrato ni mínimos.**

---

## Qué hay que hacer en Persona

1. Crear cuenta en [withpersona.com](https://withpersona.com) (plan Starter).
2. Crear una **plantilla de verificación** (*Inquiry template*) que compruebe
   **documento oficial + selfie con prueba de vida**. Los dos juntos: un
   documento por sí solo demuestra que el documento existe, no que quien lo
   enseña sea su titular.
3. Activar la **decisión automática** en la plantilla, para que las
   verificaciones terminen en `approved` o `declined`. Ver el aviso de abajo.
4. Copiar el **identificador de la plantilla** (`itmpl_…`) y la **clave de API**.
5. Dar de alta un **webhook** apuntando a
   `https://tu-dominio/api/v1/webhooks/identidad`, y copiar su secreto.

## Qué hay que poner en el servidor

| Variable | Para qué |
|---|---|
| `PERSONA_API_KEY` | Clave del panel. Con ella se activa Persona; sin ella, modo manual |
| `PERSONA_TEMPLATE_ID` | Plantilla de documento + selfie. Acredita **las dos** comprobaciones en un solo trámite |
| `PERSONA_WEBHOOK_SECRET` | Firma de los avisos. Sin él se rechazan todos |
| `PERSONA_TEMPLATE_PHONE` | Opcional, plantilla de teléfono |
| `PERSONA_TEMPLATE_EMAIL` | Opcional, plantilla de correo |
| `PERSONA_ACEPTAR_COMPLETADO` | `1` acepta verificaciones que terminan sin decisión. **Leer el aviso** |

Nada más. `trust.Provider` ya estaba, así que no hay nada que rediseñar.

> ### Sobre `PERSONA_ACEPTAR_COMPLETADO`
>
> Si la plantilla no tiene decisión automática, **todas** las verificaciones
> terminan en `completed` sin que nadie haya revisado nada. Con esta variable
> activada, eso acreditaría a cualquiera.
>
> Por eso viene desactivada. Si la gente se queda sin poder verificarse, la
> plantilla está mal configurada: se arregla en Persona, no aquí. Que alguien no
> pueda verificarse tiene arreglo; una identidad falsa dada por buena, no.

## Cómo funciona

1. La persona pulsa **Verificar** y la app abre una *inquiry* con su
   identificador como `reference-id`.
2. Se le manda a un **enlace de un solo uso** que caduca en 24 horas.
3. Cuando Persona decide, envía un aviso firmado al webhook.
4. La app comprueba la firma, aplica el veredicto y —si la plantilla cubre
   documento y cara— acredita **las dos comprobaciones a la vez**. Un trámite,
   no dos.
5. Si el aviso no llega, `POST /api/v1/me/verificaciones/{ref}/refrescar`
   consulta el estado y reconcilia. No se depende de que el webhook llegue.

### Qué se guarda

Solo la referencia de Persona y el veredicto. **Nunca el documento ni la
fotografía**: los datos sensibles se quedan en quien está preparado para
custodiarlos, y una filtración de esta base de datos no expone el documento de
identidad de nadie.

Sí se guarda la **caducidad del documento**, porque el nivel de confianza vence
con él: cuando el carné caduca, la acreditación se cae sola sin que nadie tenga
que pasar por la base de datos marcando nada.

### Cómo se traducen los estados

| Persona | Aquí |
|---|---|
| `approved` | verificado |
| `completed` | pendiente, salvo `PERSONA_ACEPTAR_COMPLETADO=1` |
| `declined`, `failed` | rechazado |
| `expired` | caducado |
| `created`, `pending`, `needs_review`, cualquier estado nuevo | pendiente |

Ante un estado desconocido se sigue esperando. Acreditar por defecto sería
justo el error que este sistema existe para evitar.

## La firma de los avisos

El webhook es una ruta **pública** porque la llama Persona, no un usuario con
sesión. Lo que la protege es la firma, no un token.

La cabecera es `Persona-Signature: t=<unix>,v1=<hmac>`, y la firma es
`HMAC-SHA256(secreto, "<t>.<cuerpo>")`. La app:

- **Compara en tiempo constante**, para que medir tiempos de respuesta no filtre
  el secreto byte a byte.
- **Rechaza avisos de más de 5 minutos**: sin ese límite, quien capturase uno
  legítimo podría reenviarlo para siempre.
- **Acepta varias firmas** en la misma cabecera, porque durante la rotación del
  secreto Persona envía las dos.
- **No reabre lo ya resuelto**: reenviar un «aprobado» después de un rechazo no
  convierte el no en un sí.
- **Responde 200 a avisos de verificaciones que no conoce**, para que el
  proveedor no los reintente eternamente.

Hay pruebas para cada uno de esos casos, incluido el ataque que importa: una
firma legítima reutilizada sobre un cuerpo cambiado.

## Comprobar que está bien conectado

En los registros de arranque:

```
proveedor de identidad: Persona  comprobaciones=2
```

Si aparece cualquiera de estos, algo falta:

- `proveedor de identidad en modo manual` — no hay `PERSONA_API_KEY`
- `sin PERSONA_WEBHOOK_SECRET los avisos se rechazarán`
- `Persona mal configurado, se sigue en modo manual`

## Qué cuesta

| | |
|---|---|
| Primeras 500 verificaciones/mes | **0 $** |
| A partir de ahí | Plan de pago de Persona, o 1,50 $/verificación con Stripe Identity |

La verificación es **una vez por persona**, no por viaje. Con 1 000 usuarios
verificados al mes el coste ronda los 1 500 $; hasta 500, nada.

## Cambiar de proveedor

`trust.Provider` son tres métodos. Stripe Identity, Onfido o Veriff se conectan
implementándola, sin tocar el resto de la aplicación. El paquete `trust` no sabe
quién verifica: solo qué se ha acreditado y cuándo caduca.
