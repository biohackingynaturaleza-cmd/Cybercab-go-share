package domain

import "time"

// El código que confirma que un buzón existe y es de quien dice.
//
// Va aparte del proveedor de identidad a propósito. Comprobar un correo es
// mandarle algo y ver si vuelve: lo hacemos nosotros, que ya mandamos correo,
// en vez de pagarle a un proveedor de identidad por un trámite que no necesita
// documentos ni cámara. El proveedor se reserva para lo que solo él puede
// hacer, que es acreditar que la cara y el documento son de la misma persona.
const (
	// LongitudCodigoCorreo son seis dígitos: se leen de un vistazo en la
	// notificación del móvil y se teclean sin equivocarse.
	LongitudCodigoCorreo = 6
	// VigenciaCodigoCorreo es lo que dura. Media hora es de sobra para ir al
	// buzón y volver.
	VigenciaCodigoCorreo = 30 * time.Minute
	// MaxIntentosCodigo acota los intentos por código. Seis dígitos son un
	// millón de combinaciones, que sin tope se prueban en minutos; con tope,
	// cada código solo admite cinco disparos y luego hay que pedir otro.
	MaxIntentosCodigo = 5
)

// CodigoCorreo es el código enviado a un buzón para acreditarlo.
type CodigoCorreo struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	// CheckRef ata el código con la comprobación de confianza que resuelve.
	CheckRef string `json:"check_ref"`
	// CodigoHash es el hash del código, nunca el código. Quien lea la tabla no
	// puede acreditar el buzón de nadie con lo que hay en ella.
	CodigoHash string     `json:"-"`
	Intentos   int        `json:"intentos"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiraAt   time.Time  `json:"expira_at"`
	UsadoAt    *time.Time `json:"usado_at,omitempty"`
}

// Vigente indica si el código todavía sirve: ni usado, ni caducado, ni gastado
// a fuerza de intentos.
func (c *CodigoCorreo) Vigente(now time.Time) bool {
	return c.UsadoAt == nil &&
		c.Intentos < MaxIntentosCodigo &&
		now.Before(c.ExpiraAt)
}

// IntentosRestantes es lo que se le dice a quien se equivoca al teclearlo.
func (c *CodigoCorreo) IntentosRestantes() int {
	if n := MaxIntentosCodigo - c.Intentos; n > 0 {
		return n
	}
	return 0
}
