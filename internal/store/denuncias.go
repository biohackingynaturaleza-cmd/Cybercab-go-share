package store

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
)

// --- En memoria ---

func (m *Memory) CrearDenuncia(d *domain.Denuncia) error {
	if err := d.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.denuncias[d.ID]; ok {
		return errors.New("la denuncia ya existe")
	}
	cp := *d
	m.denuncias[d.ID] = &cp
	return nil
}

func (m *Memory) GetDenuncia(id string) (*domain.Denuncia, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.denuncias[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *d
	return &cp, nil
}

func (m *Memory) UpdateDenuncia(d *domain.Denuncia) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.denuncias[d.ID]; !ok {
		return ErrNotFound
	}
	cp := *d
	m.denuncias[d.ID] = &cp
	return nil
}

func (m *Memory) DenunciasAbiertas() ([]*domain.Denuncia, error) {
	return m.denunciasSi(func(d *domain.Denuncia) bool { return d.Estado == domain.DenunciaAbierta }), nil
}

func (m *Memory) DenunciasDe(userID string) ([]*domain.Denuncia, error) {
	return m.denunciasSi(func(d *domain.Denuncia) bool { return d.DenuncianteID == userID }), nil
}

func (m *Memory) DenunciasContra(userID string) ([]*domain.Denuncia, error) {
	return m.denunciasSi(func(d *domain.Denuncia) bool { return d.DenunciadoID == userID }), nil
}

func (m *Memory) denunciasSi(cumple func(*domain.Denuncia) bool) []*domain.Denuncia {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Denuncia
	for _, d := range m.denuncias {
		if cumple(d) {
			cp := *d
			out = append(out, &cp)
		}
	}
	// Lo urgente primero, y dentro de cada grupo lo más antiguo: una denuncia
	// de seguridad esperando turno detrás de veinte de conducta es una cola
	// mal ordenada.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Motivo.Urgente() != out[j].Motivo.Urgente() {
			return out[i].Motivo.Urgente()
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (m *Memory) SuspenderUsuario(userID string, hasta *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return ErrNotFound
	}
	if hasta == nil {
		u.SuspendidoHasta = nil
		return nil
	}
	cuando := *hasta
	u.SuspendidoHasta = &cuando
	return nil
}

// --- Postgres ---

func (p *Postgres) CrearDenuncia(d *domain.Denuncia) error {
	if err := d.Validate(); err != nil {
		return err
	}
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO denuncias (id, trip_id, denunciante_id, denunciado_id, motivo,
			descripcion, estado, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		d.ID, nuloSiVacio(d.TripID), d.DenuncianteID, d.DenunciadoID, d.Motivo,
		d.Descripcion, d.Estado, d.CreatedAt)
	return err
}

const selectDenuncia = `SELECT id, coalesce(trip_id, ''), denunciante_id, denunciado_id,
	motivo, descripcion, estado, created_at, coalesce(resuelta_at, 'epoch'), resolucion
	FROM denuncias`

func (p *Postgres) GetDenuncia(id string) (*domain.Denuncia, error) {
	var d domain.Denuncia
	err := p.pool.QueryRow(context.Background(), selectDenuncia+` WHERE id = $1`, id).
		Scan(&d.ID, &d.TripID, &d.DenuncianteID, &d.DenunciadoID, &d.Motivo,
			&d.Descripcion, &d.Estado, &d.CreatedAt, &d.ResueltaAt, &d.Resolucion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	limpiarEpoch(&d)
	return &d, nil
}

func (p *Postgres) UpdateDenuncia(d *domain.Denuncia) error {
	var resuelta *time.Time
	if !d.ResueltaAt.IsZero() {
		resuelta = &d.ResueltaAt
	}
	tag, err := p.pool.Exec(context.Background(), `
		UPDATE denuncias SET estado = $2, resuelta_at = $3, resolucion = $4 WHERE id = $1`,
		d.ID, d.Estado, resuelta, d.Resolucion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) DenunciasAbiertas() ([]*domain.Denuncia, error) {
	// Lo urgente primero: una denuncia de seguridad esperando turno detrás de
	// veinte de conducta es una cola mal ordenada.
	return p.denuncias(selectDenuncia + ` WHERE estado = 'abierta'
		ORDER BY motivo IN ('seguridad', 'identidad') DESC, created_at`)
}

func (p *Postgres) DenunciasDe(userID string) ([]*domain.Denuncia, error) {
	return p.denuncias(selectDenuncia+` WHERE denunciante_id = $1 ORDER BY created_at DESC`, userID)
}

func (p *Postgres) DenunciasContra(userID string) ([]*domain.Denuncia, error) {
	return p.denuncias(selectDenuncia+` WHERE denunciado_id = $1 ORDER BY created_at DESC`, userID)
}

func (p *Postgres) denuncias(sql string, args ...any) ([]*domain.Denuncia, error) {
	rows, err := p.pool.Query(context.Background(), sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.Denuncia
	for rows.Next() {
		var d domain.Denuncia
		if err := rows.Scan(&d.ID, &d.TripID, &d.DenuncianteID, &d.DenunciadoID, &d.Motivo,
			&d.Descripcion, &d.Estado, &d.CreatedAt, &d.ResueltaAt, &d.Resolucion); err != nil {
			return nil, err
		}
		limpiarEpoch(&d)
		out = append(out, &d)
	}
	return out, rows.Err()
}

// limpiarEpoch devuelve a cero la fecha de resolución de las denuncias que
// siguen abiertas: el coalesce de la consulta las trae como el año 1970.
func limpiarEpoch(d *domain.Denuncia) {
	if d.ResueltaAt.Year() <= 1970 {
		d.ResueltaAt = time.Time{}
	}
}

func nuloSiVacio(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (p *Postgres) SuspenderUsuario(userID string, hasta *time.Time) error {
	tag, err := p.pool.Exec(context.Background(),
		`UPDATE users SET suspendido_hasta = $2 WHERE id = $1`, userID, hasta)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
