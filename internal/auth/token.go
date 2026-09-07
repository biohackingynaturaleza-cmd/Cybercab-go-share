package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// DefaultTokenTTL es lo que dura una sesión antes de tener que volver a entrar.
const DefaultTokenTTL = 24 * time.Hour

// Issuer es el nombre con el que se firman los tokens de esta aplicación.
const Issuer = "cybercab-go-share"

// ErrTokenInvalido cubre cualquier token ausente, caducado o manipulado.
var ErrTokenInvalido = errors.New("token no válido o caducado")

// Claims es lo que viaja dentro del token: solo el identificador del usuario.
// Nada de datos personales, que el token va en claro dentro del JWT.
type Claims struct {
	jwt.RegisteredClaims
}

// TokenIssuer emite y verifica los tokens de sesión.
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenIssuer construye el emisor. El secreto debe tener al menos 32 bytes:
// con menos, la firma HS256 es forzable por fuerza bruta.
func NewTokenIssuer(secret []byte, ttl time.Duration) (*TokenIssuer, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("el secreto de firma necesita al menos 32 bytes, tiene %d", len(secret))
	}
	if ttl <= 0 {
		ttl = DefaultTokenTTL
	}
	return &TokenIssuer{secret: secret, ttl: ttl}, nil
}

// GenerateSecret crea un secreto aleatorio, para desarrollo.
func GenerateSecret() ([]byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	out := make([]byte, base64.StdEncoding.EncodedLen(len(b)))
	base64.StdEncoding.Encode(out, b)
	return out, nil
}

// Issue emite un token de sesión para un usuario.
func (t *TokenIssuer) Issue(userID string, now time.Time) (string, time.Time, error) {
	expires := now.Add(t.ttl)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    Issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expires, nil
}

// Verify comprueba la firma y la vigencia, y devuelve el usuario del token.
func (t *TokenIssuer) Verify(token string) (string, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{},
		func(tk *jwt.Token) (any, error) {
			// Sin esta comprobación, un token firmado con "alg: none" —o con
			// HMAC sobre una clave pública— pasaría por válido.
			if _, ok := tk.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("algoritmo de firma inesperado: %v", tk.Header["alg"])
			}
			return t.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(Issuer),
	)
	if err != nil || !parsed.Valid {
		return "", ErrTokenInvalido
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || claims.Subject == "" {
		return "", ErrTokenInvalido
	}
	return claims.Subject, nil
}
