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

### Antes de abrir a usuarios reales

**Falta contratar un proveedor de identidad.** El que hay es manual y da por
buena cualquier verificación: sirve para desarrollo y nada más. Sin uno real,
nadie alcanza el nivel verificado y ningún Cybercab admite pasajeros — que es el
fallo seguro correcto, pero también significa que la app no convierte a nadie.

Persona ofrece 500 verificaciones de documento al mes gratis y Stripe Identity
cobra 1,50 $ por verificación superada, sin contrato ni mínimos. Conectar uno es
implementar `trust.Provider`; no hay que rediseñar nada.

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
