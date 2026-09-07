// Package trust decide en quién se puede confiar para compartir un vehículo.
//
// El planteamiento parte de una diferencia con el coche compartido de toda la
// vida: aquí no hay conductor. En un viaje compartido convencional, quien
// conduce hace de testigo y de autoridad dentro del coche. En un robotaxi no
// hay nadie al mando, y en un Cybercab biplaza son dos desconocidos solos. Ese
// mecanismo de seguridad que otros servicios tienen gratis, aquí hay que
// construirlo: por eso la verificación de identidad es más estricta, no menos.
package trust

import "time"

// CheckKind es cada comprobación que se le puede pedir a una persona.
type CheckKind string

const (
	// CheckEmail confirma que el buzón existe y es suyo.
	CheckEmail CheckKind = "email"
	// CheckPhone confirma un teléfono real. Sube mucho el coste de crear
	// cuentas desechables.
	CheckPhone CheckKind = "phone"
	// CheckGovernmentID comprueba un documento oficial contra el proveedor.
	CheckGovernmentID CheckKind = "government_id"
	// CheckSelfie compara la cara con la del documento y verifica que hay una
	// persona viva delante de la cámara.
	CheckSelfie CheckKind = "selfie_liveness"
	// CheckPayment confirma un medio de pago válido: deja rastro y responde
	// económicamente de lo que pase.
	CheckPayment CheckKind = "payment_method"
)

// Valid indica si la comprobación es de un tipo conocido.
func (k CheckKind) Valid() bool {
	switch k {
	case CheckEmail, CheckPhone, CheckGovernmentID, CheckSelfie, CheckPayment:
		return true
	}
	return false
}

// Label es el nombre que ve la persona usuaria.
func (k CheckKind) Label() string {
	switch k {
	case CheckEmail:
		return "correo electrónico"
	case CheckPhone:
		return "teléfono"
	case CheckGovernmentID:
		return "documento de identidad"
	case CheckSelfie:
		return "selfie con prueba de vida"
	case CheckPayment:
		return "medio de pago"
	}
	return string(k)
}

// CheckStatus es el estado de una comprobación.
type CheckStatus string

const (
	StatusPending  CheckStatus = "pending"  // iniciada, esperando al proveedor
	StatusVerified CheckStatus = "verified" // superada
	StatusRejected CheckStatus = "rejected" // el proveedor la rechazó
	StatusExpired  CheckStatus = "expired"  // caducada (un DNI vence)
)

// Check es una comprobación concreta sobre una persona.
//
// Nunca guarda el documento ni la fotografía: solo la referencia del proveedor
// y el resultado. Los datos sensibles se quedan en quien está preparado para
// custodiarlos, y una filtración de esta base de datos no expone documentos de
// identidad de nadie.
type Check struct {
	ID     string      `json:"id"`
	UserID string      `json:"user_id"`
	Kind   CheckKind   `json:"kind"`
	Status CheckStatus `json:"status"`
	// ProviderRef identifica la verificación en el proveedor externo, para
	// poder auditarla sin replicar aquí sus datos.
	ProviderRef string    `json:"provider_ref,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	VerifiedAt  time.Time `json:"verified_at,omitzero"`
	// ExpiresAt es cuándo deja de valer. Un documento caduca; un teléfono no.
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	// RejectionReason explica un rechazo sin exponer datos personales.
	RejectionReason string `json:"rejection_reason,omitempty"`
}

// Active indica si la comprobación cuenta a día de hoy.
func (c Check) Active(now time.Time) bool {
	if c.Status != StatusVerified {
		return false
	}
	// Una comprobación caducada deja de valer aunque nadie haya actualizado su
	// estado: se decide con el reloj, no con lo que diga la columna.
	return c.ExpiresAt.IsZero() || c.ExpiresAt.After(now)
}
