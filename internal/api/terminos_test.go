package api_test

import (
	"net/http"
	"testing"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
)

func TestElRegistroSinAceptarLasCondicionesSeRechaza(t *testing.T) {
	srv := newTestServer(t)

	var problema struct {
		Error           string `json:"error"`
		TerminosVersion string `json:"terminos_version"`
	}
	code := do(t, srv, http.MethodPost, "/api/v1/auth/register", "",
		map[string]any{
			"name": "Ana", "email": "ana@example.com",
			"password": password, "idioma": "es", "acepta_terminos": false,
		}, &problema)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("código = %d, esperaba 422", code)
	}
	// La respuesta dice qué redacción hay que aceptar: sin eso, la interfaz no
	// puede enseñar el texto correcto.
	if problema.TerminosVersion != domain.VersionTerminos {
		t.Fatalf("terminos_version = %q, esperaba %q", problema.TerminosVersion, domain.VersionTerminos)
	}

	// Y no ha quedado ninguna cuenta a medias: se puede registrar ese correo.
	if code := do(t, srv, http.MethodPost, "/api/v1/auth/register", "",
		altaDe("Ana", "ana@example.com"), nil); code != http.StatusCreated {
		t.Fatalf("registro posterior: código = %d, esperaba 201", code)
	}
}

func TestElAltaDejaConstanciaDeLaVersionAceptada(t *testing.T) {
	srv := newTestServer(t)
	s := registrarSinVerificar(t, srv, "Ana", "ana@example.com")

	var yo struct {
		TerminosVersion string  `json:"terminos_version"`
		TerminosAt      *string `json:"terminos_at"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/me", s.Token, nil, &yo); code != http.StatusOK {
		t.Fatalf("GET /me: código = %d", code)
	}
	if yo.TerminosVersion != domain.VersionTerminos {
		t.Fatalf("versión = %q, esperaba %q", yo.TerminosVersion, domain.VersionTerminos)
	}
	if yo.TerminosAt == nil {
		t.Fatal("no quedó constancia de cuándo se aceptaron")
	}
}

func TestSePuedeRenovarLaAceptacionDesdeLaAPI(t *testing.T) {
	srv := newTestServer(t)
	s := registrarSinVerificar(t, srv, "Ana", "ana@example.com")

	var yo struct {
		TerminosVersion string `json:"terminos_version"`
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/me/terminos", s.Token, nil, &yo); code != http.StatusOK {
		t.Fatalf("código = %d, esperaba 200", code)
	}
	if yo.TerminosVersion != domain.VersionTerminos {
		t.Fatalf("versión = %q tras aceptar", yo.TerminosVersion)
	}
}

func TestLaVersionVigenteEsPublica(t *testing.T) {
	// La interfaz la necesita sin sesión: es lo que le dice qué redacción
	// enseñar en el formulario de alta.
	srv := newTestServer(t)
	var cfg struct {
		TerminosVersion string `json:"terminos_version"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/config", "", nil, &cfg); code != http.StatusOK {
		t.Fatalf("código = %d", code)
	}
	if cfg.TerminosVersion != domain.VersionTerminos {
		t.Fatalf("terminos_version = %q, esperaba %q", cfg.TerminosVersion, domain.VersionTerminos)
	}
}
