-- Códigos de confirmación de correo.
--
-- La comprobación del buzón deja de pasar por el proveedor de identidad: es un
-- código que mandamos y esperamos de vuelta, no un trámite con documento y
-- cámara. El proveedor se reserva para lo que solo él puede hacer.

CREATE TABLE IF NOT EXISTS codigos_correo (
    id          TEXT PRIMARY KEY,
    user_id     TEXT        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- La comprobación de confianza que este código resuelve.
    check_ref   TEXT        NOT NULL,
    -- El hash del código, nunca el código.
    codigo_hash TEXT        NOT NULL,
    -- Los intentos gastados. Sin tope, seis dígitos se prueban en minutos.
    intentos    SMALLINT    NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expira_at   TIMESTAMPTZ NOT NULL,
    usado_at    TIMESTAMPTZ
);

-- Para encontrar el código vivo de alguien sin recorrer la tabla.
CREATE INDEX IF NOT EXISTS codigos_correo_vivos
    ON codigos_correo (user_id, created_at DESC)
    WHERE usado_at IS NULL;
