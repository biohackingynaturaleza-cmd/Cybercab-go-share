package trust

import "errors"

// ErrNivelDesconocido se devuelve al leer un nivel que no existe.
var ErrNivelDesconocido = errors.New("nivel de confianza desconocido")

// ErrTipoDesconocido se devuelve al pedir una comprobación de un tipo que no
// se soporta.
var ErrTipoDesconocido = errors.New("tipo de comprobación desconocido")

// ErrConfianzaInsuficiente se devuelve cuando alguien no alcanza el nivel que
// exige un trayecto.
var ErrConfianzaInsuficiente = errors.New("no alcanzas el nivel de confianza que pide este trayecto")
