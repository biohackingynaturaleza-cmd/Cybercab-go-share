-- Liquidaciones y sus instrucciones.
--
-- Hasta ahora la liquidación se calculaba y se perdía: los apuntes quedaban
-- marcados como cobrados y las instrucciones de cobro solo existían en la
-- respuesta HTTP de quien la lanzó. Sin esta tabla no hay a qué reintentar, ni
-- qué auditar, ni cómo saber si a alguien se le cobró.

CREATE TABLE IF NOT EXISTS liquidaciones (
    id                   TEXT PRIMARY KEY,
    desde                TIMESTAMPTZ NOT NULL,
    hasta                TIMESTAMPTZ NOT NULL,
    comision_total_cents BIGINT      NOT NULL DEFAULT 0,
    apuntes_liquidados   INTEGER     NOT NULL DEFAULT 0,
    -- calculada: las instrucciones existen y esperan procesador.
    -- ejecutada: todas se movieron. parcial: unas sí y otras no.
    estado               TEXT        NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    ejecutada_at         TIMESTAMPTZ,

    CONSTRAINT liquidaciones_periodo_con_sentido CHECK (hasta > desde)
);

-- Un periodo se liquida una sola vez. Es la barrera que impide que dos
-- ejecuciones simultáneas del programador cobren el mismo mes dos veces.
CREATE UNIQUE INDEX IF NOT EXISTS liquidaciones_un_periodo
    ON liquidaciones (desde, hasta);

CREATE TABLE IF NOT EXISTS instrucciones_liquidacion (
    id              TEXT PRIMARY KEY,
    liquidacion_id  TEXT        NOT NULL REFERENCES liquidaciones (id) ON DELETE CASCADE,
    user_id         TEXT        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    tipo            TEXT        NOT NULL CHECK (tipo IN ('cobro', 'pago')),
    amount_cents    BIGINT      NOT NULL CHECK (amount_cents > 0),
    estado          TEXT        NOT NULL,
    -- La referencia del movimiento en el procesador, para auditarlo sin
    -- replicar aquí sus datos.
    ref             TEXT        NOT NULL DEFAULT '',
    motivo          TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    ejecutada_at    TIMESTAMPTZ
);

-- Un movimiento por persona y liquidación. Es la misma clave de idempotencia
-- que se le manda al procesador: reintentar no puede duplicar un cargo.
CREATE UNIQUE INDEX IF NOT EXISTS instrucciones_una_por_persona
    ON instrucciones_liquidacion (liquidacion_id, user_id);

CREATE INDEX IF NOT EXISTS instrucciones_de ON instrucciones_liquidacion (user_id, created_at DESC);
