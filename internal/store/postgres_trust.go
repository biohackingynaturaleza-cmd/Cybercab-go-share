package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// --- Comprobaciones de identidad ---

func (p *Postgres) CreateCheck(c *trust.Check) error {
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO identity_checks (
			id, user_id, kind, status, provider_ref,
			created_at, verified_at, expires_at, rejection_reason
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		c.ID, c.UserID, string(c.Kind), string(c.Status), nullString(c.ProviderRef),
		c.CreatedAt, nullTime(c.VerifiedAt), nullTime(c.ExpiresAt), c.RejectionReason)
	if esViolacionUnica(err, "identity_checks_una_viva_por_tipo") {
		return ErrComprobacionEnCurso
	}
	return err
}

func (p *Postgres) UpdateCheck(c *trust.Check) error {
	tag, err := p.pool.Exec(context.Background(), `
		UPDATE identity_checks SET
			status = $2, provider_ref = $3, verified_at = $4,
			expires_at = $5, rejection_reason = $6
		WHERE id = $1`,
		c.ID, string(c.Status), nullString(c.ProviderRef),
		nullTime(c.VerifiedAt), nullTime(c.ExpiresAt), c.RejectionReason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const selectCheck = `
	SELECT id, user_id, kind, status, COALESCE(provider_ref, ''),
	       created_at, verified_at, expires_at, rejection_reason
	FROM identity_checks`

func (p *Postgres) GetCheckByRef(providerRef string) (*trust.Check, error) {
	rows, err := p.pool.Query(context.Background(),
		selectCheck+` WHERE provider_ref = $1`, providerRef)
	if err != nil {
		return nil, err
	}
	checks, err := scanChecks(rows)
	if err != nil {
		return nil, err
	}
	if len(checks) == 0 {
		return nil, ErrNotFound
	}
	return &checks[0], nil
}

func (p *Postgres) ChecksByUser(userID string) ([]trust.Check, error) {
	rows, err := p.pool.Query(context.Background(),
		selectCheck+` WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	return scanChecks(rows)
}

func scanChecks(rows pgx.Rows) ([]trust.Check, error) {
	defer rows.Close()
	var out []trust.Check
	for rows.Next() {
		var (
			c          trust.Check
			kind       string
			status     string
			verifiedAt *time.Time
			expiresAt  *time.Time
		)
		if err := rows.Scan(&c.ID, &c.UserID, &kind, &status, &c.ProviderRef,
			&c.CreatedAt, &verifiedAt, &expiresAt, &c.RejectionReason); err != nil {
			return nil, err
		}
		c.Kind = trust.CheckKind(kind)
		c.Status = trust.CheckStatus(status)
		c.CreatedAt = c.CreatedAt.UTC()
		if verifiedAt != nil {
			c.VerifiedAt = verifiedAt.UTC()
		}
		if expiresAt != nil {
			c.ExpiresAt = expiresAt.UTC()
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- Bloqueos ---

func (p *Postgres) CreateBlock(blockerID, blockedID string) error {
	if blockerID == blockedID {
		return errors.New("no puedes bloquearte a ti mismo")
	}
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO user_blocks (blocker_id, blocked_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, blockerID, blockedID)
	return err
}

func (p *Postgres) DeleteBlock(blockerID, blockedID string) error {
	_, err := p.pool.Exec(context.Background(),
		`DELETE FROM user_blocks WHERE blocker_id = $1 AND blocked_id = $2`,
		blockerID, blockedID)
	return err
}

// BlockedPairs consulta las dos direcciones a la vez: el bloqueo corta el
// contacto en ambos sentidos, no solo para quien lo puso.
func (p *Postgres) BlockedPairs(userID string) (map[string]bool, error) {
	rows, err := p.pool.Query(context.Background(), `
		SELECT blocked_id FROM user_blocks WHERE blocker_id = $1
		UNION
		SELECT blocker_id FROM user_blocks WHERE blocked_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
