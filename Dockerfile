# Compilación
FROM golang:1.24-alpine AS build

WORKDIR /src

# Las dependencias primero: cambian mucho menos que el código, así que esta capa
# se reaprovecha entre compilaciones.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Sin cgo y con la información de depuración quitada: binario estático y pequeño
# que corre en una imagen vacía.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/cybercab ./cmd/server

# Imagen final
FROM alpine:3.20

# Certificados para poder hablar con el motor de rutas y el proveedor de
# identidad por HTTPS, y zonas horarias para interpretar bien las salidas.
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 cybercab

COPY --from=build /bin/cybercab /usr/local/bin/cybercab

# Nunca como root: si alguien encuentra un fallo en el servidor, que no herede
# la máquina entera.
USER cybercab

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["cybercab"]
