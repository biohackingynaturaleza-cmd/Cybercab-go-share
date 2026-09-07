-- Libro de apuntes y liquidaciones.
--
-- No hay tabla de saldos ni de monederos, y es deliberado: lo que se guarda son
-- cuentas pendientes, no dinero custodiado. La app nunca retiene fondos.

CREATE TABLE IF NOT EXISTS ledger_entries (
    id              TEXT PRIMARY KEY,
    user_id         TEXT        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    trip_id         TEXT        NOT NULL REFERENCES trips (id) ON DELETE RESTRICT,
    booking_id      TEXT,
    kind            TEXT        NOT NULL,
    amount_cents    BIGINT      NOT NULL CHECK (amount_cents > 0),
    counterparty_id TEXT        REFERENCES users (id) ON DELETE RESTRICT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    settlement_id   TEXT,

    -- Nadie puede deberse dinero a sí mismo.
    CONSTRAINT ledger_no_a_uno_mismo CHECK (counterparty_id IS NULL OR counterparty_id <> user_id),
    -- Una parte del coste siempre tiene acreedor; la comisión, nunca.
    CONSTRAINT ledger_contraparte_coherente CHECK (
        (kind = 'cost_share'  AND counterparty_id IS NOT NULL) OR
        (kind = 'service_fee' AND counterparty_id IS NULL)
    )
);

-- Las filas se borran nunca: los apuntes son inmutables y corregir es añadir el
-- apunte contrario. Por eso las claves ajenas son RESTRICT y no CASCADE: no se
-- puede hacer desaparecer a alguien con dinero pendiente.

-- La consulta caliente es "qué queda por liquidar", así que el índice parcial
-- se ciñe a los apuntes pendientes.
CREATE INDEX IF NOT EXISTS ledger_pendientes
    ON ledger_entries (created_at)
    WHERE settlement_id IS NULL;

CREATE INDEX IF NOT EXISTS ledger_user ON ledger_entries (user_id, created_at);
CREATE INDEX IF NOT EXISTS ledger_counterparty ON ledger_entries (counterparty_id, created_at);

-- Un viaje no puede generar dos veces los mismos apuntes: es lo que impide
-- cobrar dos veces por completar el mismo trayecto.
CREATE UNIQUE INDEX IF NOT EXISTS ledger_un_apunte_por_reserva_y_tipo
    ON ledger_entries (booking_id, kind)
    WHERE booking_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS settlements (
    id                   TEXT PRIMARY KEY,
    period_start         TIMESTAMPTZ NOT NULL,
    period_end           TIMESTAMPTZ NOT NULL,
    fee_total_cents      BIGINT      NOT NULL,
    entries_settled      INTEGER     NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT settlements_periodo_con_sentido CHECK (period_end > period_start)
);
