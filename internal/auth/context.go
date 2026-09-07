package auth

import "context"

// ctxKey es un tipo privado: impide que otro paquete escriba o lea esta clave
// del contexto por accidente.
type ctxKey struct{}

// WithUser guarda en el contexto el usuario ya autenticado.
func WithUser(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ctxKey{}, userID)
}

// UserFrom recupera el usuario autenticado. El segundo valor es falso cuando la
// petición no venía identificada.
func UserFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKey{}).(string)
	return id, ok && id != ""
}
