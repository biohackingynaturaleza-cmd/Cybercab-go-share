# Poner la app en marcha

Un solo binario estático de 11 MB con la interfaz dentro. No hay despliegue de
frontend aparte que pueda quedar desincronizado con la API, ni servidor de
estáticos que configurar.

## En local, en un minuto

```bash
make demo        # http://localhost:8080 con datos de ejemplo
```

Guarda en memoria y se pierde al reiniciar. Suficiente para probar.

## Con base de datos y rutas reales

```bash
docker compose up --build
```

Levanta la aplicación y Postgres con PostGIS. Las migraciones se aplican solas
al arrancar.

Para rutas por carretera de verdad hace falta un OSRM propio con el mapa de
Texas (el servidor público de demostración no tiene garantías de servicio y no
debe usarse en producción):

```bash
mkdir -p osrm && cd osrm
curl -O https://download.geofabrik.de/north-america/us/texas-latest.osm.pbf
docker run -t -v "${PWD}:/data" ghcr.io/project-osrm/osrm-backend \
  osrm-extract -p /opt/car.lua /data/texas-latest.osm.pbf
docker run -t -v "${PWD}:/data" ghcr.io/project-osrm/osrm-backend \
  osrm-partition /data/texas-latest.osrm
docker run -t -v "${PWD}:/data" ghcr.io/project-osrm/osrm-backend \
  osrm-customize /data/texas-latest.osrm
cd .. && docker compose --profile rutas-reales up
```

## En producción

```bash
docker build -t cybercab-go-share .
docker run -p 8080:8080 \
  -e ENV=production \
  -e DATABASE_URL="postgres://..." \
  -e AUTH_SECRET="$(openssl rand -base64 32)" \
  -e OSRM_URL="http://tu-osrm:5000" \
  cybercab-go-share
```

Con `ENV=production` la aplicación **se niega a arrancar** sin `DATABASE_URL` ni
`AUTH_SECRET`, y el endpoint de desarrollo que resuelve verificaciones a mano no
se registra siquiera. Es deliberado: son los dos fallos que convertirían un
despliegue en un problema serio y silencioso.

### Variables obligatorias en producción

| Variable | Por qué |
|---|---|
| `DATABASE_URL` | Sin ella los datos viven en memoria y se pierden al reiniciar |
| `AUTH_SECRET` | Mínimo 32 bytes. Si se generase uno nuevo en cada arranque, todas las sesiones caducarían en cada despliegue y no se podría escalar a varias réplicas |
| `SMTP_HOST` y `SMTP_FROM` | Sin correo nadie se entera de nada: quien organiza no sabrá que le han pedido plaza |
| `PUBLIC_URL` | La dirección que se pone en los enlaces de los correos |

### Correo

| Variable | Por defecto |
|---|---|
| `SMTP_HOST` | — Sin él, los correos se escriben en el registro en vez de enviarse |
| `SMTP_PORT` | `587` |
| `SMTP_USER`, `SMTP_PASSWORD` | Vacías: se conecta sin autenticar (solo con un relé local) |
| `SMTP_FROM` | `Cybercab Go Share <no-reply@localhost>` |

En desarrollo, sin `SMTP_HOST`, los correos aparecen enteros en el registro. Se
ve exactamente qué se habría mandado sin montar un servidor y sin riesgo de
escribir a direcciones reales desde una máquina de pruebas.

### Antes de abrir a usuarios reales

**Hay que dar de alta la cuenta de Persona.** El código está hecho; falta crear
la cuenta, la plantilla de verificación y poner cuatro variables de entorno. Sin
eso la app arranca en modo manual, que da por buena cualquier verificación y
solo sirve para desarrollo.

Paso a paso en
[`verificacion-de-identidad.md`](verificacion-de-identidad.md). Son 500
verificaciones al mes gratis, sin contrato.

El resto de lo que queda antes de operar está en
[`viabilidad-legal.md`](viabilidad-legal.md).

## Comprobar que un despliegue está sano

```bash
curl https://tu-dominio/healthz          # {"status":"ok"}
curl https://tu-dominio/api/v1/zonas     # el área de servicio
```

En los registros de arranque, estos avisos indican que algo falta:

- `DATABASE_URL sin definir` — está guardando en memoria
- `AUTH_SECRET sin definir` — las sesiones morirán al reiniciar
- `proveedor de identidad en modo manual` — no verifica nada
- `endpoint de desarrollo activo` — no debe aparecer en producción

## Detrás de un proxy

La aplicación sirve HTTP plano. En producción va detrás de un proxy que termine
TLS. La política de seguridad de contenido que ya envía asume que la interfaz y
la API comparten origen, así que conviene servirlas bajo el mismo dominio.
