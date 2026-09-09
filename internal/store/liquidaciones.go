package store

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/billing"
)

// ErrPeriodoYaLiquidado se devuelve al liquidar un periodo que ya se liquidó.
// Es la barrera que impide que dos ejecuciones del programador cobren el mismo
// mes dos veces.
var ErrPeriodoYaLiquidado = errors.New("ese periodo ya está liquidado")

// --- En memoria ---

func (m *Memory) GuardarLiquidacion(l *billing.Liquidacion, apuntes []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, ya := range m.liquidaciones {
		if ya.Desde.Equal(l.Desde) && ya.Hasta.Equal(l.Hasta) {
			return ErrPeriodoYaLiquidado
		}
	}
	// Los apuntes se comprueban antes de escribir nada: si alguno ya estaba
	// liquidado, la liquidación entera se descarta.
	for _, id := range apuntes {
		e, ok := m.entries[id]
		if !ok {
			return ErrNotFound
		}
		if !e.Pendiente() {
			return ErrPeriodoYaLiquidado
		}
	}
	for _, id := range apuntes {
		m.entries[id].SettlementID = l.ID
	}

	cp := *l
	cp.Posiciones = append([]billing.Posicion(nil), l.Posiciones...)
	cp.Instrucciones = append([]billing.Instruccion(nil), l.Instrucciones...)
	m.liquidaciones[l.ID] = &cp
	return nil
}

func (m *Memory) GetLiquidacion(id string) (*billing.Liquidacion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	l, ok := m.liquidaciones[id]
	if !ok {
		return nil, ErrNotFound
	}
	return copiaLiquidacion(l), nil
}

func (m *Memory) Liquidaciones() ([]*billing.Liquidacion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*billing.Liquidacion, 0, len(m.liquidaciones))
	for _, l := range m.liquidaciones {
		out = append(out, copiaLiquidacion(l))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Desde.After(out[j].Desde) })
	return out, nil
}

func (m *Memory) InstruccionesDe(userID string) ([]billing.Instruccion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []billing.Instruccion
	for _, l := range m.liquidaciones {
		for _, i := range l.Instrucciones {
			if i.UserID == userID {
				out = append(out, i)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (m *Memory) ActualizarInstruccion(liquidacionID string, in billing.Instruccion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.liquidaciones[liquidacionID]
	if !ok {
		return ErrNotFound
	}
	for i := range l.Instrucciones {
		if l.Instrucciones[i].ID == in.ID {
			l.Instrucciones[i] = in
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) ActualizarEstadoLiquidacion(id string, estado billing.EstadoLiquidacion, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.liquidaciones[id]
	if !ok {
		return ErrNotFound
	}
	l.Estado = estado
	if estado == billing.LiquidacionEjecutada {
		l.EjecutadaAt = at
	}
	return nil
}

func copiaLiquidacion(l *billing.Liquidacion) *billing.Liquidacion {
	cp := *l
	cp.Posiciones = append([]billing.Posicion(nil), l.Posiciones...)
	cp.Instrucciones = append([]billing.Instruccion(nil), l.Instrucciones...)
	return &cp
}

// --- Postgres ---

// GuardarLiquidacion escribe la liquidación, sus instrucciones y el cierre de
// sus apuntes en una sola transacción.
//
// Las tres cosas o ninguna: una liquidación guardada cuyos apuntes siguieran
// abiertos los cobraría otra vez el mes siguiente, y unos apuntes cerrados sin
// liquidación serían dinero que nadie va a reclamar.
func (p *Postgres) GuardarLiquidacion(l *billing.Liquidacion, apuntes []string) error {
	ctx := context.Background()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO liquidaciones (id, desde, hasta, comision_total_cents,
			apuntes_liquidados, estado, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		l.ID, l.Desde, l.Hasta, l.ComisionTotalCents, l.ApuntesLiquidados,
		string(l.Estado), l.CreatedAt); err != nil {
		if esViolacionUnica(err, "liquidaciones_un_periodo") {
			return ErrPeriodoYaLiquidado
		}
		return err
	}

	for _, in := range l.Instrucciones {
		if _, err := tx.Exec(ctx, `
			INSERT INTO instrucciones_liquidacion (id, liquidacion_id, user_id, tipo,
				amount_cents, estado, ref, motivo, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			in.ID, l.ID, in.UserID, string(in.Tipo), in.AmountCents,
			in.Estado, in.Ref, in.Motivo, l.CreatedAt); err != nil {
			return err
		}
	}

	// El cierre de los apuntes lleva la condición en el WHERE: si otro los
	// cerró mientras tanto, aquí se marcan menos de los esperados y toda la
	// transacción se cae.
	tag, err := tx.Exec(ctx, `
		UPDATE ledger_entries SET settlement_id = $2
		WHERE id = ANY($1) AND settlement_id IS NULL`, apuntes, l.ID)
	if err != nil {
		return err
	}
	if int(tag.RowsAffected()) != len(apuntes) {
		return ErrPeriodoYaLiquidado
	}
	return tx.Commit(ctx)
}

const selectLiquidacion = `SELECT id, desde, hasta, comision_total_cents,
	apuntes_liquidados, estado, created_at, coalesce(ejecutada_at, 'epoch')
	FROM liquidaciones`

func (p *Postgres) GetLiquidacion(id string) (*billing.Liquidacion, error) {
	ls, err := p.liquidaciones(selectLiquidacion+` WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	if len(ls) == 0 {
		return nil, ErrNotFound
	}
	return ls[0], nil
}

func (p *Postgres) Liquidaciones() ([]*billing.Liquidacion, error) {
	return p.liquidaciones(selectLiquidacion + ` ORDER BY desde DESC`)
}

func (p *Postgres) liquidaciones(sql string, args ...any) ([]*billing.Liquidacion, error) {
	rows, err := p.pool.Query(context.Background(), sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*billing.Liquidacion
	for rows.Next() {
		var l billing.Liquidacion
		var estado string
		if err := rows.Scan(&l.ID, &l.Desde, &l.Hasta, &l.ComisionTotalCents,
			&l.ApuntesLiquidados, &estado, &l.CreatedAt, &l.EjecutadaAt); err != nil {
			return nil, err
		}
		l.Estado = billing.EstadoLiquidacion(estado)
		if l.EjecutadaAt.Year() <= 1970 {
			l.EjecutadaAt = time.Time{}
		}
		out = append(out, &l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Las instrucciones se traen aparte: son pocas por liquidación y así la
	// consulta principal no se convierte en una unión que hay que desdoblar.
	for _, l := range out {
		if l.Instrucciones, err = p.instrucciones(
			`WHERE liquidacion_id = $1 ORDER BY user_id`, l.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

const selectInstruccion = `SELECT id, user_id, tipo, amount_cents, estado, ref, motivo,
	coalesce(ejecutada_at, 'epoch') FROM instrucciones_liquidacion `

func (p *Postgres) InstruccionesDe(userID string) ([]billing.Instruccion, error) {
	return p.instrucciones(`WHERE user_id = $1 ORDER BY created_at DESC`, userID)
}

func (p *Postgres) instrucciones(donde string, args ...any) ([]billing.Instruccion, error) {
	rows, err := p.pool.Query(context.Background(), selectInstruccion+donde, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []billing.Instruccion
	for rows.Next() {
		var in billing.Instruccion
		var tipo string
		if err := rows.Scan(&in.ID, &in.UserID, &tipo, &in.AmountCents,
			&in.Estado, &in.Ref, &in.Motivo, &in.EjecutadaAt); err != nil {
			return nil, err
		}
		in.Tipo = billing.TipoInstruccion(tipo)
		if in.EjecutadaAt.Year() <= 1970 {
			in.EjecutadaAt = time.Time{}
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func (p *Postgres) ActualizarInstruccion(_ string, in billing.Instruccion) error {
	var ejecutada *time.Time
	if !in.EjecutadaAt.IsZero() {
		ejecutada = &in.EjecutadaAt
	}
	tag, err := p.pool.Exec(context.Background(), `
		UPDATE instrucciones_liquidacion
		SET estado = $2, ref = $3, motivo = $4, ejecutada_at = $5
		WHERE id = $1`, in.ID, in.Estado, in.Ref, in.Motivo, ejecutada)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) ActualizarEstadoLiquidacion(id string, estado billing.EstadoLiquidacion, at time.Time) error {
	var ejecutada *time.Time
	if estado == billing.LiquidacionEjecutada {
		ejecutada = &at
	}
	tag, err := p.pool.Exec(context.Background(),
		`UPDATE liquidaciones SET estado = $2, ejecutada_at = $3 WHERE id = $1`,
		id, string(estado), ejecutada)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
