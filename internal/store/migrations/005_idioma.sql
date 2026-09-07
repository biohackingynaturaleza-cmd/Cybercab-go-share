-- Idioma de cada persona.
--
-- Los correos se escriben en el idioma en el que usa la app: mandar un aviso en
-- español a quien la usa en inglés es tratarle como a un usuario de segunda.
-- El servicio opera en Austin, así que el valor por defecto es el inglés.

ALTER TABLE users ADD COLUMN IF NOT EXISTS idioma TEXT NOT NULL DEFAULT 'en';

-- ADD CONSTRAINT no admite IF NOT EXISTS, así que se comprueba antes. Todas
-- las migraciones de este proyecto se pueden volver a aplicar sin romperse, y
-- esta no va a ser la excepción.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'users_idioma_conocido'
    ) THEN
        ALTER TABLE users ADD CONSTRAINT users_idioma_conocido
            CHECK (idioma IN ('en', 'es')) NOT VALID;
    END IF;
END $$;
