package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
)

// ErrContactoRepetido se devuelve al añadir dos veces la misma dirección: dos
// avisos idénticos no avisan más.
var ErrContactoRepetido = errors.New("ya tienes a esa persona como contacto")

// --- Contactos, en memoria ---

func (m *Memory) CrearContacto(c *domain.ContactoDeConfianza) error {
	if err := c.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ya := range m.contactos {
		if ya.UserID == c.UserID && igualSinMayusculas(ya.Email, c.Email) {
			return ErrContactoRepetido
		}
	}
	cp := *c
	m.contactos[c.ID] = &cp
	return nil
}

func (m *Memory) ContactosDe(userID string) ([]*domain.ContactoDeConfianza, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.ContactoDeConfianza
	for _, c := range m.contactos {
		if c.UserID == userID {
			cp := *c
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) BorrarContacto(id, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.contactos[id]
	if !ok || c.UserID != userID {
		return ErrNotFound
	}
	delete(m.contactos, id)
	return nil
}

// --- Seguimientos, en memoria ---

func (m *Memory) CrearSeguimiento(s *domain.Seguimiento) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Los anteriores siguen vivos: ver el comentario de la migración.
	cp := *s
	m.seguimientos[s.ID] = &cp
	return nil
}

func (m *Memory) SeguimientoPorHash(hash string) (*domain.Seguimiento, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.seguimientos {
		if s.TokenHash == hash {
			cp := *s
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

// SeguimientosVivos devuelve todos los enlaces abiertos de esa persona en ese
// viaje, el más reciente primero.
func (m *Memory) SeguimientosVivos(tripID, userID string) ([]*domain.Seguimiento, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Seguimiento
	for _, s := range m.seguimientos {
		if s.TripID == tripID && s.UserID == userID && s.RevocadoAt == nil {
			cp := *s
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// RevocarSeguimientos cierra todos los enlaces de esa persona en ese viaje.
// Cerrar el seguimiento es dejar de compartir, no cerrar uno de varios.
func (m *Memory) RevocarSeguimientos(tripID, userID string, at time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int
	for _, s := range m.seguimientos {
		if s.TripID == tripID && s.UserID == userID && s.RevocadoAt == nil {
			cuando := at
			s.RevocadoAt = &cuando
			n++
		}
	}
	return n, nil
}

// ApuntarPosicion la escribe en todos los enlaces vivos: la posición es una
// sola, y quien tenga cualquiera de los enlaces tiene que verla.
func (m *Memory) ApuntarPosicion(tripID, userID string, p geo.Point, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.seguimientos {
		if s.TripID == tripID && s.UserID == userID && s.RevocadoAt == nil {
			punto, cuando := p, at
			s.UltimaPosicion, s.UltimaPosicionAt = &punto, &cuando
		}
	}
	return nil
}

// --- Alertas, en memoria ---

func (m *Memory) CrearAlerta(a *domain.Alerta) error {
	if err := a.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *a
	m.alertas[a.ID] = &cp
	return nil
}

func (m *Memory) GetAlerta(id string) (*domain.Alerta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.alertas[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *Memory) UpdateAlerta(a *domain.Alerta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.alertas[a.ID]; !ok {
		return ErrNotFound
	}
	cp := *a
	m.alertas[a.ID] = &cp
	return nil
}

func (m *Memory) AlertasAbiertas() ([]*domain.Alerta, error) {
	return m.alertasSi(func(a *domain.Alerta) bool { return a.Estado == domain.AlertaAbierta }), nil
}

func (m *Memory) AlertaVivaDe(tripID, userID string) (*domain.Alerta, error) {
	for _, a := range m.alertasSi(func(a *domain.Alerta) bool {
		return a.TripID == tripID && a.UserID == userID && a.Viva()
	}) {
		return a, nil
	}
	return nil, ErrNotFound
}

func (m *Memory) AlertasDeViaje(tripID string) ([]*domain.Alerta, error) {
	return m.alertasSi(func(a *domain.Alerta) bool { return a.TripID == tripID }), nil
}

func (m *Memory) alertasSi(cumple func(*domain.Alerta) bool) []*domain.Alerta {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Alerta
	for _, a := range m.alertas {
		if cumple(a) {
			cp := *a
			out = append(out, &cp)
		}
	}
	// La más reciente primero: en una emergencia lo último que pasó es lo que
	// importa.
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func igualSinMayusculas(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// --- Postgres ---

func (p *Postgres) CrearContacto(c *domain.ContactoDeConfianza) error {
	if err := c.Validate(); err != nil {
		return err
	}
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO contactos_confianza (id, user_id, nombre, email, avisar_al_salir, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		c.ID, c.UserID, c.Nombre, c.Email, c.AvisarAlSalir, c.CreatedAt)
	if esViolacionUnica(err, "contactos_sin_repetir") {
		return ErrContactoRepetido
	}
	return err
}

func (p *Postgres) ContactosDe(userID string) ([]*domain.ContactoDeConfianza, error) {
	rows, err := p.pool.Query(context.Background(), `
		SELECT id, user_id, nombre, email, avisar_al_salir, created_at
		FROM contactos_confianza WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.ContactoDeConfianza
	for rows.Next() {
		var c domain.ContactoDeConfianza
		if err := rows.Scan(&c.ID, &c.UserID, &c.Nombre, &c.Email, &c.AvisarAlSalir, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// BorrarContacto exige el dueño en el WHERE: comprobarlo antes con una consulta
// aparte deja la puerta abierta a borrar el contacto de otro entre las dos.
func (p *Postgres) BorrarContacto(id, userID string) error {
	tag, err := p.pool.Exec(context.Background(),
		`DELETE FROM contactos_confianza WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) CrearSeguimiento(s *domain.Seguimiento) error {
	// Los anteriores siguen vivos: ver el comentario de la migración.
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO seguimientos (id, trip_id, user_id, token_hash, created_at, expira_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		s.ID, s.TripID, s.UserID, s.TokenHash, s.CreatedAt, s.ExpiraAt)
	return err
}

const selectSeguimiento = `SELECT id, trip_id, user_id, token_hash, created_at, expira_at,
	revocado_at, ultima_lat, ultima_lng, ultima_posicion_at FROM seguimientos`

func (p *Postgres) SeguimientoPorHash(hash string) (*domain.Seguimiento, error) {
	return p.seguimiento(selectSeguimiento+` WHERE token_hash = $1`, hash)
}

func (p *Postgres) SeguimientosVivos(tripID, userID string) ([]*domain.Seguimiento, error) {
	rows, err := p.pool.Query(context.Background(), selectSeguimiento+
		` WHERE trip_id = $1 AND user_id = $2 AND revocado_at IS NULL ORDER BY created_at DESC`,
		tripID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.Seguimiento
	for rows.Next() {
		s, err := escanearSeguimiento(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) seguimiento(sql string, args ...any) (*domain.Seguimiento, error) {
	s, err := escanearSeguimiento(p.pool.QueryRow(context.Background(), sql, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

type escaneable interface{ Scan(dest ...any) error }

func escanearSeguimiento(fila escaneable) (*domain.Seguimiento, error) {
	var s domain.Seguimiento
	var lat, lng *float64
	if err := fila.Scan(&s.ID, &s.TripID, &s.UserID, &s.TokenHash, &s.CreatedAt,
		&s.ExpiraAt, &s.RevocadoAt, &lat, &lng, &s.UltimaPosicionAt); err != nil {
		return nil, err
	}
	if lat != nil && lng != nil {
		s.UltimaPosicion = &geo.Point{Lat: *lat, Lng: *lng}
	}
	return &s, nil
}

func (p *Postgres) RevocarSeguimientos(tripID, userID string, at time.Time) (int, error) {
	tag, err := p.pool.Exec(context.Background(), `
		UPDATE seguimientos SET revocado_at = $3
		WHERE trip_id = $1 AND user_id = $2 AND revocado_at IS NULL`, tripID, userID, at)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (p *Postgres) ApuntarPosicion(tripID, userID string, punto geo.Point, at time.Time) error {
	_, err := p.pool.Exec(context.Background(), `
		UPDATE seguimientos SET ultima_lat = $3, ultima_lng = $4, ultima_posicion_at = $5
		WHERE trip_id = $1 AND user_id = $2 AND revocado_at IS NULL`,
		tripID, userID, punto.Lat, punto.Lng, at)
	return err
}

func (p *Postgres) CrearAlerta(a *domain.Alerta) error {
	if err := a.Validate(); err != nil {
		return err
	}
	var lat, lng *float64
	if a.Posicion != nil {
		lat, lng = &a.Posicion.Lat, &a.Posicion.Lng
	}
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO alertas (id, trip_id, user_id, lat, lng, nota, estado, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		a.ID, a.TripID, a.UserID, lat, lng, a.Nota, a.Estado, a.CreatedAt)
	return err
}

const selectAlerta = `SELECT id, trip_id, user_id, lat, lng, nota, estado, created_at,
	coalesce(resuelta_at, 'epoch'), resolucion FROM alertas`

func (p *Postgres) GetAlerta(id string) (*domain.Alerta, error) {
	as, err := p.alertas(selectAlerta+` WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	if len(as) == 0 {
		return nil, ErrNotFound
	}
	return as[0], nil
}

func (p *Postgres) UpdateAlerta(a *domain.Alerta) error {
	var resuelta *time.Time
	if !a.ResueltaAt.IsZero() {
		resuelta = &a.ResueltaAt
	}
	tag, err := p.pool.Exec(context.Background(),
		`UPDATE alertas SET estado = $2, resuelta_at = $3, resolucion = $4 WHERE id = $1`,
		a.ID, a.Estado, resuelta, a.Resolucion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) AlertasAbiertas() ([]*domain.Alerta, error) {
	return p.alertas(selectAlerta + ` WHERE estado = 'abierta' ORDER BY created_at DESC`)
}

func (p *Postgres) AlertaVivaDe(tripID, userID string) (*domain.Alerta, error) {
	as, err := p.alertas(selectAlerta+
		` WHERE trip_id = $1 AND user_id = $2 AND estado = 'abierta' ORDER BY created_at DESC LIMIT 1`,
		tripID, userID)
	if err != nil {
		return nil, err
	}
	if len(as) == 0 {
		return nil, ErrNotFound
	}
	return as[0], nil
}

func (p *Postgres) AlertasDeViaje(tripID string) ([]*domain.Alerta, error) {
	return p.alertas(selectAlerta+` WHERE trip_id = $1 ORDER BY created_at DESC`, tripID)
}

func (p *Postgres) alertas(sql string, args ...any) ([]*domain.Alerta, error) {
	rows, err := p.pool.Query(context.Background(), sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.Alerta
	for rows.Next() {
		var a domain.Alerta
		var lat, lng *float64
		if err := rows.Scan(&a.ID, &a.TripID, &a.UserID, &lat, &lng, &a.Nota,
			&a.Estado, &a.CreatedAt, &a.ResueltaAt, &a.Resolucion); err != nil {
			return nil, err
		}
		if lat != nil && lng != nil {
			a.Posicion = &geo.Point{Lat: *lat, Lng: *lng}
		}
		if a.ResueltaAt.Year() <= 1970 {
			a.ResueltaAt = time.Time{}
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}
