package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
)

// ErrRecuperacionUsada se devuelve al intentar gastar dos veces el mismo
// enlace. Es un caso distinto de "no existe": significa que alguien ya cambió
// la contraseña con él.
var ErrRecuperacionUsada = errors.New("ese enlace ya se ha usado")

// --- En memoria ---

func (m *Memory) CrearRecuperacion(r *domain.Recuperacion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.recuperaciones[r.ID]; ok {
		return errors.New("la recuperación ya existe")
	}
	cp := *r
	m.recuperaciones[r.ID] = &cp
	return nil
}

func (m *Memory) RecuperacionPorHash(hash string) (*domain.Recuperacion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.recuperaciones {
		if r.TokenHash == hash {
			cp := *r
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) UsarRecuperacion(id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.recuperaciones[id]
	if !ok {
		return ErrNotFound
	}
	if r.UsadaAt != nil {
		return ErrRecuperacionUsada
	}
	cuando := at
	r.UsadaAt = &cuando
	return nil
}

// AnularRecuperaciones invalida los enlaces vivos de una persona. Se llama tras
// cambiar la contraseña: los enlaces que quedaran por ahí dejan de servir en el
// mismo instante.
func (m *Memory) AnularRecuperaciones(userID string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.recuperaciones {
		if r.UserID == userID && r.UsadaAt == nil {
			cuando := at
			r.UsadaAt = &cuando
		}
	}
	return nil
}

func (m *Memory) CambiarContrasena(userID, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return ErrNotFound
	}
	u.PasswordHash = hash
	return nil
}

// --- Postgres ---

func (p *Postgres) CrearRecuperacion(r *domain.Recuperacion) error {
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO password_resets (id, user_id, token_hash, created_at, expira_at)
		VALUES ($1, $2, $3, $4, $5)`,
		r.ID, r.UserID, r.TokenHash, r.CreatedAt, r.ExpiraAt)
	return err
}

func (p *Postgres) RecuperacionPorHash(hash string) (*domain.Recuperacion, error) {
	var r domain.Recuperacion
	err := p.pool.QueryRow(context.Background(), `
		SELECT id, user_id, token_hash, created_at, expira_at, usada_at
		FROM password_resets WHERE token_hash = $1`, hash).
		Scan(&r.ID, &r.UserID, &r.TokenHash, &r.CreatedAt, &r.ExpiraAt, &r.UsadaAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// UsarRecuperacion marca el enlace como gastado en una sola sentencia.
//
// La condición va en el WHERE a propósito: comprobar y luego escribir deja una
// ventana en la que dos peticiones simultáneas gastan el mismo enlace.
func (p *Postgres) UsarRecuperacion(id string, at time.Time) error {
	tag, err := p.pool.Exec(context.Background(), `
		UPDATE password_resets SET usada_at = $2
		WHERE id = $1 AND usada_at IS NULL`, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// O no existe, o ya estaba usada. Distinguirlo cuesta otra consulta y
		// no cambia nada: en los dos casos el enlace no sirve.
		return ErrRecuperacionUsada
	}
	return nil
}

func (p *Postgres) AnularRecuperaciones(userID string, at time.Time) error {
	_, err := p.pool.Exec(context.Background(), `
		UPDATE password_resets SET usada_at = $2
		WHERE user_id = $1 AND usada_at IS NULL`, userID, at)
	return err
}

func (p *Postgres) CambiarContrasena(userID, hash string) error {
	tag, err := p.pool.Exec(context.Background(),
		`UPDATE users SET password_hash = $2 WHERE id = $1`, userID, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
