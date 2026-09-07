package trust

import "time"

// Level es el nivel de confianza de una persona. No se guarda: se deriva de las
// comprobaciones vigentes y del historial, de modo que caduca solo cuando
// caduca lo que lo sostiene.
type Level int

const (
	// LevelNuevo es quien acaba de llegar: no ha probado nada todavía.
	LevelNuevo Level = iota
	// LevelBasico ha confirmado email y teléfono. Basta para publicar un
	// trayecto, no para compartirlo con desconocidos.
	LevelBasico
	// LevelVerificado ha acreditado su identidad con documento oficial y
	// selfie. Es el nivel que exige compartir coche.
	LevelVerificado
	// LevelVeterano es alguien verificado con historial y buenas valoraciones.
	LevelVeterano
)

// Label es el nombre que ve la persona usuaria.
func (l Level) Label() string {
	switch l {
	case LevelBasico:
		return "básico"
	case LevelVerificado:
		return "verificado"
	case LevelVeterano:
		return "veterano"
	default:
		return "nuevo"
	}
}

func (l Level) String() string { return l.Label() }

// MarshalJSON escribe el nivel como texto: un número desnudo en la API sería
// ilegible y se rompería al insertar un nivel intermedio.
func (l Level) MarshalJSON() ([]byte, error) {
	return []byte(`"` + l.Label() + `"`), nil
}

// UnmarshalJSON acepta el nivel por su nombre.
func (l *Level) UnmarshalJSON(data []byte) error {
	s := string(data)
	if len(s) >= 2 && s[0] == '"' {
		s = s[1 : len(s)-1]
	}
	parsed, ok := ParseLevel(s)
	if !ok {
		return ErrNivelDesconocido
	}
	*l = parsed
	return nil
}

// ParseLevel convierte el nombre de un nivel en su valor.
func ParseLevel(s string) (Level, bool) {
	switch s {
	case "nuevo", "":
		return LevelNuevo, true
	case "básico", "basico":
		return LevelBasico, true
	case "verificado":
		return LevelVerificado, true
	case "veterano":
		return LevelVeterano, true
	}
	return LevelNuevo, false
}

// Stats es el historial que suma al nivel de confianza.
type Stats struct {
	CompletedTrips int     `json:"completed_trips"`
	Rating         float64 `json:"rating"`
	RatingCount    int     `json:"rating_count"`
}

// Umbrales para llegar a veterano. Salen de una idea sencilla: la reputación
// solo dice algo cuando hay suficientes viajes y suficientes valoraciones
// distintas detrás.
const (
	VeteranoMinTrips   = 5
	VeteranoMinRatings = 3
	VeteranoMinRating  = 4.5
)

// LevelOf calcula el nivel a partir de las comprobaciones vigentes y el
// historial.
//
// Los niveles son escalones, no una suma de puntos: no se llega a verificado
// acumulando comprobaciones menores. Y hacen falta documento *y* selfie a la
// vez, porque un documento por sí solo demuestra que el documento existe, no
// que quien lo enseña sea su titular.
func LevelOf(checks []Check, stats Stats, now time.Time) Level {
	vigentes := map[CheckKind]bool{}
	for _, c := range checks {
		if c.Active(now) {
			vigentes[c.Kind] = true
		}
	}

	if !vigentes[CheckEmail] {
		return LevelNuevo
	}
	if !vigentes[CheckPhone] {
		return LevelNuevo
	}
	if !vigentes[CheckGovernmentID] || !vigentes[CheckSelfie] {
		return LevelBasico
	}
	if esVeterano(stats) {
		return LevelVeterano
	}
	return LevelVerificado
}

func esVeterano(s Stats) bool {
	return s.CompletedTrips >= VeteranoMinTrips &&
		s.RatingCount >= VeteranoMinRatings &&
		s.Rating >= VeteranoMinRating
}

// Missing enumera lo que le falta a alguien para alcanzar un nivel, para poder
// decírselo en vez de limitarse a negarle el acceso.
func Missing(checks []Check, target Level, now time.Time) []CheckKind {
	vigentes := map[CheckKind]bool{}
	for _, c := range checks {
		if c.Active(now) {
			vigentes[c.Kind] = true
		}
	}

	var necesarias []CheckKind
	if target >= LevelBasico {
		necesarias = append(necesarias, CheckEmail, CheckPhone)
	}
	if target >= LevelVerificado {
		necesarias = append(necesarias, CheckGovernmentID, CheckSelfie)
	}

	var faltan []CheckKind
	for _, k := range necesarias {
		if !vigentes[k] {
			faltan = append(faltan, k)
		}
	}
	return faltan
}
