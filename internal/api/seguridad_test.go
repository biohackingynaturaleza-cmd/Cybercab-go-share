package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
)

// viajeEnMarcha deja una plaza confirmada: dos personas que van a ir en el
// mismo coche.
func viajeEnMarcha(t *testing.T, srv *entorno) (ana, carla sesion, tripID string) {
	t.Helper()
	ana = registrar(t, srv, "Ana Torres", "ana@example.com")
	carla = registrar(t, srv, "Carla Ruiz", "carla@example.com")
	tripID = publicarTrayecto(t, srv, ana.Token)

	var booking map[string]any
	if code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/bookings", carla.Token, map[string]any{
		"pickup": lugar("Riverside", riverside), "dropoff": lugar("AUS", aus),
	}, &booking); code != http.StatusCreated {
		t.Fatalf("reserva: código = %d (%v)", code, booking)
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/bookings/"+booking["id"].(string)+"/decision",
		ana.Token, map[string]any{"accept": true, "acepta_responsabilidad": true}, nil); code != http.StatusOK {
		t.Fatalf("confirmación: código = %d", code)
	}
	return ana, carla, tripID
}

func TestElViajeConfirmadoSaleComoActivo(t *testing.T) {
	srv := newTestServer(t)
	_, carla, tripID := viajeEnMarcha(t, srv)

	var body struct {
		Viajes []struct {
			TripID string `json:"trip_id"`
			Papel  string `json:"papel"`
		} `json:"viajes"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/me/viajes-activos", carla.Token, nil, &body); code != http.StatusOK {
		t.Fatalf("código = %d", code)
	}
	if len(body.Viajes) != 1 || body.Viajes[0].TripID != tripID || body.Viajes[0].Papel != "pasajero" {
		t.Fatalf("viajes = %+v", body.Viajes)
	}
}

func TestCompartirElViajeYSeguirloSinCuenta(t *testing.T) {
	srv := newTestServer(t)
	_, carla, tripID := viajeEnMarcha(t, srv)

	var enlace struct {
		URL string `json:"url"`
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/compartir",
		carla.Token, map[string]any{}, &enlace); code != http.StatusCreated {
		t.Fatalf("compartir: código = %d", code)
	}
	_, testigo, ok := strings.Cut(enlace.URL, "?t=")
	if !ok || testigo == "" {
		t.Fatalf("el enlace no lleva testigo: %q", enlace.URL)
	}

	// Sin sesión: el contacto de confianza de alguien no tiene por qué
	// registrarse aquí para saber que su hija llegó bien.
	var vista struct {
		Comparte  string `json:"comparte"`
		Telefono  string `json:"telefono_emergencias"`
		Ocupantes []struct {
			Nombre string `json:"nombre"`
		} `json:"ocupantes"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/seguimiento?t="+testigo, "", nil, &vista); code != http.StatusOK {
		t.Fatalf("seguimiento sin sesión: código = %d", code)
	}
	if vista.Comparte != "Carla" {
		t.Fatalf("comparte = %q, esperaba solo el primer nombre", vista.Comparte)
	}
	if len(vista.Ocupantes) != 2 {
		t.Fatalf("ocupantes = %+v", vista.Ocupantes)
	}
	for _, o := range vista.Ocupantes {
		if strings.Contains(o.Nombre, " ") {
			t.Fatalf("ocupante %q: se enseña el nombre completo a un extraño", o.Nombre)
		}
	}
	if vista.Telefono != domain.TelefonoEmergencias {
		t.Fatalf("teléfono = %q", vista.Telefono)
	}

	// Revocado, deja de enseñar nada.
	if code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/dejar-de-compartir",
		carla.Token, map[string]any{}, nil); code != http.StatusOK {
		t.Fatalf("dejar de compartir: código = %d", code)
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/seguimiento?t="+testigo, "", nil, nil); code != http.StatusGone {
		t.Fatalf("tras revocar: código = %d, esperaba 410", code)
	}
}

func TestUnTestigoInventadoNoSigueNada(t *testing.T) {
	srv := newTestServer(t)
	for _, testigo := range []string{"", "no-es-un-testigo"} {
		if code := do(t, srv, http.MethodGet, "/api/v1/seguimiento?t="+testigo, "", nil, nil); code != http.StatusGone {
			t.Fatalf("testigo %q: código = %d, esperaba 410", testigo, code)
		}
	}
}

func TestSoloComparteQuienVaEnElViaje(t *testing.T) {
	srv := newTestServer(t)
	_, _, tripID := viajeEnMarcha(t, srv)
	extraño := registrar(t, srv, "Eva", "eva@example.com")

	if code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/compartir",
		extraño.Token, map[string]any{}, nil); code != http.StatusForbidden {
		t.Fatalf("código = %d, esperaba 403", code)
	}
}

func TestElBotonDeEmergenciaDePuntaAPunta(t *testing.T) {
	srv := newTestServer(t)
	_, carla, tripID := viajeEnMarcha(t, srv)

	if code := do(t, srv, http.MethodPost, "/api/v1/me/contactos", carla.Token,
		map[string]any{"nombre": "Madre", "email": "madre@example.com", "avisar_al_salir": false},
		nil); code != http.StatusCreated {
		t.Fatalf("contacto: código = %d", code)
	}

	var r struct {
		Alerta struct {
			ID     string `json:"id"`
			Estado string `json:"estado"`
		} `json:"alerta"`
		Telefono string `json:"telefono_emergencias"`
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/emergencia", carla.Token,
		map[string]any{"lat": 30.2672, "lng": -97.7431, "nota": "Me ha pedido bajarme en otro sitio"},
		&r); code != http.StatusCreated {
		t.Fatalf("emergencia: código = %d", code)
	}
	if r.Alerta.Estado != "abierta" || r.Telefono != domain.TelefonoEmergencias {
		t.Fatalf("respuesta = %+v", r)
	}

	// El contacto recibe el aviso con un enlace que funciona.
	aviso, ok := srv.avisos.Ultimo(notify.SucesoAlertaDisparada)
	if !ok {
		t.Fatal("no se avisó a los contactos")
	}
	_, testigo, _ := strings.Cut(aviso.Datos["enlace"], "?t=")
	var vista struct {
		Alerta *struct {
			ID string `json:"id"`
		} `json:"alerta"`
		UltimaPosicion *struct {
			Lat float64 `json:"lat"`
		} `json:"ultima_posicion"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/seguimiento?t="+testigo, "", nil, &vista); code != http.StatusOK {
		t.Fatalf("el enlace del aviso: código = %d", code)
	}
	if vista.Alerta == nil || vista.Alerta.ID != r.Alerta.ID {
		t.Fatalf("el enlace no enseña la alerta: %+v", vista.Alerta)
	}
	if vista.UltimaPosicion == nil {
		t.Fatal("el enlace no enseña dónde estaba")
	}

	// Operaciones la ve la primera.
	var cola struct {
		Alertas []map[string]any `json:"alertas"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/operaciones/alertas", "", nil, &cola); code != http.StatusNotFound {
		t.Fatalf("sin token de operaciones: código = %d, esperaba 404", code)
	}

	// Retirarla manda la corrección.
	if code := do(t, srv, http.MethodPost, "/api/v1/alertas/"+r.Alerta.ID+"/retirar",
		carla.Token, map[string]any{}, nil); code != http.StatusOK {
		t.Fatalf("retirar: código = %d", code)
	}
	if _, ok := srv.avisos.Ultimo(notify.SucesoAlertaRetirada); !ok {
		t.Fatal("no se mandó la corrección de la falsa alarma")
	}
}

func TestElBotonSaleSinCoordenadas(t *testing.T) {
	// Sin permiso de ubicación el botón sigue funcionando.
	srv := newTestServer(t)
	_, carla, tripID := viajeEnMarcha(t, srv)
	if code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/emergencia", carla.Token,
		map[string]any{"lat": nil, "lng": nil, "nota": ""}, nil); code != http.StatusCreated {
		t.Fatalf("código = %d, esperaba 201", code)
	}
}

func TestOperacionesVeLasAlertasYLasCierra(t *testing.T) {
	srv := servidorConOperaciones(t)
	_, carla, tripID := viajeEnMarcha(t, srv)

	var r struct {
		Alerta struct {
			ID string `json:"id"`
		} `json:"alerta"`
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/emergencia", carla.Token,
		map[string]any{}, &r); code != http.StatusCreated {
		t.Fatalf("emergencia: código = %d", code)
	}

	var cola struct {
		Alertas []map[string]any `json:"alertas"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/operaciones/alertas", tokenDeOperaciones, nil, &cola); code != http.StatusOK {
		t.Fatalf("cola: código = %d", code)
	}
	if len(cola.Alertas) != 1 {
		t.Fatalf("alertas = %d, esperaba 1", len(cola.Alertas))
	}

	if code := do(t, srv, http.MethodPost, "/api/v1/operaciones/alertas/"+r.Alerta.ID+"/atender",
		tokenDeOperaciones, map[string]any{"resolucion": "Hablado con ella, está bien."},
		nil); code != http.StatusOK {
		t.Fatalf("atender: código = %d", code)
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/operaciones/alertas", tokenDeOperaciones, nil, &cola); code != http.StatusOK {
		t.Fatalf("cola: código = %d", code)
	}
	if len(cola.Alertas) != 0 {
		t.Fatalf("alertas tras atender = %d", len(cola.Alertas))
	}
}

func TestNoSePuedeDenunciarNiAlertarSobreUnViajeAjeno(t *testing.T) {
	srv := newTestServer(t)
	_, _, tripID := viajeEnMarcha(t, srv)
	extraño := registrar(t, srv, "Eva", "eva@example.com")

	if code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/emergencia",
		extraño.Token, map[string]any{}, nil); code != http.StatusForbidden {
		t.Fatalf("código = %d, esperaba 403", code)
	}
}

func TestLosContactosTienenTope(t *testing.T) {
	srv := newTestServer(t)
	s := registrarSinVerificar(t, srv, "Ana", "ana@example.com")

	for i := range domain.MaxContactos {
		correo := string(rune('a'+i)) + "@example.com"
		if code := do(t, srv, http.MethodPost, "/api/v1/me/contactos", s.Token,
			map[string]any{"nombre": "Contacto", "email": correo}, nil); code != http.StatusCreated {
			t.Fatalf("contacto %d: código = %d", i, code)
		}
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/me/contactos", s.Token,
		map[string]any{"nombre": "Uno más", "email": "z@example.com"}, nil); code != http.StatusConflict {
		t.Fatalf("código = %d, esperaba 409", code)
	}

	var body struct {
		Contactos []map[string]any `json:"contactos"`
		Maximo    int              `json:"maximo"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/me/contactos", s.Token, nil, &body); code != http.StatusOK {
		t.Fatalf("código = %d", code)
	}
	if len(body.Contactos) != domain.MaxContactos || body.Maximo != domain.MaxContactos {
		t.Fatalf("contactos = %d, máximo = %d", len(body.Contactos), body.Maximo)
	}
}
