package store

import (
	"context"
	"time"
)

// AceptarTerminos guarda qué redacción de las condiciones aceptó esa persona.
//
// Se guarda la versión y la fecha, no un booleano: "aceptó las condiciones" no
// acredita nada el día que alguien discuta a qué se comprometió exactamente.

func (m *Memory) AceptarTerminos(userID, version string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return ErrNotFound
	}
	cuando := at
	u.TerminosVersion, u.TerminosAt = version, &cuando
	return nil
}

func (p *Postgres) AceptarTerminos(userID, version string, at time.Time) error {
	tag, err := p.pool.Exec(context.Background(),
		`UPDATE users SET terminos_version = $2, terminos_at = $3 WHERE id = $1`,
		userID, version, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
