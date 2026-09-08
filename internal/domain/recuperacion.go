package domain

import "time"

// VigenciaRecuperacion es lo que dura un enlace para cambiar la contraseña.
//
// Una hora es tiempo de sobra para ir al correo y volver, y poco para que el
// enlace siga sirviendo si el buzón acaba en malas manos más tarde.
const VigenciaRecuperacion = time.Hour

// Recuperacion es una petición de cambio de contraseña.
//
// Lo que se guarda es el hash del testigo, nunca el testigo. Quien consiga leer
// esta tabla no puede entrar en ninguna cuenta con lo que hay en ella, igual
// que no puede con la columna de contraseñas.
type Recuperacion struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	TokenHash string     `json:"-"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiraAt  time.Time  `json:"expira_at"`
	UsadaAt   *time.Time `json:"usada_at,omitempty"`
}

// Vigente indica si el enlace todavía sirve: ni usado ni caducado.
func (r *Recuperacion) Vigente(now time.Time) bool {
	return r.UsadaAt == nil && now.Before(r.ExpiraAt)
}
