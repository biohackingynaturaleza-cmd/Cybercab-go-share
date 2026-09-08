package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// PlazoValoracion es lo que se espera a que la otra parte valore antes de
// publicar una valoración sin su recíproca.
//
// Dos semanas: tiempo de sobra para que alguien se acuerde de valorar, y poco
// para que quien recibió una mala valoración pueda seguir escondiéndola.
const PlazoValoracion = 14 * 24 * time.Hour

// MaxComentario acota el texto de una valoración. Lo que no cabe en un párrafo
// no es una valoración, es un conflicto, y para eso está la denuncia.
const MaxComentario = 500

// Valoracion es lo que una persona opina de otra tras compartir un viaje.
//
// No se publica en cuanto se escribe: hasta que la otra parte valora también
// —o vence el plazo— no cuenta para nadie. Es la única forma de evitar la
// represalia, que es lo que hunde cualquier sistema de valoraciones a dos
// bandas: si veo primero lo que me han puesto, mi nota deja de ser una opinión
// y pasa a ser una respuesta.
type Valoracion struct {
	ID string `json:"id"`
	// BookingID identifica la pareja concreta: una valoración por reserva y
	// por autor, en cada sentido.
	BookingID string `json:"booking_id"`
	TripID    string `json:"trip_id"`
	AutorID   string `json:"autor_id"`
	SobreID   string `json:"sobre_id"`
	Estrellas int    `json:"estrellas"`
	// Comentario es opcional: obligarlo produce texto de relleno que no dice
	// nada, y la nota sola ya es información.
	Comentario string    `json:"comentario,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// MinEstrellas y MaxEstrellas acotan la nota.
const (
	MinEstrellas = 1
	MaxEstrellas = 5
)

// Visible dice si esta valoración ya cuenta para la reputación de quien la
// recibió. La recíproca es la de la otra parte sobre la misma reserva.
func (v *Valoracion) Visible(hayReciproca bool, now time.Time) bool {
	return hayReciproca || now.After(v.CreatedAt.Add(PlazoValoracion))
}

// Validate comprueba la valoración antes de guardarla.
func (v *Valoracion) Validate() error {
	switch {
	case v.BookingID == "" || v.AutorID == "" || v.SobreID == "":
		return errors.New("faltan datos de la valoración")
	case v.AutorID == v.SobreID:
		return errors.New("no puedes valorarte a ti mismo")
	case v.Estrellas < MinEstrellas || v.Estrellas > MaxEstrellas:
		return fmt.Errorf("la nota debe estar entre %d y %d", MinEstrellas, MaxEstrellas)
	case utf8.RuneCountInString(v.Comentario) > MaxComentario:
		return fmt.Errorf("el comentario no puede pasar de %d caracteres", MaxComentario)
	}
	return nil
}

// NormalizarComentario recorta los espacios sobrantes. Un comentario que solo
// tiene espacios es un comentario vacío, no un comentario.
func NormalizarComentario(c string) string { return strings.TrimSpace(c) }
