package api_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// La interfaz va embebida en el binario: si alguien la mueve o la renombra,
// estas pruebas fallan antes de que se despliegue un servidor sin cara.

func TestLaInterfazSeSirve(t *testing.T) {
	srv := newTestServer(t)

	for _, ruta := range []string{"/", "/app.css", "/app.js"} {
		resp, err := srv.Client().Get(srv.URL + ruta)
		if err != nil {
			t.Fatalf("GET %s: %v", ruta, err)
		}
		cuerpo, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: código = %d", ruta, resp.StatusCode)
		}
		if len(cuerpo) == 0 {
			t.Errorf("GET %s: cuerpo vacío", ruta)
		}
	}
}

func TestUnaRutaDesconocidaDevuelveLaInterfaz(t *testing.T) {
	// La interfaz es una sola página: recargar en una vista interna tiene que
	// funcionar en vez de dar un 404.
	srv := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/mis-viajes")
	if err != nil {
		t.Fatalf("petición: %v", err)
	}
	defer resp.Body.Close()
	cuerpo, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("código = %d, esperaba 200", resp.StatusCode)
	}
	if !strings.Contains(string(cuerpo), "Cybercab") {
		t.Fatal("no se devolvió la interfaz")
	}
}

func TestLaInterfazNoSeComeLaAPI(t *testing.T) {
	// Montar la web en "/" no puede tapar las rutas de la API.
	srv := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/api/v1/trips/no_existe")
	if err != nil {
		t.Fatalf("petición: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("código = %d, esperaba 404 de la API y no la interfaz", resp.StatusCode)
	}
}

func TestLaInterfazLlevaCabecerasDeSeguridad(t *testing.T) {
	srv := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("petición: %v", err)
	}
	defer resp.Body.Close()

	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP = %q: los scripts deben venir solo de este servidor", csp)
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Error("falta X-Frame-Options: la página maneja sesiones")
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("falta X-Content-Type-Options")
	}
}

func TestLasZonasSonPublicas(t *testing.T) {
	srv := newTestServer(t)
	var body struct {
		Zonas []struct {
			Nombre string  `json:"nombre"`
			Lat    float64 `json:"lat"`
		} `json:"zonas"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/zonas", "", nil, &body); code != http.StatusOK {
		t.Fatalf("código = %d", code)
	}
	if len(body.Zonas) < 5 {
		t.Fatalf("zonas = %d, esperaba el área de servicio completa", len(body.Zonas))
	}
	for _, z := range body.Zonas {
		if z.Nombre == "" || z.Lat == 0 {
			t.Errorf("zona incompleta: %+v", z)
		}
	}
}
