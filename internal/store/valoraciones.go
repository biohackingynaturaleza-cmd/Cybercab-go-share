package store

import (
	"context"
	"errors"
	"sort"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
)

// ErrYaValorado se devuelve al valorar dos veces la misma reserva. Cada parte
// opina una vez: poder reescribir la nota convertiría la valoración en una
// moneda de cambio ("súbeme la mía y te subo la tuya").
var ErrYaValorado = errors.New("ya has valorado este viaje")

// --- En memoria ---

func (m *Memory) CrearValoracion(v *domain.Valoracion) error {
	if err := v.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ya := range m.valoraciones {
		if ya.BookingID == v.BookingID && ya.AutorID == v.AutorID {
			return ErrYaValorado
		}
	}
	cp := *v
	m.valoraciones[v.ID] = &cp
	return nil
}

func (m *Memory) ValoracionesRecibidas(userID string) ([]*domain.Valoracion, error) {
	return m.valoracionesSi(func(v *domain.Valoracion) bool { return v.SobreID == userID }), nil
}

func (m *Memory) ValoracionesEmitidas(userID string) ([]*domain.Valoracion, error) {
	return m.valoracionesSi(func(v *domain.Valoracion) bool { return v.AutorID == userID }), nil
}

func (m *Memory) ValoracionesDeBooking(bookingID string) ([]*domain.Valoracion, error) {
	return m.valoracionesSi(func(v *domain.Valoracion) bool { return v.BookingID == bookingID }), nil
}

func (m *Memory) valoracionesSi(cumple func(*domain.Valoracion) bool) []*domain.Valoracion {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Valoracion
	for _, v := range m.valoraciones {
		if cumple(v) {
			cp := *v
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Memory) IncrementarViajes(userIDs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range userIDs {
		if u, ok := m.users[id]; ok {
			u.RideCount++
		}
	}
	return nil
}

// --- Postgres ---

func (p *Postgres) CrearValoracion(v *domain.Valoracion) error {
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO valoraciones (id, booking_id, trip_id, autor_id, sobre_id, estrellas, comentario, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		v.ID, v.BookingID, v.TripID, v.AutorID, v.SobreID, v.Estrellas, v.Comentario, v.CreatedAt)
	if esViolacionUnica(err, "valoraciones_una_por_parte") {
		return ErrYaValorado
	}
	return err
}

const selectValoracion = `SELECT id, booking_id, trip_id, autor_id, sobre_id, estrellas, comentario, created_at
	FROM valoraciones`

func (p *Postgres) ValoracionesRecibidas(userID string) ([]*domain.Valoracion, error) {
	return p.valoraciones(selectValoracion+` WHERE sobre_id = $1 ORDER BY created_at DESC`, userID)
}

func (p *Postgres) ValoracionesEmitidas(userID string) ([]*domain.Valoracion, error) {
	return p.valoraciones(selectValoracion+` WHERE autor_id = $1 ORDER BY created_at DESC`, userID)
}

func (p *Postgres) ValoracionesDeBooking(bookingID string) ([]*domain.Valoracion, error) {
	return p.valoraciones(selectValoracion+` WHERE booking_id = $1 ORDER BY created_at`, bookingID)
}

func (p *Postgres) valoraciones(sql string, args ...any) ([]*domain.Valoracion, error) {
	rows, err := p.pool.Query(context.Background(), sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.Valoracion
	for rows.Next() {
		var v domain.Valoracion
		if err := rows.Scan(&v.ID, &v.BookingID, &v.TripID, &v.AutorID, &v.SobreID,
			&v.Estrellas, &v.Comentario, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &v)
	}
	return out, rows.Err()
}

// IncrementarViajes suma un viaje completado a cada participante.
//
// Va en una sola sentencia para todos: cerrar un trayecto tiene que dejar el
// historial de todos los que iban dentro coherente, o no dejarlo.
func (p *Postgres) IncrementarViajes(userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	_, err := p.pool.Exec(context.Background(),
		`UPDATE users SET ride_count = ride_count + 1 WHERE id = ANY($1)`, userIDs)
	return err
}
