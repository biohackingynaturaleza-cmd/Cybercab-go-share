package api_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/api"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/auth"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// servidorConCorreo monta el servidor con un grabador de avisos, que es de
// donde sale el enlace que recibiría una persona de verdad.
func servidorConCorreo(t *testing.T) (*entorno, *notify.Grabador) {
	t.Helper()
	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	tokens, err := auth.NewTokenIssuer(secret, time.Hour)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	identidad := trust.NewManual("http://test")
	avisos := &notify.Grabador{}
	svc := service.New(store.NewMemory(), service.Config{
		Tokens: tokens, Identidad: identidad, Avisos: avisos,
		PublicURL: "https://app.ejemplo.test",
	})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(api.NewServer(svc, tokens, log))
	t.Cleanup(srv.Close)
	return &entorno{Server: srv, identidad: identidad}, avisos
}

func TestRecuperarLaContrasenaDePuntaAPunta(t *testing.T) {
	e, avisos := servidorConCorreo(t)
	registrarSinVerificar(t, e, "Ana", "ana@example.com")

	if code := do(t, e, http.MethodPost, "/api/v1/auth/recuperar", "",
		map[string]string{"email": "ana@example.com"}, nil); code != http.StatusAccepted {
		t.Fatalf("pedir el enlace: código = %d, esperaba 202", code)
	}

	aviso, ok := avisos.Ultimo(notify.SucesoRecuperacionPedida)
	if !ok {
		t.Fatal("no llegó el correo con el enlace")
	}
	_, testigo, _ := strings.Cut(aviso.Datos["enlace"], "#recuperar=")
	if testigo == "" {
		t.Fatalf("el enlace no lleva testigo: %q", aviso.Datos["enlace"])
	}

	if code := do(t, e, http.MethodPost, "/api/v1/auth/recuperar/confirmar", "",
		map[string]string{"testigo": testigo, "password": "otra-contraseña-larga"}, nil); code != http.StatusOK {
		t.Fatalf("confirmar: código = %d, esperaba 200", code)
	}

	var s sesion
	if code := do(t, e, http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"email": "ana@example.com", "password": "otra-contraseña-larga"}, &s); code != http.StatusOK {
		t.Fatalf("entrar con la contraseña nueva: código = %d", code)
	}
	if s.Token == "" {
		t.Fatal("el login no devolvió sesión")
	}
}

func TestPedirLaRecuperacionNoRevelaSiElCorreoExiste(t *testing.T) {
	// Las dos respuestas tienen que ser indistinguibles: si no, este formulario
	// es un buscador de quién tiene cuenta aquí.
	e, avisos := servidorConCorreo(t)
	registrarSinVerificar(t, e, "Ana", "ana@example.com")

	conocido := do(t, e, http.MethodPost, "/api/v1/auth/recuperar", "",
		map[string]string{"email": "ana@example.com"}, nil)
	desconocido := do(t, e, http.MethodPost, "/api/v1/auth/recuperar", "",
		map[string]string{"email": "nadie@example.com"}, nil)

	if conocido != desconocido {
		t.Fatalf("códigos distintos: conocido %d, desconocido %d", conocido, desconocido)
	}
	if n := len(avisos.Para("nadie@example.com")); n != 0 {
		t.Fatalf("se mandaron %d correos a una dirección sin cuenta", n)
	}
}

func TestUnEnlaceGastadoDevuelve410(t *testing.T) {
	e, avisos := servidorConCorreo(t)
	registrarSinVerificar(t, e, "Ana", "ana@example.com")
	do(t, e, http.MethodPost, "/api/v1/auth/recuperar", "",
		map[string]string{"email": "ana@example.com"}, nil)

	aviso, _ := avisos.Ultimo(notify.SucesoRecuperacionPedida)
	_, testigo, _ := strings.Cut(aviso.Datos["enlace"], "#recuperar=")

	cuerpo := map[string]string{"testigo": testigo, "password": "otra-contraseña-larga"}
	if code := do(t, e, http.MethodPost, "/api/v1/auth/recuperar/confirmar", "", cuerpo, nil); code != http.StatusOK {
		t.Fatalf("primer uso: código = %d", code)
	}
	if code := do(t, e, http.MethodPost, "/api/v1/auth/recuperar/confirmar", "", cuerpo, nil); code != http.StatusGone {
		t.Fatalf("segundo uso: código = %d, esperaba 410", code)
	}
}
