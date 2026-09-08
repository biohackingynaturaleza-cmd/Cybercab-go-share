-- Aceptación de las condiciones del servicio.
--
-- Se guarda qué redacción se aceptó y cuándo, no un simple "sí": una casilla
-- marcada sin versión ni fecha no acredita nada el día que alguien discuta a
-- qué se comprometió. La columna admite vacío porque las cuentas anteriores a
-- esta migración existen y no habían aceptado nada todavía.

ALTER TABLE users ADD COLUMN IF NOT EXISTS terminos_version TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS terminos_at TIMESTAMPTZ;
