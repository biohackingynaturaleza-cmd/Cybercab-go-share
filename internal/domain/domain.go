// Package domain define las entidades del producto: quién viaja, qué trayecto
// se comparte y qué plazas se reservan.
package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// ErrValidation envuelve cualquier fallo de validación de entrada.
var ErrValidation = errors.New("validación")

// VehicleType distingue los vehículos de la flota de robotaxis.
type VehicleType string

const (
	// VehicleCybercab es el Cybercab de producción: biplaza, sin volante.
	// Compartirlo significa una plaza para quien organiza y una para quien se une.
	VehicleCybercab VehicleType = "cybercab"
	// VehicleModelY es el robotaxi que Tesla opera hoy en Austin.
	VehicleModelY VehicleType = "model_y"
)

// Seats devuelve las plazas utilizables del vehículo.
func (v VehicleType) Seats() int {
	switch v {
	case VehicleCybercab:
		return 2
	case VehicleModelY:
		return 4
	default:
		return 0
	}
}

// Valid indica si el tipo de vehículo está soportado.
func (v VehicleType) Valid() bool { return v.Seats() > 0 }

// Place es un punto con nombre legible: "AUS Terminal Barbara Jordan".
type Place struct {
	Name  string    `json:"name"`
	Point geo.Point `json:"point"`
}

// User es una persona registrada en la app.
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	// PasswordHash nunca sale de la API: el tag `json:"-"` evita que se filtre
	// aunque alguien serialice el usuario entero por descuido.
	PasswordHash string `json:"-"`
	// Idioma es en el que se le escribe: "en" o "es".
	Idioma string `json:"idioma"`
	// RideCount son los viajes ya completados. Es un contador y no una consulta
	// porque solo crece, y crece exactamente una vez: al cerrar un trayecto,
	// que es una transición que el propio estado del trayecto impide repetir.
	//
	// La valoración media no está aquí: se calcula a partir de las
	// valoraciones publicadas, igual que el nivel de confianza se calcula a
	// partir de las comprobaciones vigentes. Un promedio guardado se
	// desincroniza en cuanto una valoración cambia de visible a no visible.
	RideCount int       `json:"ride_count"`
	CreatedAt time.Time `json:"created_at"`
	// TerminosVersion es la redacción de las condiciones que aceptó, y
	// TerminosAt cuándo. Se guarda la versión y no un simple "sí": dentro de
	// dos años, "aceptó las condiciones" no significa nada si no se sabe
	// cuáles.
	TerminosVersion string     `json:"terminos_version"`
	TerminosAt      *time.Time `json:"terminos_at,omitempty"`
	// SuspendidoHasta aparta a alguien de compartir viajes. Nulo es la
	// situación normal. No impide entrar en la cuenta a propósito: quien está
	// suspendido sigue teniendo saldos que liquidar y viajes que consultar, y
	// dejarle fuera de todo solo consigue que no responda de nada.
	SuspendidoHasta *time.Time `json:"suspendido_hasta,omitempty"`
}

// Suspendido indica si esta persona está apartada de compartir viajes.
func (u *User) Suspendido(now time.Time) bool {
	return u.SuspendidoHasta != nil && now.Before(*u.SuspendidoHasta)
}

// TerminosAlDia indica si esta persona aceptó la redacción vigente.
func (u *User) TerminosAlDia() bool { return u.TerminosVersion == VersionTerminos }

// IdiomaPorDefecto es el inglés: el servicio opera en Austin.
const IdiomaPorDefecto = "en"

// IdiomasSoportados son los que la aplicación sabe hablar.
var IdiomasSoportados = []string{"en", "es"}

// NormalizarIdioma devuelve un idioma que la aplicación sabe hablar.
//
// Se aplica en todos los caminos que crean un usuario, no solo en el registro:
// un idioma vacío o desconocido tiene que convertirse en uno válido antes de
// llegar a la base de datos, que lo rechazaría.
func NormalizarIdioma(idioma string) string {
	for _, v := range IdiomasSoportados {
		if idioma == v {
			return idioma
		}
	}
	return IdiomaPorDefecto
}

// TripStatus es el ciclo de vida de un trayecto compartido.
type TripStatus string

const (
	TripOpen      TripStatus = "open"      // admite pasajeros
	TripFull      TripStatus = "full"      // sin plazas libres
	TripCancelled TripStatus = "cancelled" // anulado por quien lo organiza
	TripCompleted TripStatus = "completed" // ya realizado
)

// Trip es un trayecto que alguien ofrece compartir: reserva (o reservará) el
// robotaxi y abre las plazas libres a otros usuarios.
type Trip struct {
	ID          string `json:"id"`
	HostID      string `json:"host_id"`
	Origin      Place  `json:"origin"`
	Destination Place  `json:"destination"`
	// Route es la polilínea completa: origen, waypoints y destino.
	Route geo.Route `json:"route"`
	// TarifaDeclaradaCents es lo que la app de la flota presupuestó para este
	// viaje. Se declara al publicar, no al cerrar: así el pasajero ve su parte
	// exacta antes de comprometerse y no se le puede subir después.
	TarifaDeclaradaCents int64 `json:"tarifa_declarada_cents,omitempty"`
	// TarifaRealCents es lo que la flota cobró de verdad. Solo mueve la parte
	// de quien organiza; las de los pasajeros ya estaban cerradas.
	TarifaRealCents int64 `json:"tarifa_real_cents,omitempty"`
	// DurationMin es el tiempo estimado del trayecto. Con rutas reales lo da
	// el motor de rutas; si no, se estima con una velocidad media.
	DurationMin float64 `json:"duration_min"`
	// FleetRideRef es la referencia del viaje en la flota. En el modelo de
	// traspaso la introduce quien organiza tras pedir el coche en la app de
	// Tesla, y es lo único que ata este trayecto con el viaje real.
	FleetRideRef string `json:"fleet_ride_ref,omitempty"`
	// RouteSource dice de dónde salió la ruta ("osrm", "straight_line"), para
	// no confundir una estimación con una ruta de calle real.
	RouteSource   string      `json:"route_source"`
	DepartureTime time.Time   `json:"departure_time"`
	Vehicle       VehicleType `json:"vehicle"`
	SeatsTotal    int         `json:"seats_total"`
	SeatsTaken    int         `json:"seats_taken"`
	// MaxDetourKm es cuánto acepta desviarse quien organiza para recoger o
	// dejar a alguien fuera de la línea de la ruta.
	MaxDetourKm float64 `json:"max_detour_km"`
	// MinTrustLevel es el nivel de confianza que quien organiza exige a quien
	// se suba. El suelo del vehículo puede elevarlo, nunca rebajarlo: véase
	// trust.Requisito.
	MinTrustLevel trust.Level `json:"min_trust_level"`
	Notes         string      `json:"notes,omitempty"`
	Status        TripStatus  `json:"status"`
	CreatedAt     time.Time   `json:"created_at"`
}

// AforoTotal son las plazas del vehículo, incluida la de quien organiza.
// Es lo que decide si el viaje es un cara a cara o hay testigos a bordo.
func (t *Trip) AforoTotal() int { return t.Vehicle.Seats() }

// NivelExigido es el nivel de confianza que hay que tener para subirse: el
// mayor entre lo que pide quien organiza y el suelo que impone el vehículo.
func (t *Trip) NivelExigido() trust.Level {
	return trust.Requisito(t.MinTrustLevel, t.AforoTotal())
}

// SeatsAvailable son las plazas que quedan para pasajeros.
func (t *Trip) SeatsAvailable() int {
	free := t.SeatsTotal - t.SeatsTaken
	if free < 0 {
		return 0
	}
	return free
}

// DistanceKm es la longitud del trayecto completo.
func (t *Trip) DistanceKm() float64 { return t.Route.LengthKm() }

// Bookable indica si el trayecto admite todavía reservas.
func (t *Trip) Bookable() bool {
	return t.Status == TripOpen && t.SeatsAvailable() > 0
}

// Validate comprueba la coherencia del trayecto antes de guardarlo.
func (t *Trip) Validate() error {
	switch {
	case t.HostID == "":
		return errors.New("falta el usuario que organiza el trayecto")
	case !t.Vehicle.Valid():
		return errors.New("tipo de vehículo no soportado")
	case !t.Route.Valid():
		return errors.New("la ruta necesita al menos dos puntos válidos")
	case t.DepartureTime.IsZero():
		return errors.New("falta la hora de salida")
	case t.SeatsTotal < 1 || t.SeatsTotal > t.Vehicle.Seats()-1:
		// Una plaza siempre es de quien organiza; el resto son compartibles.
		return errors.New("plazas ofertadas fuera del aforo del vehículo")
	case t.MaxDetourKm < 0:
		return errors.New("el desvío máximo no puede ser negativo")
	}
	return nil
}

// BookingStatus es el ciclo de vida de una reserva.
type BookingStatus string

const (
	BookingPending   BookingStatus = "pending"   // esperando a quien organiza
	BookingConfirmed BookingStatus = "confirmed" // plaza asegurada
	BookingRejected  BookingStatus = "rejected"
	BookingCancelled BookingStatus = "cancelled"
)

// Active indica si la reserva ocupa plaza ahora mismo.
func (s BookingStatus) Active() bool {
	return s == BookingPending || s == BookingConfirmed
}

// Booking es la petición de un pasajero para recorrer un tramo del trayecto.
type Booking struct {
	ID          string `json:"id"`
	TripID      string `json:"trip_id"`
	PassengerID string `json:"passenger_id"`
	Pickup      Place  `json:"pickup"`
	Dropoff     Place  `json:"dropoff"`
	// PickupAlongKm y DropoffAlongKm sitúan el tramo sobre la ruta del viaje.
	PickupAlongKm  float64 `json:"pickup_along_km"`
	DropoffAlongKm float64 `json:"dropoff_along_km"`
	Seats          int     `json:"seats"`
	// PriceCents es la estimación mostrada al reservar; el reparto definitivo
	// se recalcula al cerrarse el trayecto.
	PriceCents int64         `json:"price_cents"`
	Status     BookingStatus `json:"status"`
	CreatedAt  time.Time     `json:"created_at"`
}

// SharedKm es la distancia que el pasajero recorre dentro del trayecto.
func (b *Booking) SharedKm() float64 { return b.DropoffAlongKm - b.PickupAlongKm }

// --- Incidencias ---

// TipoIncidencia es lo que la flota cobra aparte del viaje.
type TipoIncidencia string

const (
	// IncidenciaLimpieza es la tasa que cobra la flota por dejar el vehículo
	// sucio. Tesla la carga a quien pidió el coche, aunque lo ensuciara otro.
	IncidenciaLimpieza TipoIncidencia = "limpieza"
	// IncidenciaDanos cubre desperfectos.
	IncidenciaDanos TipoIncidencia = "danos"
	// IncidenciaOtra es cualquier otro cargo posterior al viaje.
	IncidenciaOtra TipoIncidencia = "otra"
)

// Valid indica si el tipo es conocido.
func (t TipoIncidencia) Valid() bool {
	switch t {
	case IncidenciaLimpieza, IncidenciaDanos, IncidenciaOtra:
		return true
	}
	return false
}

// EstadoIncidencia es en qué punto está una incidencia.
type EstadoIncidencia string

const (
	// IncidenciaDeclarada la ha puesto quien organiza y espera respuesta.
	IncidenciaDeclarada EstadoIncidencia = "declarada"
	// IncidenciaAceptada la ha reconocido quien la causó, y se cobra.
	IncidenciaAceptada EstadoIncidencia = "aceptada"
	// IncidenciaDiscutida la niega quien la causó. No se cobra: nadie puede
	// cargarle dinero a otro por su cuenta.
	IncidenciaDiscutida EstadoIncidencia = "discutida"
	// IncidenciaRetirada la ha retirado quien la declaró.
	IncidenciaRetirada EstadoIncidencia = "retirada"
)

// Incidencia es un cargo posterior al viaje que quien organiza quiere
// repercutir a quien lo causó.
//
// No se cobra sola: hasta que la otra parte la acepta, no existe como deuda.
// Cargarle dinero a alguien por su sola palabra sería un agujero evidente, y la
// app no puede arbitrar quién tiene razón.
type Incidencia struct {
	ID           string           `json:"id"`
	TripID       string           `json:"trip_id"`
	DeclaranteID string           `json:"declarante_id"`
	AtribuidaA   string           `json:"atribuida_a"`
	Tipo         TipoIncidencia   `json:"tipo"`
	ImporteCents int64            `json:"importe_cents"`
	Descripcion  string           `json:"descripcion,omitempty"`
	Estado       EstadoIncidencia `json:"estado"`
	CreatedAt    time.Time        `json:"created_at"`
	ResueltaAt   time.Time        `json:"resuelta_at,omitzero"`
}

// Viva indica si la incidencia sigue esperando respuesta.
func (i *Incidencia) Viva() bool {
	return i.Estado == IncidenciaDeclarada || i.Estado == IncidenciaDiscutida
}

// TopeIncidenciaCents acota lo que se puede reclamar por una incidencia.
//
// La tasa más cara que cobra la flota son 150 $ por suciedad severa; dejar el
// campo abierto permitiría reclamar cantidades absurdas y convertir la app en
// una herramienta de extorsión entre desconocidos.
const TopeIncidenciaCents int64 = 20000

// Validate comprueba la incidencia antes de guardarla.
func (i *Incidencia) Validate() error {
	switch {
	case i.TripID == "" || i.DeclaranteID == "" || i.AtribuidaA == "":
		return errors.New("faltan datos de la incidencia")
	case i.DeclaranteID == i.AtribuidaA:
		return errors.New("no puedes atribuirte una incidencia a ti mismo")
	case !i.Tipo.Valid():
		return errors.New("tipo de incidencia desconocido")
	case i.ImporteCents <= 0:
		return errors.New("el importe debe ser positivo")
	case i.ImporteCents > TopeIncidenciaCents:
		return fmt.Errorf("el importe supera el máximo reclamable (%d)", TopeIncidenciaCents)
	}
	return nil
}
