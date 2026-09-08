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

func (m *Memory) CrearCodigoCorreo(c *domain.CodigoCorreo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.codigos[c.ID]; ok {
		return errors.New("el código ya existe")
	}
	cp := *c
	m.codigos[c.ID] = &cp
	return nil
}

func (m *Memory) CodigoCorreoVivo(userID string) (*domain.CodigoCorreo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var vivos []*domain.CodigoCorreo
	for _, c := range m.codigos {
		if c.UserID == userID && c.UsadoAt == nil {
			vivos = append(vivos, c)
		}
	}
	if len(vivos) == 0 {
		return nil, ErrNotFound
	}
	// El más reciente: pedir otro código invalida el anterior.
	sort.Slice(vivos, func(i, j int) bool { return vivos[i].CreatedAt.After(vivos[j].CreatedAt) })
	cp := *vivos[0]
	return &cp, nil
}

func (m *Memory) AnotarIntentoCodigo(id string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.codigos[id]
	if !ok {
		return 0, ErrNotFound
	}
	c.Intentos++
	return c.Intentos, nil
}

func (m *Memory) UsarCodigoCorreo(id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.codigos[id]
	if !ok {
		return ErrNotFound
	}
	if c.UsadoAt != nil {
		return ErrCodigoGastado
	}
	cuando := at
	c.UsadoAt = &cuando
	return nil
}

func (m *Memory) AnularCodigosCorreo(userID string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.codigos {
		if c.UserID == userID && c.UsadoAt == nil {
			cuando := at
			c.UsadoAt = &cuando
		}
	}
	return nil
}

// ErrCodigoGastado se devuelve al usar dos veces el mismo código.
var ErrCodigoGastado = errors.New("ese código ya se ha usado")

// --- Postgres ---

func (p *Postgres) CrearCodigoCorreo(c *domain.CodigoCorreo) error {
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO codigos_correo (id, user_id, check_ref, codigo_hash, intentos, created_at, expira_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		c.ID, c.UserID, c.CheckRef, c.CodigoHash, c.Intentos, c.CreatedAt, c.ExpiraAt)
	return err
}

func (p *Postgres) CodigoCorreoVivo(userID string) (*domain.CodigoCorreo, error) {
	var c domain.CodigoCorreo
	err := p.pool.QueryRow(context.Background(), `
		SELECT id, user_id, check_ref, codigo_hash, intentos, created_at, expira_at, usado_at
		FROM codigos_correo
		WHERE user_id = $1 AND usado_at IS NULL
		ORDER BY created_at DESC LIMIT 1`, userID).
		Scan(&c.ID, &c.UserID, &c.CheckRef, &c.CodigoHash, &c.Intentos,
			&c.CreatedAt, &c.ExpiraAt, &c.UsadoAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// AnotarIntentoCodigo suma un intento y devuelve cuántos van.
//
// El incremento y la lectura van en la misma sentencia: contarlos leyendo y
// escribiendo por separado deja una ventana en la que varias peticiones
// simultáneas gastan un solo intento entre todas, que es justo lo que hace
// quien prueba códigos a lo bruto.
func (p *Postgres) AnotarIntentoCodigo(id string) (int, error) {
	var intentos int
	err := p.pool.QueryRow(context.Background(),
		`UPDATE codigos_correo SET intentos = intentos + 1 WHERE id = $1 RETURNING intentos`, id).
		Scan(&intentos)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return intentos, err
}

func (p *Postgres) UsarCodigoCorreo(id string, at time.Time) error {
	tag, err := p.pool.Exec(context.Background(),
		`UPDATE codigos_correo SET usado_at = $2 WHERE id = $1 AND usado_at IS NULL`, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCodigoGastado
	}
	return nil
}

func (p *Postgres) AnularCodigosCorreo(userID string, at time.Time) error {
	_, err := p.pool.Exec(context.Background(),
		`UPDATE codigos_correo SET usado_at = $2 WHERE user_id = $1 AND usado_at IS NULL`, userID, at)
	return err
}
