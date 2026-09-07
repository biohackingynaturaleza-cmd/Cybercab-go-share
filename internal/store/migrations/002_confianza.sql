-- Verificación de identidad y bloqueos entre personas.
--
-- Ninguna tabla guarda documentos ni fotografías: solo la referencia del
-- proveedor externo y el veredicto. Una filtración de esta base de datos no
-- expone el documento de identidad de nadie.

CREATE TABLE IF NOT EXISTS identity_checks (
    id               TEXT PRIMARY KEY,
    user_id          TEXT        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind             TEXT        NOT NULL,
    status           TEXT        NOT NULL,
    provider_ref     TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    verified_at      TIMESTAMPTZ,
    expires_at       TIMESTAMPTZ,
    rejection_reason TEXT        NOT NULL DEFAULT ''
);

-- La referencia del proveedor identifica la verificación de forma única: es
-- por donde entran los webhooks, y aceptar dos veces la misma permitiría
-- reutilizar un veredicto ajeno.
CREATE UNIQUE INDEX IF NOT EXISTS identity_checks_provider_ref
    ON identity_checks (provider_ref)
    WHERE provider_ref IS NOT NULL;

CREATE INDEX IF NOT EXISTS identity_checks_user ON identity_checks (user_id);

-- Solo puede haber una comprobación viva de cada tipo por persona: sin esto,
-- un rechazo se podría enterrar bajo intentos repetidos.
CREATE UNIQUE INDEX IF NOT EXISTS identity_checks_una_viva_por_tipo
    ON identity_checks (user_id, kind)
    WHERE status IN ('pending', 'verified');

CREATE TABLE IF NOT EXISTS user_blocks (
    blocker_id TEXT        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    blocked_id TEXT        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (blocker_id, blocked_id),
    CONSTRAINT user_blocks_no_a_uno_mismo CHECK (blocker_id <> blocked_id)
);

-- Se consulta en las dos direcciones: quién he bloqueado y quién me ha
-- bloqueado a mí.
CREATE INDEX IF NOT EXISTS user_blocks_blocked ON user_blocks (blocked_id);

-- Nivel de confianza mínimo que exige cada trayecto. El suelo del vehículo
-- puede elevarlo en la aplicación, nunca rebajarlo.
ALTER TABLE trips ADD COLUMN IF NOT EXISTS min_trust_level TEXT NOT NULL DEFAULT 'nuevo';

-- Número de valoraciones recibidas, para poder distinguir una media sólida de
-- una construida sobre un único voto.
ALTER TABLE users ADD COLUMN IF NOT EXISTS rating_count INTEGER NOT NULL DEFAULT 0;
