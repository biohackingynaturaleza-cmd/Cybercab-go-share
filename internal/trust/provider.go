package trust

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Session es la verificación abierta en el proveedor externo: la persona
// completa el trámite en RedirectURL y el proveedor nos avisa del resultado.
type Session struct {
	// Ref identifica la verificación en el proveedor.
	Ref string `json:"ref"`
	// RedirectURL es adonde hay que enviar a la persona.
	RedirectURL string    `json:"redirect_url"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Outcome es el veredicto del proveedor.
type Outcome struct {
	Status CheckStatus
	// DocumentExpiresAt es la caducidad del documento acreditado, si la tiene.
	DocumentExpiresAt time.Time
	// Reason explica un rechazo sin arrastrar datos personales.
	Reason string
	// Cubre son las comprobaciones que este veredicto acredita. Suele ser solo
	// la que se pidió, pero un mismo trámite puede resolver varias: los
	// proveedores serios comprueban el documento y la cara en un único paso, y
	// obligar a la persona a repetirlo dos veces sería absurdo.
	Cubre []CheckKind
}

// Acredita indica si el veredicto cubre esa comprobación.
func (o Outcome) Acredita(k CheckKind) bool {
	if len(o.Cubre) == 0 {
		return true // sin detalle, vale para la que se pidió
	}
	for _, c := range o.Cubre {
		if c == k {
			return true
		}
	}
	return false
}

// Provider verifica identidades contra un servicio externo (Stripe Identity,
// Onfido, Persona y similares).
//
// Es una interfaz por dos razones. Una, no atarse a un proveedor: cambiarlo es
// una decisión de negocio, no una reescritura. Y dos, para que ni el documento
// ni la fotografía pasen jamás por esta aplicación: aquí solo entra el
// veredicto.
type Provider interface {
	// Start abre una verificación y devuelve adónde mandar a la persona.
	Start(ctx context.Context, userID string, kind CheckKind) (*Session, error)
	// Result consulta el veredicto. Los proveedores serios además envían un
	// webhook; esto sirve para reconciliar y para no depender de que llegue.
	Result(ctx context.Context, ref string) (*Outcome, error)
}

// --- Proveedor de desarrollo ---

// Manual es un Provider de mentira para desarrollo y pruebas: no verifica
// nada, deja las comprobaciones pendientes y permite resolverlas a mano.
//
// Nunca debe usarse en producción: daría por buena la identidad de cualquiera,
// que es justo lo contrario de lo que este sistema existe para hacer.
type Manual struct {
	mu       sync.Mutex
	outcomes map[string]*Outcome
	// BaseURL es el prefijo de las URL de redirección simuladas.
	BaseURL string
}

// NewManual construye el proveedor de desarrollo.
func NewManual(baseURL string) *Manual {
	return &Manual{outcomes: map[string]*Outcome{}, BaseURL: baseURL}
}

var _ Provider = (*Manual)(nil)

// Start abre una verificación simulada, que queda pendiente hasta que alguien
// la resuelva con Resolve.
func (m *Manual) Start(_ context.Context, userID string, kind CheckKind) (*Session, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("%w: %s", ErrTipoDesconocido, kind)
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	ref := "manual_" + hex.EncodeToString(b[:])

	m.mu.Lock()
	m.outcomes[ref] = &Outcome{Status: StatusPending}
	m.mu.Unlock()

	return &Session{
		Ref:         ref,
		RedirectURL: fmt.Sprintf("%s/dev/verificar/%s?usuario=%s&tipo=%s", m.BaseURL, ref, userID, kind),
		ExpiresAt:   time.Now().UTC().Add(24 * time.Hour),
	}, nil
}

// Result devuelve el veredicto registrado para esa verificación.
func (m *Manual) Result(_ context.Context, ref string) (*Outcome, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.outcomes[ref]
	if !ok {
		return nil, fmt.Errorf("verificación desconocida: %s", ref)
	}
	cp := *o
	return &cp, nil
}

// Resolve fija a mano el veredicto de una verificación. Solo para desarrollo.
func (m *Manual) Resolve(ref string, outcome Outcome) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.outcomes[ref]; !ok {
		return fmt.Errorf("verificación desconocida: %s", ref)
	}
	m.outcomes[ref] = &outcome
	return nil
}
