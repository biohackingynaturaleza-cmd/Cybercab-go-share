package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/api"
)

// servidorConLimites monta el servidor con los techos de producción, que son
// los que estas pruebas quieren ejercitar. Son los de siempre: no hay una
// variante relajada, para que nadie pruebe contra unos límites que luego no
// son los que corren.
func servidorConLimites(t *testing.T) *entorno {
	t.Helper()
	return nuevoEntorno(t)
}

// pedir es como do, pero devuelve la respuesta entera: aquí importan las
// cabeceras, no solo el código.
func pedir(t *testing.T, e *entorno, method, path string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("codificando el cuerpo: %v", err)
	}
	req, err := http.NewRequest(method, e.URL+path, &buf)
	if err != nil {
		t.Fatalf("construyendo la petición: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func TestProbarContrasenasSeCortaConUn429(t *testing.T) {
	e := servidorConLimites(t)
	registrarSinVerificar(t, e, "Ana", "ana@ejemplo.test")

	// Contraseñas equivocadas contra la misma cuenta: el techo por cuenta es el
	// más estrecho, así que salta antes que el de IP.
	var ultimo int
	intentos := 0
	for ; intentos < api.IntentosPorCuenta+5; intentos++ {
		ultimo = do(t, e, http.MethodPost, "/api/v1/auth/login", "",
			map[string]string{"email": "ana@ejemplo.test", "password": "no-es-esta-clave"}, nil)
		if ultimo == http.StatusTooManyRequests {
			break
		}
		if ultimo != http.StatusUnauthorized {
			t.Fatalf("intento %d: código = %d, esperaba 401", intentos+1, ultimo)
		}
	}
	if ultimo != http.StatusTooManyRequests {
		t.Fatalf("tras %d intentos fallidos seguidos nadie cortó", intentos)
	}
	if intentos > api.IntentosPorCuenta {
		t.Fatalf("hicieron falta %d intentos para cortar, y el techo es %d",
			intentos, api.IntentosPorCuenta)
	}
}

func TestElCorteDiceCuandoVolver(t *testing.T) {
	// Un 429 sin Retry-After obliga a reintentar a ciegas, que es justo lo que
	// hace la gente cuando la app le dice que no y no le dice cuándo sí.
	e := servidorConLimites(t)
	registrarSinVerificar(t, e, "Ana", "ana@ejemplo.test")

	var resp *http.Response
	for range api.IntentosPorCuenta + 5 {
		resp = pedir(t, e, http.MethodPost, "/api/v1/auth/login",
			map[string]string{"email": "ana@ejemplo.test", "password": "no-es-esta-clave"})
		if resp.StatusCode == http.StatusTooManyRequests {
			break
		}
		resp.Body.Close()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("código = %d, esperaba 429", resp.StatusCode)
	}
	espera := resp.Header.Get("Retry-After")
	if espera == "" {
		t.Fatal("el 429 no trae Retry-After")
	}
	segundos, err := strconv.Atoi(espera)
	if err != nil || segundos < 1 {
		t.Fatalf("Retry-After = %q, esperaba un número de segundos", espera)
	}
}

func TestElTechoDeUnaCuentaNoAfectaALasDemas(t *testing.T) {
	// Si bastara con machacar una cuenta ajena para dejarla fuera, el límite
	// sería un arma en vez de una defensa.
	e := servidorConLimites(t)
	registrarSinVerificar(t, e, "Ana", "ana@ejemplo.test")
	registrarSinVerificar(t, e, "Berta", "berta@ejemplo.test")

	for range api.IntentosPorCuenta + 2 {
		do(t, e, http.MethodPost, "/api/v1/auth/login", "",
			map[string]string{"email": "ana@ejemplo.test", "password": "no-es-esta-clave"}, nil)
	}

	var s sesion
	if code := do(t, e, http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"email": "berta@ejemplo.test", "password": password}, &s); code != http.StatusOK {
		t.Fatalf("berta no puede entrar: código = %d", code)
	}
}

func TestPedirLaRecuperacionEnBucleTambienSeCorta(t *testing.T) {
	e := servidorConLimites(t)
	registrarSinVerificar(t, e, "Ana", "ana@ejemplo.test")

	var ultimo int
	for range api.IntentosPorCuenta + 5 {
		ultimo = do(t, e, http.MethodPost, "/api/v1/auth/recuperar", "",
			map[string]string{"email": "ana@ejemplo.test"}, nil)
		if ultimo == http.StatusTooManyRequests {
			break
		}
	}
	if ultimo != http.StatusTooManyRequests {
		t.Fatalf("código final = %d: se puede inundar un buzón ajeno sin freno", ultimo)
	}
}
