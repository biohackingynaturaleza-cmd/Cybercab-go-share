-- Recuperación de contraseña.
--
-- Sin esto, quien olvida la contraseña pierde la cuenta para siempre: el
-- historial de viajes, la identidad verificada y los apuntes pendientes se
-- quedan dentro y no hay forma de volver a entrar.

CREATE TABLE IF NOT EXISTS password_resets (
    id         TEXT PRIMARY KEY,
    user_id    TEXT        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- El hash del testigo, nunca el testigo. Quien lea esta tabla no puede
    -- entrar en ninguna cuenta con lo que hay en ella.
    token_hash TEXT        NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expira_at  TIMESTAMPTZ NOT NULL,
    -- Un enlace se gasta una vez. Marcarlo aquí es lo que impide reutilizarlo.
    usada_at   TIMESTAMPTZ
);

-- Para anular de golpe los enlaces vivos de alguien al cambiar la contraseña.
CREATE INDEX IF NOT EXISTS password_resets_vivos
    ON password_resets (user_id)
    WHERE usada_at IS NULL;
