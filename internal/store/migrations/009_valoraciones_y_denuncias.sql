-- Valoraciones y denuncias.

-- Valoraciones entre quien organiza y quien se sube.
--
-- No llevan columna de "publicada": si esta valoración cuenta o no se deduce de
-- si existe la recíproca y de cuánto tiempo ha pasado, igual que el nivel de
-- confianza se deduce de las comprobaciones vigentes. Un estado guardado que
-- también se puede deducir acaba discrepando del que se deduce.
CREATE TABLE IF NOT EXISTS valoraciones (
    id         TEXT PRIMARY KEY,
    booking_id TEXT        NOT NULL REFERENCES bookings (id) ON DELETE CASCADE,
    trip_id    TEXT        NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    autor_id   TEXT        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    sobre_id   TEXT        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    estrellas  SMALLINT    NOT NULL CHECK (estrellas BETWEEN 1 AND 5),
    comentario TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT valoraciones_no_a_uno_mismo CHECK (autor_id <> sobre_id)
);

-- Una valoración por reserva y autor: cada parte opina una vez de la otra.
CREATE UNIQUE INDEX IF NOT EXISTS valoraciones_una_por_parte
    ON valoraciones (booking_id, autor_id);

CREATE INDEX IF NOT EXISTS valoraciones_sobre ON valoraciones (sobre_id);
CREATE INDEX IF NOT EXISTS valoraciones_autor ON valoraciones (autor_id);

-- Denuncias.
--
-- Nunca se le enseñan al denunciado: una denuncia que llega a sus oídos es una
-- denuncia que nadie pone.
CREATE TABLE IF NOT EXISTS denuncias (
    id             TEXT PRIMARY KEY,
    trip_id        TEXT        REFERENCES trips (id) ON DELETE SET NULL,
    denunciante_id TEXT        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    denunciado_id  TEXT        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    motivo         TEXT        NOT NULL,
    descripcion    TEXT        NOT NULL,
    estado         TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    resuelta_at    TIMESTAMPTZ,
    resolucion     TEXT        NOT NULL DEFAULT '',

    CONSTRAINT denuncias_no_a_uno_mismo CHECK (denunciante_id <> denunciado_id)
);

-- La cola de revisión se ordena por antigüedad entre las abiertas.
CREATE INDEX IF NOT EXISTS denuncias_abiertas
    ON denuncias (created_at)
    WHERE estado = 'abierta';
CREATE INDEX IF NOT EXISTS denuncias_denunciado ON denuncias (denunciado_id);
CREATE INDEX IF NOT EXISTS denuncias_denunciante ON denuncias (denunciante_id);

-- Suspensión: la consecuencia que hace que denunciar sirva para algo.
--
-- Aparta de compartir viajes, no de entrar en la cuenta: quien está suspendido
-- sigue teniendo saldos que liquidar, y dejarle fuera de todo solo consigue
-- que no responda de nada.
ALTER TABLE users ADD COLUMN IF NOT EXISTS suspendido_hasta TIMESTAMPTZ;

-- La media de valoraciones se calcula a partir de la tabla, no se guarda.
ALTER TABLE users DROP COLUMN IF EXISTS rating;
ALTER TABLE users DROP COLUMN IF EXISTS rating_count;
