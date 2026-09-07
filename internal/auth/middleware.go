package auth

import (
	"net/http"
	"strings"
)

// Verifier es lo que el middleware necesita del emisor de tokens.
type Verifier interface {
	Verify(token string) (string, error)
}

// Unauthorized es el gancho para responder a una petición sin identidad válida.
// Lo inyecta la capa HTTP para dar el formato de error de la API.
type Unauthorized func(w http.ResponseWriter, r *http.Request, err error)

// Require exige un token válido: sin él, la petición no llega al manejador.
func Require(v Verifier, onError Unauthorized) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := identify(v, r)
			if err != nil {
				onError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), userID)))
		})
	}
}

// Optional identifica al usuario si trae token, pero deja pasar si no.
// Sirve para rutas públicas que enseñan más información a quien ha entrado.
func Optional(v Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if userID, err := identify(v, r); err == nil {
				r = r.WithContext(WithUser(r.Context(), userID))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// identify extrae y valida el token de la cabecera Authorization.
func identify(v Verifier, r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", ErrTokenInvalido
	}
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", ErrTokenInvalido
	}
	return v.Verify(strings.TrimSpace(token))
}
