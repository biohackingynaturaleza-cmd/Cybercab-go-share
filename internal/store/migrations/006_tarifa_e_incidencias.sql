-- Tarifa declarada al publicar, e incidencias posteriores al viaje.

-- La tarifa que la app de Tesla enseña antes de confirmar el viaje.
--
-- Se declara al publicar, no al cerrar: así el pasajero ve su parte exacta
-- antes de comprometerse y quien organiza no puede inflarla después. Cero
-- significa que no se declaró y se reparte sobre nuestra estimación.
ALTER TABLE trips ADD COLUMN IF NOT EXISTS tarifa_declarada_cents BIGINT NOT NULL DEFAULT 0
    CHECK (tarifa_declarada_cents >= 0);

-- Lo que la flota cobró de verdad, una vez terminado el viaje.
ALTER TABLE trips ADD COLUMN IF NOT EXISTS tarifa_real_cents BIGINT NOT NULL DEFAULT 0
    CHECK (tarifa_real_cents >= 0);

-- Incidencias que la flota cobra después del viaje.
--
-- Tesla carga tasas de limpieza (50 $ por suciedad moderada, 150 $ por severa)
-- a la cuenta de quien pidió el coche, aunque el destrozo lo hiciera otro. Sin
-- esta tabla, quien organiza asume esa responsabilidad sin ninguna herramienta
-- para repercutirla.
CREATE TABLE IF NOT EXISTS incidencias (
    id            TEXT PRIMARY KEY,
    trip_id       TEXT        NOT NULL REFERENCES trips (id) ON DELETE RESTRICT,
    -- Quien la declara: siempre quien organiza, que es a quien se la cobran.
    declarante_id TEXT        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    -- A quien se le atribuye.
    atribuida_a   TEXT        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    tipo          TEXT        NOT NULL,
    importe_cents BIGINT      NOT NULL CHECK (importe_cents > 0),
    descripcion   TEXT        NOT NULL DEFAULT '',
    estado        TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    resuelta_at   TIMESTAMPTZ,

    -- Nadie se atribuye una incidencia a sí mismo: para eso no hace falta
    -- declararla, ya la paga.
    CONSTRAINT incidencias_no_a_uno_mismo CHECK (declarante_id <> atribuida_a)
);

CREATE INDEX IF NOT EXISTS incidencias_trip ON incidencias (trip_id);
CREATE INDEX IF NOT EXISTS incidencias_atribuida ON incidencias (atribuida_a, estado);

-- Una sola incidencia viva por trayecto y persona: si hace falta otra, primero
-- se resuelve la anterior.
CREATE UNIQUE INDEX IF NOT EXISTS incidencias_una_viva
    ON incidencias (trip_id, atribuida_a)
    WHERE estado IN ('declarada', 'discutida');
