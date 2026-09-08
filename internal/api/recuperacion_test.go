package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
)

// servidorConCorreo devuelve el entorno y su grabador de correos, que es de
// donde sale el enlace que recibiría una persona de verdad.
func servidorConCorreo(t *testing.T) (*entorno, *notify.Grabador) {
	t.Helper()
	e := nuevoEntorno(t)
	return e, e.avisos
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
