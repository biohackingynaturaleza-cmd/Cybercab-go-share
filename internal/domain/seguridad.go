package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
)

// --- Contactos de confianza ---

// MaxContactos son los avisos que se mandan al pulsar el botón.
//
// Tres y no más: una lista larga diluye la responsabilidad —cada uno supone que
// ya habrá reaccionado otro— y multiplica a quién se le enseña dónde estás.
const MaxContactos = 3

// ContactoDeConfianza es alguien a quien avisar si algo va mal.
type ContactoDeConfianza struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	Nombre string `json:"nombre"`
	Email  string `json:"email"`
	// AvisarAlSalir manda el enlace de seguimiento al empezar cada viaje, sin
	// tener que acordarse. La mayoría de la gente no se acuerda.
	AvisarAlSalir bool      `json:"avisar_al_salir"`
	CreatedAt     time.Time `json:"created_at"`
}

// Validate comprueba el contacto antes de guardarlo.
func (c *ContactoDeConfianza) Validate() error {
	switch {
	case strings.TrimSpace(c.Nombre) == "":
		return errors.New("el contacto necesita un nombre")
	case utf8.RuneCountInString(c.Nombre) > 80:
		return errors.New("el nombre es demasiado largo")
	case !strings.Contains(c.Email, "@"):
		return errors.New("el correo del contacto no parece válido")
	}
	return nil
}

// --- Seguimiento del viaje ---

// GraciaSeguimiento es lo que sigue abierto un enlace después de la hora de
// salida. Un viaje al aeropuerto que se retrasa no puede dejar a quien te
// espera mirando una página caducada.
const GraciaSeguimiento = 12 * time.Hour

// Seguimiento es el enlace público que enseña un viaje en curso.
//
// Lo crea quien va dentro, para quien quiera. Es la versión digital de decirle
// a alguien "voy en este coche, con esta persona, y llego sobre esta hora": no
// evita nada por sí solo, pero convierte un viaje anónimo en uno del que hay
// testigo.
type Seguimiento struct {
	ID     string `json:"id"`
	TripID string `json:"trip_id"`
	// UserID es quien comparte. El enlace enseña el viaje desde su punto de
	// vista, y solo él puede revocarlo.
	UserID string `json:"user_id"`
	// TokenHash es el hash del testigo del enlace, nunca el testigo. Quien lea
	// esta tabla no puede seguir el viaje de nadie con lo que hay en ella.
	TokenHash  string     `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiraAt   time.Time  `json:"expira_at"`
	RevocadoAt *time.Time `json:"revocado_at,omitempty"`
	// UltimaPosicion es dónde estaba quien comparte la última vez que su
	// navegador lo dijo. Nula mientras no lo diga: no se rastrea a nadie de
	// fondo, solo con la app abierta y con permiso.
	UltimaPosicion   *geo.Point `json:"ultima_posicion,omitempty"`
	UltimaPosicionAt *time.Time `json:"ultima_posicion_at,omitempty"`
}

// Vigente indica si el enlace todavía enseña algo.
func (s *Seguimiento) Vigente(now time.Time) bool {
	return s.RevocadoAt == nil && now.Before(s.ExpiraAt)
}

// --- Alertas ---

// EstadoAlerta es en qué punto está una alerta.
type EstadoAlerta string

const (
	// AlertaAbierta acaba de dispararse y nadie la ha atendido.
	AlertaAbierta EstadoAlerta = "abierta"
	// AlertaRetirada la ha retirado quien la disparó: falsa alarma.
	AlertaRetirada EstadoAlerta = "retirada"
	// AlertaAtendida la ha cerrado operaciones tras ocuparse de ella.
	AlertaAtendida EstadoAlerta = "atendida"
)

// Alerta es el botón de emergencia pulsado durante un viaje.
//
// Lo que hace está acotado a propósito, y la interfaz lo dice con todas las
// letras: avisa a los contactos de confianza con el enlace del viaje y la
// última posición, y entra en la cola de operaciones por delante de todo. **No
// llama a los servicios de emergencia.** Una app no puede hacer esa llamada, y
// dar a entender que sí es la clase de mentira por la que alguien se queda
// esperando ayuda que no viene.
type Alerta struct {
	ID     string `json:"id"`
	TripID string `json:"trip_id"`
	UserID string `json:"user_id"`
	// Posicion es dónde estaba quien la pulsó, si el navegador lo dijo. Puede
	// faltar: sin permiso de ubicación el botón sigue funcionando, porque lo
	// que no puede pasar es que falle justo cuando hace falta.
	Posicion   *geo.Point   `json:"posicion,omitempty"`
	Nota       string       `json:"nota,omitempty"`
	Estado     EstadoAlerta `json:"estado"`
	CreatedAt  time.Time    `json:"created_at"`
	ResueltaAt time.Time    `json:"resuelta_at,omitzero"`
	// Resolucion es lo que hizo quien la atendió.
	Resolucion string `json:"resolucion,omitempty"`
}

// MaxNotaAlerta acota el texto que se puede escribir al pulsar.
const MaxNotaAlerta = 500

// Validate comprueba la alerta antes de guardarla.
func (a *Alerta) Validate() error {
	switch {
	case a.TripID == "" || a.UserID == "":
		return errors.New("faltan datos de la alerta")
	case utf8.RuneCountInString(a.Nota) > MaxNotaAlerta:
		return fmt.Errorf("la nota no puede pasar de %d caracteres", MaxNotaAlerta)
	}
	return nil
}

// Viva indica si la alerta sigue sin resolverse.
func (a *Alerta) Viva() bool { return a.Estado == AlertaAbierta }

// TelefonoEmergencias es el número que la interfaz pone a un toque.
//
// El servicio opera en Austin. Está aquí, en el dominio, para que cambiarlo al
// abrir en otro país sea un sitio y no una búsqueda por toda la interfaz.
const TelefonoEmergencias = "911"

// PrimerNombre es lo que se le enseña a quien abre un enlace de seguimiento
// sobre los demás ocupantes del vehículo.
//
// El nombre completo de un tercero no es de quien comparte: comparte su viaje,
// no la identidad de quien va a su lado.
func PrimerNombre(nombre string) string {
	nombre = strings.TrimSpace(nombre)
	if i := strings.IndexAny(nombre, " \t"); i > 0 {
		return nombre[:i]
	}
	return nombre
}
