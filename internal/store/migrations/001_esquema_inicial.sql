-- Esquema inicial de Cybercab Go Share.
--
-- Las rutas se guardan como geography(LineString, 4326): PostGIS calcula
-- distancias reales sobre el elipsoide, sin que haya que proyectar a mano.

CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    name          TEXT        NOT NULL,
    email         TEXT        NOT NULL,
    password_hash TEXT        NOT NULL,
    rating        REAL        NOT NULL DEFAULT 5,
    ride_count    INTEGER     NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Unicidad de email sin distinguir mayúsculas: los buzones no las distinguen
-- en la práctica, y el índice funcional deja que la base de datos lo garantice
-- en vez de confiar en que la aplicación se acuerde.
CREATE UNIQUE INDEX IF NOT EXISTS users_email_key ON users (lower(email));

CREATE TABLE IF NOT EXISTS trips (
    id               TEXT PRIMARY KEY,
    host_id          TEXT        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    origin_name      TEXT        NOT NULL,
    destination_name TEXT        NOT NULL,
    -- La ruta completa que sigue el vehículo. Origen y destino se leen de sus
    -- extremos, así que no se duplican en columnas aparte y no pueden
    -- contradecirla.
    route            geography(LineString, 4326) NOT NULL,
    duration_min     DOUBLE PRECISION NOT NULL DEFAULT 0,
    route_source     TEXT        NOT NULL DEFAULT 'straight_line',
    departure_time   TIMESTAMPTZ NOT NULL,
    vehicle          TEXT        NOT NULL,
    seats_total      INTEGER     NOT NULL CHECK (seats_total >= 0),
    seats_taken      INTEGER     NOT NULL DEFAULT 0 CHECK (seats_taken >= 0),
    max_detour_km    DOUBLE PRECISION NOT NULL DEFAULT 1.5,
    notes            TEXT        NOT NULL DEFAULT '',
    status           TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Nunca se pueden ocupar más plazas de las ofertadas, pase lo que pase en
    -- la aplicación.
    CONSTRAINT trips_plazas_coherentes CHECK (seats_taken <= seats_total)
);

-- El índice espacial es lo que hace viable la búsqueda: sin él, cada consulta
-- recorrería todas las rutas abiertas.
CREATE INDEX IF NOT EXISTS trips_route_gist ON trips USING GIST (route);

-- Las búsquedas siempre filtran por trayectos abiertos dentro de una horquilla
-- horaria; el índice parcial se ciñe a esas filas.
CREATE INDEX IF NOT EXISTS trips_abiertos_por_salida
    ON trips (departure_time)
    WHERE status = 'open';

CREATE INDEX IF NOT EXISTS trips_host_id ON trips (host_id);

CREATE TABLE IF NOT EXISTS bookings (
    id               TEXT PRIMARY KEY,
    trip_id          TEXT        NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    passenger_id     TEXT        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    pickup_name      TEXT        NOT NULL,
    pickup_point     geography(Point, 4326) NOT NULL,
    dropoff_name     TEXT        NOT NULL,
    dropoff_point    geography(Point, 4326) NOT NULL,
    pickup_along_km  DOUBLE PRECISION NOT NULL,
    dropoff_along_km DOUBLE PRECISION NOT NULL,
    seats            INTEGER     NOT NULL CHECK (seats >= 1),
    price_cents      BIGINT      NOT NULL,
    status           TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Nadie puede bajarse antes de haberse subido.
    CONSTRAINT bookings_tramo_con_sentido CHECK (dropoff_along_km > pickup_along_km)
);

CREATE INDEX IF NOT EXISTS bookings_trip_id ON bookings (trip_id, created_at);
CREATE INDEX IF NOT EXISTS bookings_passenger_id ON bookings (passenger_id, created_at);

-- Una misma persona no puede tener dos reservas vivas en el mismo trayecto.
CREATE UNIQUE INDEX IF NOT EXISTS bookings_una_viva_por_pasajero
    ON bookings (trip_id, passenger_id)
    WHERE status IN ('pending', 'confirmed');
