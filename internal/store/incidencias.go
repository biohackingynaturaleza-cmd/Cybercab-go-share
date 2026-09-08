package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
)

// ErrIncidenciaEnCurso se devuelve al declarar una incidencia contra alguien
// que ya tiene otra viva en el mismo trayecto.
var ErrIncidenciaEnCurso = errors.New("ya hay una incidencia sin resolver contra esa persona en este trayecto")

// --- En memoria ---

func (m *Memory) CreateIncidencia(i *domain.Incidencia) error {
	if err := i.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.incidencias[i.ID]; ok {
		return errors.New("la incidencia ya existe")
	}
	for _, ya := range m.incidencias {
		if ya.TripID == i.TripID && ya.AtribuidaA == i.AtribuidaA && ya.Viva() {
			return ErrIncidenciaEnCurso
		}
	}
	cp := *i
	m.incidencias[i.ID] = &cp
	return nil
}

func (m *Memory) GetIncidencia(id string) (*domain.Incidencia, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	i, ok := m.incidencias[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *i
	return &cp, nil
}

func (m *Memory) UpdateIncidencia(i *domain.Incidencia) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.incidencias[i.ID]; !ok {
		return ErrNotFound
	}
	cp := *i
	m.incidencias[i.ID] = &cp
	return nil
}

func (m *Memory) IncidenciasDe(userID string) ([]*domain.Incidencia, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Incidencia
	for _, i := range m.incidencias {
		if i.AtribuidaA == userID || i.DeclaranteID == userID {
			cp := *i
			out = append(out, &cp)
		}
	}
	sortIncidencias(out)
	return out, nil
}

// --- Postgres ---

func (p *Postgres) CreateIncidencia(i *domain.Incidencia) error {
	if err := i.Validate(); err != nil {
		return err
	}
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO incidencias (
			id, trip_id, declarante_id, atribuida_a, tipo,
			importe_cents, descripcion, estado, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		i.ID, i.TripID, i.DeclaranteID, i.AtribuidaA, string(i.Tipo),
		i.ImporteCents, i.Descripcion, string(i.Estado), i.CreatedAt)
	if esViolacionUnica(err, "incidencias_una_viva") {
		return ErrIncidenciaEnCurso
	}
	return err
}

const selectIncidencia = `
	SELECT id, trip_id, declarante_id, atribuida_a, tipo, importe_cents,
	       descripcion, estado, created_at, resuelta_at
	FROM incidencias`

func (p *Postgres) GetIncidencia(id string) (*domain.Incidencia, error) {
	rows, err := p.pool.Query(context.Background(), selectIncidencia+` WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	out, err := scanIncidencias(rows)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out[0], nil
}

func (p *Postgres) UpdateIncidencia(i *domain.Incidencia) error {
	tag, err := p.pool.Exec(context.Background(), `
		UPDATE incidencias SET estado = $2, resuelta_at = $3 WHERE id = $1`,
		i.ID, string(i.Estado), nullTime(i.ResueltaAt))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) IncidenciasDe(userID string) ([]*domain.Incidencia, error) {
	rows, err := p.pool.Query(context.Background(),
		selectIncidencia+` WHERE atribuida_a = $1 OR declarante_id = $1 ORDER BY created_at DESC`,
		userID)
	if err != nil {
		return nil, err
	}
	return scanIncidencias(rows)
}

func scanIncidencias(rows pgx.Rows) ([]*domain.Incidencia, error) {
	defer rows.Close()
	var out []*domain.Incidencia
	for rows.Next() {
		var (
			i          domain.Incidencia
			tipo       string
			estado     string
			resueltaAt *time.Time
		)
		if err := rows.Scan(&i.ID, &i.TripID, &i.DeclaranteID, &i.AtribuidaA, &tipo,
			&i.ImporteCents, &i.Descripcion, &estado, &i.CreatedAt, &resueltaAt); err != nil {
			return nil, err
		}
		i.Tipo = domain.TipoIncidencia(tipo)
		i.Estado = domain.EstadoIncidencia(estado)
		i.CreatedAt = i.CreatedAt.UTC()
		if resueltaAt != nil {
			i.ResueltaAt = resueltaAt.UTC()
		}
		out = append(out, &i)
	}
	return out, rows.Err()
}
