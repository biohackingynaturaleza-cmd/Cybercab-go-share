package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func newIssuer(t *testing.T, ttl time.Duration) *TokenIssuer {
	t.Helper()
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	issuer, err := NewTokenIssuer(secret, ttl)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	return issuer
}

// --- Contraseñas ---

func TestHashYComprobacionDeContrasena(t *testing.T) {
	hash, err := HashPassword("una-contraseña-larga")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if strings.Contains(hash, "una-contraseña-larga") {
		t.Fatal("el hash contiene la contraseña en claro")
	}
	if err := CheckPassword(hash, "una-contraseña-larga"); err != nil {
		t.Errorf("la contraseña correcta no valida: %v", err)
	}
	if err := CheckPassword(hash, "otra-contraseña-larga"); !errors.Is(err, ErrCredencialesInvalidas) {
		t.Errorf("error = %v, esperaba ErrCredencialesInvalidas", err)
	}
}

func TestHashPasswordRechazaContrasenasCortas(t *testing.T) {
	if _, err := HashPassword("corta"); err == nil {
		t.Fatal("esperaba un error con una contraseña por debajo del mínimo")
	}
}

func TestHashPasswordRechazaMasDe72Bytes(t *testing.T) {
	// bcrypt ignora lo que pase de 72 bytes: aceptarlo en silencio haría que
	// dos contraseñas distintas abrieran la misma cuenta.
	if _, err := HashPassword(strings.Repeat("a", 73)); err == nil {
		t.Fatal("esperaba un error con una contraseña de más de 72 bytes")
	}
}

func TestElMismoHashNoSeRepite(t *testing.T) {
	a, _ := HashPassword("una-contraseña-larga")
	b, _ := HashPassword("una-contraseña-larga")
	if a == b {
		t.Fatal("dos hashes de la misma contraseña son idénticos: falta la sal")
	}
}

// --- Tokens ---

func TestTokenIdaYVuelta(t *testing.T) {
	issuer := newIssuer(t, time.Hour)
	now := time.Now()

	token, expires, err := issuer.Issue("usr_123", now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !expires.After(now) {
		t.Error("la caducidad no está en el futuro")
	}

	got, err := issuer.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != "usr_123" {
		t.Fatalf("usuario = %q, esperaba usr_123", got)
	}
}

func TestNewTokenIssuerRechazaSecretosCortos(t *testing.T) {
	if _, err := NewTokenIssuer([]byte("demasiado-corto"), time.Hour); err == nil {
		t.Fatal("esperaba un error: un secreto corto hace forzable la firma")
	}
}

func TestVerifyRechazaTokenCaducado(t *testing.T) {
	issuer := newIssuer(t, time.Hour)
	// Emitido hace dos horas con una vigencia de una: ya no vale.
	token, _, err := issuer.Issue("usr_123", time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := issuer.Verify(token); !errors.Is(err, ErrTokenInvalido) {
		t.Fatalf("error = %v, esperaba ErrTokenInvalido", err)
	}
}

func TestVerifyRechazaTokenDeOtroSecreto(t *testing.T) {
	emisor := newIssuer(t, time.Hour)
	impostor := newIssuer(t, time.Hour)

	token, _, _ := impostor.Issue("usr_123", time.Now())
	if _, err := emisor.Verify(token); !errors.Is(err, ErrTokenInvalido) {
		t.Fatalf("error = %v: un token firmado con otro secreto no puede valer", err)
	}
}

func TestVerifyRechazaAlgoritmoNone(t *testing.T) {
	// El ataque clásico contra JWT: firmar con "alg: none" y colar el usuario
	// que a uno le apetezca.
	issuer := newIssuer(t, time.Hour)
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{
		Subject:   "usr_intruso",
		Issuer:    Issuer,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("construyendo el token de prueba: %v", err)
	}

	if _, err := issuer.Verify(token); !errors.Is(err, ErrTokenInvalido) {
		t.Fatalf("error = %v: un token sin firma no puede valer", err)
	}
}

func TestVerifyRechazaBasura(t *testing.T) {
	issuer := newIssuer(t, time.Hour)
	for _, token := range []string{"", "no-es-un-token", "a.b.c"} {
		if _, err := issuer.Verify(token); !errors.Is(err, ErrTokenInvalido) {
			t.Errorf("token %q: error = %v, esperaba ErrTokenInvalido", token, err)
		}
	}
}

// --- Contexto y middleware ---

func TestUserFromContexto(t *testing.T) {
	if _, ok := UserFrom(context.Background()); ok {
		t.Error("un contexto vacío no debería tener usuario")
	}
	ctx := WithUser(context.Background(), "usr_1")
	if id, ok := UserFrom(ctx); !ok || id != "usr_1" {
		t.Errorf("UserFrom = %q, %v", id, ok)
	}
	if _, ok := UserFrom(WithUser(context.Background(), "")); ok {
		t.Error("un usuario vacío no cuenta como autenticado")
	}
}

// eco responde con el usuario que el middleware haya dejado en el contexto.
func eco(w http.ResponseWriter, r *http.Request) {
	id, _ := UserFrom(r.Context())
	_, _ = w.Write([]byte(id))
}

func rechazo(w http.ResponseWriter, _ *http.Request, _ error) {
	w.WriteHeader(http.StatusUnauthorized)
}

func TestRequireDejaPasarConTokenValido(t *testing.T) {
	issuer := newIssuer(t, time.Hour)
	token, _, _ := issuer.Issue("usr_42", time.Now())

	h := Require(issuer, rechazo)(http.HandlerFunc(eco))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("código = %d, esperaba 200", rec.Code)
	}
	if rec.Body.String() != "usr_42" {
		t.Fatalf("usuario = %q, esperaba usr_42", rec.Body.String())
	}
}

func TestRequireRechazaCabecerasMalFormadas(t *testing.T) {
	issuer := newIssuer(t, time.Hour)
	token, _, _ := issuer.Issue("usr_42", time.Now())

	cases := map[string]string{
		"sin cabecera":       "",
		"sin esquema":        token,
		"esquema incorrecto": "Basic " + token,
		"token vacío":        "Bearer ",
		"token inventado":    "Bearer no-es-un-token",
	}
	h := Require(issuer, rechazo)(http.HandlerFunc(eco))

	for name, header := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: código = %d, esperaba 401", name, rec.Code)
		}
	}
}

func TestRequireAceptaElEsquemaEnMinusculas(t *testing.T) {
	// RFC 7235: el esquema no distingue mayúsculas.
	issuer := newIssuer(t, time.Hour)
	token, _, _ := issuer.Issue("usr_42", time.Now())

	h := Require(issuer, rechazo)(http.HandlerFunc(eco))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("código = %d, esperaba 200", rec.Code)
	}
}

func TestOptionalDejaPasarSinToken(t *testing.T) {
	issuer := newIssuer(t, time.Hour)
	h := Optional(issuer)(http.HandlerFunc(eco))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "" {
		t.Fatalf("código = %d, cuerpo = %q; esperaba 200 y sin usuario", rec.Code, rec.Body.String())
	}
}
