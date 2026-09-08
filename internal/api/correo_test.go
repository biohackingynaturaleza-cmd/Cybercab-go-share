package api_test

import (
	"net/http"
	"testing"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
)

func TestConfirmarElBuzonDePuntaAPunta(t *testing.T) {
	srv := newTestServer(t)
	s := registrarSinVerificar(t, srv, "Ana", "ana@example.com")

	// El alta ya mandó el código: no hay que pedir nada.
	codigo := ultimoCodigo(t, srv, "ana@example.com")
	if len(codigo) != domain.LongitudCodigoCorreo {
		t.Fatalf("código = %q", codigo)
	}

	var confirmada struct {
		Verificacion struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"verificacion"`
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/me/correo/confirmar", s.Token,
		map[string]string{"codigo": codigo}, &confirmada); code != http.StatusOK {
		t.Fatalf("confirmar: código = %d", code)
	}
	if confirmada.Verificacion.Kind != "email" || confirmada.Verificacion.Status != "verified" {
		t.Fatalf("verificación = %+v", confirmada.Verificacion)
	}

	var perfil struct {
		Verificaciones []string `json:"verificaciones"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/users/"+s.User.ID+"/confianza", "", nil, &perfil); code != http.StatusOK {
		t.Fatalf("perfil: código = %d", code)
	}
	if len(perfil.Verificaciones) != 1 {
		t.Fatalf("verificaciones = %v, esperaba solo el buzón", perfil.Verificaciones)
	}
}

func TestUnCodigoEquivocadoDevuelve422ConLosIntentosQueQuedan(t *testing.T) {
	srv := newTestServer(t)
	s := registrarSinVerificar(t, srv, "Ana", "ana@example.com")

	var problema struct {
		Codigo            string `json:"codigo"`
		IntentosRestantes int    `json:"intentos_restantes"`
	}
	code := do(t, srv, http.MethodPost, "/api/v1/me/correo/confirmar", s.Token,
		map[string]string{"codigo": "000000"}, &problema)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("código = %d, esperaba 422", code)
	}
	if problema.Codigo != "codigo_invalido" {
		t.Fatalf("codigo = %q", problema.Codigo)
	}
	// Decir cuántos quedan es lo que evita que alguien siga probando a ciegas
	// hasta quedarse fuera sin saber por qué.
	if problema.IntentosRestantes != domain.MaxIntentosCodigo-1 {
		t.Fatalf("intentos restantes = %d, esperaba %d", problema.IntentosRestantes, domain.MaxIntentosCodigo-1)
	}
}

func TestElCodigoSeAgotaYDevuelve410(t *testing.T) {
	srv := newTestServer(t)
	s := registrarSinVerificar(t, srv, "Ana", "ana@example.com")
	bueno := ultimoCodigo(t, srv, "ana@example.com")

	var ultimo int
	for i := range domain.MaxIntentosCodigo + 1 {
		fallo := "10000" + string(rune('0'+i%10))
		if fallo == bueno {
			continue
		}
		ultimo = do(t, srv, http.MethodPost, "/api/v1/me/correo/confirmar", s.Token,
			map[string]string{"codigo": fallo}, nil)
	}
	if ultimo != http.StatusGone {
		t.Fatalf("código final = %d, esperaba 410 al agotarse los intentos", ultimo)
	}
	// Y ni siquiera el bueno vale ya.
	if code := do(t, srv, http.MethodPost, "/api/v1/me/correo/confirmar", s.Token,
		map[string]string{"codigo": bueno}, nil); code != http.StatusGone {
		t.Fatalf("el código bueno tras agotar los intentos: código = %d", code)
	}
}

func TestReenviarMandaOtroCodigoYMataElAnterior(t *testing.T) {
	srv := newTestServer(t)
	s := registrarSinVerificar(t, srv, "Ana", "ana@example.com")
	primero := ultimoCodigo(t, srv, "ana@example.com")

	if code := do(t, srv, http.MethodPost, "/api/v1/me/correo/reenviar", s.Token,
		map[string]any{}, nil); code != http.StatusAccepted {
		t.Fatalf("reenviar: código = %d", code)
	}
	segundo := ultimoCodigo(t, srv, "ana@example.com")
	if primero == segundo {
		t.Fatal("el reenvío mandó el mismo código")
	}

	if code := do(t, srv, http.MethodPost, "/api/v1/me/correo/confirmar", s.Token,
		map[string]string{"codigo": primero}, nil); code == http.StatusOK {
		t.Fatal("el código anterior sigue valiendo")
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/me/correo/confirmar", s.Token,
		map[string]string{"codigo": segundo}, nil); code != http.StatusOK {
		t.Fatalf("el código nuevo no vale: código = %d", code)
	}
}

func TestNadieConfirmaElBuzonDeOtro(t *testing.T) {
	srv := newTestServer(t)
	registrarSinVerificar(t, srv, "Ana", "ana@example.com")
	berta := registrarSinVerificar(t, srv, "Berta", "berta@example.com")
	deAna := ultimoCodigo(t, srv, "ana@example.com")

	// Berta prueba con el código de Ana: el suyo es otro, así que falla.
	if code := do(t, srv, http.MethodPost, "/api/v1/me/correo/confirmar", berta.Token,
		map[string]string{"codigo": deAna}, nil); code == http.StatusOK {
		t.Fatal("el código de una persona acreditó el buzón de otra")
	}
}
