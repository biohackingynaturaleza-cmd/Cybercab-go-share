-- Contactos de confianza, seguimiento del viaje y botón de emergencia.

-- A quién avisar si algo va mal.
CREATE TABLE IF NOT EXISTS contactos_confianza (
    id              TEXT PRIMARY KEY,
    user_id         TEXT        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    nombre          TEXT        NOT NULL,
    email           TEXT        NOT NULL,
    -- Manda el enlace de seguimiento al empezar cada viaje, sin tener que
    -- acordarse. La mayoría de la gente no se acuerda.
    avisar_al_salir BOOLEAN     NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS contactos_de ON contactos_confianza (user_id);

-- La misma dirección no se añade dos veces: dos avisos idénticos no avisan más.
CREATE UNIQUE INDEX IF NOT EXISTS contactos_sin_repetir
    ON contactos_confianza (user_id, lower(email));

-- El enlace público que enseña un viaje en curso.
CREATE TABLE IF NOT EXISTS seguimientos (
    id                 TEXT PRIMARY KEY,
    trip_id            TEXT        NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    user_id            TEXT        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- El hash del testigo, nunca el testigo.
    token_hash         TEXT        NOT NULL UNIQUE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    expira_at          TIMESTAMPTZ NOT NULL,
    revocado_at        TIMESTAMPTZ,
    -- La última posición que dijo el navegador de quien comparte. Nula
    -- mientras no la diga: no se rastrea a nadie de fondo.
    ultima_lat         DOUBLE PRECISION,
    ultima_lng         DOUBLE PRECISION,
    ultima_posicion_at TIMESTAMPTZ
);

-- Varios enlaces vivos por viaje y persona conviven a propósito. El testigo se
-- guarda hasheado, así que de la base de datos no se puede recuperar la
-- dirección de un enlace ya mandado: si crear uno nuevo matara al anterior, el
-- correo del botón de emergencia no podría llevar un enlace que funcione sin
-- dejar tirado a quien ya tenía el otro. Cerrar el seguimiento los cierra todos.
CREATE INDEX IF NOT EXISTS seguimientos_vivos
    ON seguimientos (trip_id, user_id)
    WHERE revocado_at IS NULL;

-- El botón de emergencia.
CREATE TABLE IF NOT EXISTS alertas (
    id          TEXT PRIMARY KEY,
    trip_id     TEXT        NOT NULL REFERENCES trips (id) ON DELETE RESTRICT,
    user_id     TEXT        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    lat         DOUBLE PRECISION,
    lng         DOUBLE PRECISION,
    nota        TEXT        NOT NULL DEFAULT '',
    estado      TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resuelta_at TIMESTAMPTZ,
    resolucion  TEXT        NOT NULL DEFAULT ''
);

-- La cola de operaciones: las alertas abiertas, la más reciente primero. Van
-- por delante de cualquier denuncia.
CREATE INDEX IF NOT EXISTS alertas_abiertas
    ON alertas (created_at DESC)
    WHERE estado = 'abierta';
CREATE INDEX IF NOT EXISTS alertas_de ON alertas (user_id, created_at DESC);
