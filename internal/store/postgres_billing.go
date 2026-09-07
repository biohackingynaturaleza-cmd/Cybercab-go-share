package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/billing"
)

// CreateEntries escribe los apuntes de una vez, en una transacción: unos
// apuntes a medias dejarían una deuda sin su comisión, o al revés.
func (p *Postgres) CreateEntries(entries []billing.Entry) error {
	ctx := context.Background()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, e := range entries {
		if err := e.Validate(); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO ledger_entries (
				id, user_id, trip_id, booking_id, kind,
				amount_cents, counterparty_id, created_at, settlement_id
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			e.ID, e.UserID, e.TripID, nullString(e.BookingID), string(e.Kind),
			e.AmountCents, nullString(e.CounterpartyID), e.CreatedAt, nullString(e.SettlementID))
		if esViolacionUnica(err, "ledger_un_apunte_por_reserva_y_tipo") ||
			esViolacionUnica(err, "ledger_entries_pkey") {
			return ErrApuntesDuplicados
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

const selectEntry = `
	SELECT id, user_id, trip_id, COALESCE(booking_id, ''), kind, amount_cents,
	       COALESCE(counterparty_id, ''), created_at, COALESCE(settlement_id, '')
	FROM ledger_entries`

func (p *Postgres) PendingEntries() ([]billing.Entry, error) {
	rows, err := p.pool.Query(context.Background(),
		selectEntry+` WHERE settlement_id IS NULL ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	return scanEntries(rows)
}

// MarkSettled cierra los apuntes indicados. La condición sobre settlement_id
// hace que un cierre concurrente no pueda pisar al otro: el segundo no
// encuentra filas que marcar.
func (p *Postgres) MarkSettled(entryIDs []string, settlementID string) error {
	_, err := p.pool.Exec(context.Background(), `
		UPDATE ledger_entries SET settlement_id = $2
		WHERE id = ANY($1) AND settlement_id IS NULL`,
		entryIDs, settlementID)
	return err
}

func scanEntries(rows pgx.Rows) ([]billing.Entry, error) {
	defer rows.Close()
	var out []billing.Entry
	for rows.Next() {
		var (
			e    billing.Entry
			kind string
		)
		if err := rows.Scan(&e.ID, &e.UserID, &e.TripID, &e.BookingID, &kind,
			&e.AmountCents, &e.CounterpartyID, &e.CreatedAt, &e.SettlementID); err != nil {
			return nil, err
		}
		e.Kind = billing.EntryKind(kind)
		e.CreatedAt = e.CreatedAt.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}
