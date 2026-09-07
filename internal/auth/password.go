// Package auth resuelve la identidad: contraseñas, tokens y el usuario que hay
// detrás de cada petición.
package auth

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// ErrCredencialesInvalidas es la respuesta a cualquier fallo de acceso.
// Es deliberadamente vago: distinguir "no existe" de "contraseña incorrecta"
// permitiría averiguar quién está registrado.
var ErrCredencialesInvalidas = errors.New("email o contraseña incorrectos")

// MinPasswordLen es la longitud mínima exigida a una contraseña.
const MinPasswordLen = 10

// HashPassword deriva el hash que se guarda en la base de datos.
func HashPassword(plain string) (string, error) {
	if n := utf8.RuneCountInString(plain); n < MinPasswordLen {
		return "", fmt.Errorf("la contraseña debe tener al menos %d caracteres", MinPasswordLen)
	}
	// bcrypt trunca en 72 bytes; avisamos en vez de recortar en silencio.
	if len(plain) > 72 {
		return "", errors.New("la contraseña no puede superar los 72 bytes")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// CheckPassword compara una contraseña con su hash en tiempo constante.
func CheckPassword(hash, plain string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)); err != nil {
		return ErrCredencialesInvalidas
	}
	return nil
}
