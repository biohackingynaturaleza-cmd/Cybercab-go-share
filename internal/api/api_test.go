package api_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
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

const password = "contraseña-de-prueba"

var (
	centro    = punto(30.2685, -97.7425)
	riverside = punto(30.2380, -97.7180)
	aus       = punto(30.1975, -97.6664)
)

func punto(lat, lng float64) map[string]float64 {
	return map[string]float64{"lat": lat, "lng": lng}
}

func lugar(name string, p map[string]float64) map[string]any {
	return map[string]any{"name": name, "point": p}
}

// entorno es el servidor de pruebas junto al proveedor de identidad, que hace
// falta para resolver las verificaciones a mano.
type entorno struct {
	*httptest.Server
	identidad *trust.Manual
	// avisos guarda los correos enviados. Hace falta para acreditar el buzón:
	// el código llega por correo, así que la única forma de recorrer el mismo
	// camino que una persona es leerlo de ahí.
	avisos *notify.Grabador
}

// nuevoEntorno monta el servidor de pruebas. Todas las variantes pasan por
// aquí: cada builder suelto que montaba el suyo acababa olvidándose de alguna
// pieza —el grabador de correos, sin ir más lejos— y fallando de formas raras
// lejos de la causa.
func nuevoEntorno(t *testing.T, opciones ...api.Option) *entorno {
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
	srv := httptest.NewServer(api.NewServer(svc, tokens, log, opciones...))
	t.Cleanup(srv.Close)
	return &entorno{Server: srv, identidad: identidad, avisos: avisos}
}

func newTestServer(t *testing.T) *entorno {
	t.Helper()
	return nuevoEntorno(t)
}

// confirmarCorreo acredita el buzón tecleando el código que llegó por correo,
// que es lo que hace una persona.
func confirmarCorreo(t *testing.T, e *entorno, token, correo string) {
	t.Helper()
	codigo := ultimoCodigo(t, e, correo)
	if code := do(t, e, http.MethodPost, "/api/v1/me/correo/confirmar", token,
		map[string]string{"codigo": codigo}, nil); code != http.StatusOK {
		t.Fatalf("confirmando el correo de %s: código = %d", correo, code)
	}
}

// ultimoCodigo saca de los correos enviados el último código para esa
// dirección.
func ultimoCodigo(t *testing.T, e *entorno, correo string) string {
	t.Helper()
	avisos := e.avisos.Para(correo)
	for i := len(avisos) - 1; i >= 0; i-- {
		if avisos[i].Suceso == notify.SucesoCodigoCorreo {
			return avisos[i].Datos["codigo"]
		}
	}
	t.Fatalf("no se mandó ningún código a %s", correo)
	return ""
}

// acreditar lleva a alguien hasta la identidad verificada por los mismos
// endpoints que usaría la aplicación.
func acreditar(t *testing.T, e *entorno, token string) {
	t.Helper()
	// El buzón no pasa por el proveedor de identidad: lo acredita el código.
	tipos := []string{"phone", "government_id", "selfie_liveness"}
	for _, kind := range tipos {
		var abierta struct {
			Verificacion struct {
				ProviderRef string `json:"provider_ref"`
			} `json:"verificacion"`
		}
		if code := do(t, e, http.MethodPost, "/api/v1/me/verificaciones", token,
			map[string]string{"kind": kind}, &abierta); code != http.StatusCreated {
			t.Fatalf("abriendo verificación %s: código = %d", kind, code)
		}
		ref := abierta.Verificacion.ProviderRef
		if err := e.identidad.Resolve(ref, trust.Outcome{Status: trust.StatusVerified}); err != nil {
			t.Fatalf("Resolve(%s): %v", kind, err)
		}
		if code := do(t, e, http.MethodPost,
			"/api/v1/me/verificaciones/"+ref+"/refrescar", token, nil, nil); code != http.StatusOK {
			t.Fatalf("refrescando %s: código = %d", kind, code)
		}
	}
}

// do lanza una petición autenticada con token (vacío = sin autenticar) y
// descodifica la respuesta JSON en out.
func do(t *testing.T, srv *entorno, method, path, token string, body, out any) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("codificando la petición: %v", err)
		}
	}
	req, err := http.NewRequest(method, srv.URL+path, &buf)
	if err != nil {
		t.Fatalf("construyendo la petición: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("descodificando %s %s: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

// altaDe es el cuerpo de un registro válido. La aceptación de las condiciones
// va aquí y no por defecto en el servidor: es lo que se está exigiendo.
func altaDe(name, email string) map[string]any {
	return map[string]any{
		"name": name, "email": email, "password": password,
		"idioma": "es", "acepta_terminos": true,
	}
}

type sesion struct {
	Token string `json:"token"`
	User  struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"user"`
}

func registrar(t *testing.T, srv *entorno, name, email string) sesion {
	t.Helper()
	var s sesion
	code := do(t, srv, http.MethodPost, "/api/v1/auth/register", "",
		altaDe(name, email), &s)
	if code != http.StatusCreated {
		t.Fatalf("registro de %s: código = %d", name, code)
	}
	if s.Token == "" {
		t.Fatalf("el registro de %s no devolvió token", name)
	}
	// Por defecto, identidad acreditada: la mayoría de pruebas van de otra
	// cosa, y las que van de confianza usan registrarSinVerificar.
	confirmarCorreo(t, srv, s.Token, email)
	acreditar(t, srv, s.Token)
	return s
}

// registrarSinVerificar da de alta a alguien sin acreditar nada.
func registrarSinVerificar(t *testing.T, srv *entorno, name, email string) sesion {
	t.Helper()
	var s sesion
	code := do(t, srv, http.MethodPost, "/api/v1/auth/register", "",
		altaDe(name, email), &s)
	if code != http.StatusCreated {
		t.Fatalf("registro de %s: código = %d", name, code)
	}
	return s
}

// publicarTrayecto crea un viaje centro → aeropuerto y devuelve su id.
func publicarTrayecto(t *testing.T, srv *entorno, token string) string {
	t.Helper()
	var trip map[string]any
	code := do(t, srv, http.MethodPost, "/api/v1/trips", token, map[string]any{
		"origin":         lugar("Centro", centro),
		"destination":    lugar("AUS", aus),
		"departure_time": time.Now().UTC().Add(3 * time.Hour).Format(time.RFC3339),
		"vehicle":        "model_y",
		"max_detour_km":  2,
	}, &trip)
	if code != http.StatusCreated {
		t.Fatalf("alta del trayecto: código = %d (%v)", code, trip)
	}
	return trip["id"].(string)
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t)
	var body map[string]string
	if code := do(t, srv, http.MethodGet, "/healthz", "", nil, &body); code != http.StatusOK {
		t.Fatalf("código = %d, esperaba 200", code)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

// --- Acceso ---

func TestRegistroYLogin(t *testing.T) {
	srv := newTestServer(t)
	s := registrar(t, srv, "Ana", "ana@example.com")

	var login sesion
	if code := do(t, srv, http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"email": "ana@example.com", "password": password}, &login); code != http.StatusOK {
		t.Fatalf("login: código = %d", code)
	}
	if login.User.ID != s.User.ID {
		t.Fatalf("el login devolvió otro usuario: %q vs %q", login.User.ID, s.User.ID)
	}

	var me map[string]any
	if code := do(t, srv, http.MethodGet, "/api/v1/me", login.Token, nil, &me); code != http.StatusOK {
		t.Fatalf("/me: código = %d", code)
	}
	if me["id"] != s.User.ID {
		t.Fatalf("/me devolvió %v, esperaba %v", me["id"], s.User.ID)
	}
}

func TestLaContrasenaNuncaSaleEnLaRespuesta(t *testing.T) {
	srv := newTestServer(t)
	s := registrar(t, srv, "Ana", "ana@example.com")

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/users/"+s.User.ID, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("petición: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	for _, prohibido := range []string{password, "password", "PasswordHash", "$2a$"} {
		if bytes.Contains(raw, []byte(prohibido)) {
			t.Errorf("la respuesta contiene %q: %s", prohibido, raw)
		}
	}
}

func TestLoginConCredencialesMalasDevuelve401(t *testing.T) {
	srv := newTestServer(t)
	registrar(t, srv, "Ana", "ana@example.com")

	cases := map[string]map[string]string{
		"contraseña incorrecta": {"email": "ana@example.com", "password": "otra-cosa-larga"},
		"usuario inexistente":   {"email": "nadie@example.com", "password": password},
	}
	for name, body := range cases {
		if code := do(t, srv, http.MethodPost, "/api/v1/auth/login", "", body, nil); code != http.StatusUnauthorized {
			t.Errorf("%s: código = %d, esperaba 401", name, code)
		}
	}
}

func TestNoSePuedeRegistrarDosVecesElMismoEmail(t *testing.T) {
	srv := newTestServer(t)
	registrar(t, srv, "Ana", "ana@example.com")

	code := do(t, srv, http.MethodPost, "/api/v1/auth/register", "",
		altaDe("Otra Ana", "ANA@example.com"), nil)
	if code != http.StatusConflict {
		t.Fatalf("código = %d, esperaba 409: el email ya está en uso", code)
	}
}

func TestRegistroConContrasenaCortaDevuelve422(t *testing.T) {
	srv := newTestServer(t)
	code := do(t, srv, http.MethodPost, "/api/v1/auth/register", "",
		map[string]string{"name": "Ana", "email": "ana@example.com", "password": "corta"}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("código = %d, esperaba 422", code)
	}
}

func TestLasRutasProtegidasExigenToken(t *testing.T) {
	srv := newTestServer(t)
	ana := registrar(t, srv, "Ana", "ana@example.com")
	tripID := publicarTrayecto(t, srv, ana.Token)

	protegidas := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/me"},
		{http.MethodGet, "/api/v1/me/bookings"},
		{http.MethodPost, "/api/v1/trips"},
		{http.MethodPost, "/api/v1/trips/" + tripID + "/cancel"},
		{http.MethodGet, "/api/v1/trips/" + tripID + "/bookings"},
		{http.MethodPost, "/api/v1/trips/" + tripID + "/bookings"},
		{http.MethodPost, "/api/v1/bookings/bkg_x/decision"},
		{http.MethodPost, "/api/v1/bookings/bkg_x/cancel"},
	}
	for _, ruta := range protegidas {
		body := map[string]any{}
		if code := do(t, srv, ruta.method, ruta.path, "", body, nil); code != http.StatusUnauthorized {
			t.Errorf("%s %s: código = %d, esperaba 401", ruta.method, ruta.path, code)
		}
	}
}

// --- Recorrido completo ---

// TestRecorridoCompletoCentroAeropuerto reproduce el caso de uso central: Ana
// publica un trayecto al aeropuerto, Carla lo encuentra, pide plaza, Ana la
// acepta y el coste queda repartido entre las dos.
func TestRecorridoCompletoCentroAeropuerto(t *testing.T) {
	srv := newTestServer(t)
	ana := registrar(t, srv, "Ana", "ana@example.com")
	carla := registrar(t, srv, "Carla", "carla@example.com")

	tripID := publicarTrayecto(t, srv, ana.Token)

	var search map[string]any
	if code := do(t, srv, http.MethodPost, "/api/v1/search", "", map[string]any{
		"pickup": riverside, "dropoff": aus,
	}, &search); code != http.StatusOK {
		t.Fatalf("búsqueda: código = %d", code)
	}
	if search["count"].(float64) != 1 {
		t.Fatalf("resultados = %v, esperaba 1", search["count"])
	}

	var booking map[string]any
	code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/bookings", carla.Token, map[string]any{
		"pickup":  lugar("Riverside", riverside),
		"dropoff": lugar("AUS", aus),
	}, &booking)
	if code != http.StatusCreated {
		t.Fatalf("reserva: código = %d (%v)", code, booking)
	}
	if booking["passenger_id"] != carla.User.ID {
		t.Fatalf("la reserva se atribuyó a %v, esperaba a Carla", booking["passenger_id"])
	}

	var decided map[string]any
	code = do(t, srv, http.MethodPost, "/api/v1/bookings/"+booking["id"].(string)+"/decision",
		ana.Token, map[string]any{"accept": true, "acepta_responsabilidad": true}, &decided)
	if code != http.StatusOK || decided["status"] != "confirmed" {
		t.Fatalf("confirmación: código = %d, estado = %v", code, decided["status"])
	}

	var fare struct {
		TotalCents int64 `json:"total_cents"`
		Shares     []struct {
			UserID      string `json:"user_id"`
			Role        string `json:"role"`
			AmountCents int64  `json:"amount_cents"`
		} `json:"shares"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/trips/"+tripID+"/fare", "", nil, &fare); code != http.StatusOK {
		t.Fatalf("desglose: código = %d", code)
	}
	if len(fare.Shares) != 2 {
		t.Fatalf("partes = %d, esperaba 2", len(fare.Shares))
	}
	var sum int64
	for _, s := range fare.Shares {
		if s.AmountCents <= 0 {
			t.Errorf("%s (%s) paga %d, esperaba un importe positivo", s.UserID, s.Role, s.AmountCents)
		}
		sum += s.AmountCents
	}
	if sum != fare.TotalCents {
		t.Fatalf("las partes suman %d, esperaba el total %d", sum, fare.TotalCents)
	}
}

// --- Autorización ---

func TestNadieMasPuedeDecidirSobreTuReserva(t *testing.T) {
	srv := newTestServer(t)
	ana := registrar(t, srv, "Ana", "ana@example.com")
	carla := registrar(t, srv, "Carla", "carla@example.com")
	intrusa := registrar(t, srv, "Eva", "eva@example.com")

	tripID := publicarTrayecto(t, srv, ana.Token)

	var booking map[string]any
	do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/bookings", carla.Token, map[string]any{
		"pickup":  lugar("Riverside", riverside),
		"dropoff": lugar("AUS", aus),
	}, &booking)

	code := do(t, srv, http.MethodPost, "/api/v1/bookings/"+booking["id"].(string)+"/decision",
		intrusa.Token, map[string]any{"accept": true, "acepta_responsabilidad": true}, nil)
	if code != http.StatusForbidden {
		t.Fatalf("código = %d, esperaba 403", code)
	}
}

func TestNadieMasPuedeAnularTuTrayecto(t *testing.T) {
	srv := newTestServer(t)
	ana := registrar(t, srv, "Ana", "ana@example.com")
	intrusa := registrar(t, srv, "Eva", "eva@example.com")
	tripID := publicarTrayecto(t, srv, ana.Token)

	if code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/cancel", intrusa.Token, nil, nil); code != http.StatusForbidden {
		t.Fatalf("código = %d, esperaba 403", code)
	}
}

func TestUnExtranoNoVeLasReservasDelTrayecto(t *testing.T) {
	srv := newTestServer(t)
	ana := registrar(t, srv, "Ana", "ana@example.com")
	carla := registrar(t, srv, "Carla", "carla@example.com")
	intrusa := registrar(t, srv, "Eva", "eva@example.com")

	tripID := publicarTrayecto(t, srv, ana.Token)
	do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/bookings", carla.Token, map[string]any{
		"pickup":  lugar("Riverside", riverside),
		"dropoff": lugar("AUS", aus),
	}, nil)

	if code := do(t, srv, http.MethodGet, "/api/v1/trips/"+tripID+"/bookings", intrusa.Token, nil, nil); code != http.StatusForbidden {
		t.Errorf("una extraña ve las reservas: código = %d, esperaba 403", code)
	}

	var propias map[string]any
	if code := do(t, srv, http.MethodGet, "/api/v1/trips/"+tripID+"/bookings", ana.Token, nil, &propias); code != http.StatusOK {
		t.Errorf("quien organiza no ve las reservas: código = %d", code)
	}
	if len(propias["bookings"].([]any)) != 1 {
		t.Errorf("reservas visibles para quien organiza = %v, esperaba 1", propias["bookings"])
	}
}

func TestNoPuedesReservarEnTuPropioTrayecto(t *testing.T) {
	srv := newTestServer(t)
	ana := registrar(t, srv, "Ana", "ana@example.com")
	tripID := publicarTrayecto(t, srv, ana.Token)

	code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/bookings", ana.Token, map[string]any{
		"pickup":  lugar("Riverside", riverside),
		"dropoff": lugar("AUS", aus),
	}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("código = %d, esperaba 422", code)
	}
}

// --- Errores ---

func TestTrayectoInexistenteDevuelve404(t *testing.T) {
	srv := newTestServer(t)
	if code := do(t, srv, http.MethodGet, "/api/v1/trips/trip_no_existe", "", nil, nil); code != http.StatusNotFound {
		t.Fatalf("código = %d, esperaba 404", code)
	}
}

func TestJSONInvalidoDevuelve400(t *testing.T) {
	srv := newTestServer(t)
	resp, err := srv.Client().Post(srv.URL+"/api/v1/auth/register", "application/json",
		bytes.NewBufferString("{no soy json"))
	if err != nil {
		t.Fatalf("petición: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("código = %d, esperaba 400", resp.StatusCode)
	}
}

func TestBusquedaConCoordenadasInvalidasDevuelve400(t *testing.T) {
	srv := newTestServer(t)
	code := do(t, srv, http.MethodPost, "/api/v1/search", "", map[string]any{
		"pickup": punto(999, -97.7425), "dropoff": aus,
	}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("código = %d, esperaba 400", code)
	}
}

func TestMetodoNoPermitido(t *testing.T) {
	srv := newTestServer(t)
	if code := do(t, srv, http.MethodDelete, "/api/v1/trips", "", nil, nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("código = %d, esperaba 405", code)
	}
}

// --- Confianza y seguridad ---

func TestSinIdentidadVerificadaNoSeReservaUnCybercab(t *testing.T) {
	srv := newTestServer(t)
	ana := registrar(t, srv, "Ana", "ana@example.com")
	// Bruno se registra pero no acredita nada.
	bruno := registrarSinVerificar(t, srv, "Bruno", "bruno@example.com")

	var trip map[string]any
	code := do(t, srv, http.MethodPost, "/api/v1/trips", ana.Token, map[string]any{
		"origin":         lugar("Centro", centro),
		"destination":    lugar("AUS", aus),
		"departure_time": time.Now().UTC().Add(3 * time.Hour).Format(time.RFC3339),
		"vehicle":        "cybercab",
	}, &trip)
	if code != http.StatusCreated {
		t.Fatalf("alta del trayecto: código = %d (%v)", code, trip)
	}
	// El trayecto anuncia el nivel que exige y por qué.
	if trip["nivel_exigido"] != "verificado" {
		t.Errorf("nivel exigido = %v, esperaba verificado", trip["nivel_exigido"])
	}
	if trip["motivo_nivel"] == "" {
		t.Error("el trayecto no explica por qué exige ese nivel")
	}

	var rechazo map[string]any
	code = do(t, srv, http.MethodPost, "/api/v1/trips/"+trip["id"].(string)+"/bookings",
		bruno.Token, map[string]any{
			"pickup":  lugar("Riverside", riverside),
			"dropoff": lugar("AUS", aus),
		}, &rechazo)

	if code != http.StatusForbidden {
		t.Fatalf("código = %d, esperaba 403", code)
	}
	// El rechazo dice qué falta, no solo que no.
	if rechazo["exigido"] != "verificado" || rechazo["actual"] != "nuevo" {
		t.Errorf("respuesta = %v", rechazo)
	}
	if _, ok := rechazo["te_falta"]; !ok {
		t.Error("el rechazo no dice qué comprobaciones faltan")
	}
	if rechazo["motivo"] == "" {
		t.Error("el rechazo no explica el motivo")
	}
}

func TestElPerfilDeConfianzaEsPublicoYNoFiltraDatos(t *testing.T) {
	srv := newTestServer(t)
	ana := registrar(t, srv, "Ana", "ana@example.com")

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/users/"+ana.User.ID+"/confianza", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("petición: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("código = %d, esperaba 200", resp.StatusCode)
	}
	var perfil map[string]any
	if err := json.Unmarshal(raw, &perfil); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if perfil["nivel"] != "verificado" {
		t.Errorf("nivel = %v, esperaba verificado", perfil["nivel"])
	}
	// El perfil dice qué se ha acreditado, nunca el dato acreditado ni el
	// contacto de la persona.
	for _, prohibido := range []string{"ana@example.com", "password", "$2a$", "provider_ref"} {
		if bytes.Contains(raw, []byte(prohibido)) {
			t.Errorf("el perfil público contiene %q: %s", prohibido, raw)
		}
	}
}

func TestBloquearOcultaYImpideReservar(t *testing.T) {
	srv := newTestServer(t)
	ana := registrar(t, srv, "Ana", "ana@example.com")
	bruno := registrar(t, srv, "Bruno", "bruno@example.com")
	tripID := publicarTrayecto(t, srv, ana.Token)

	// Antes de bloquear, Bruno ve el trayecto.
	var antes map[string]any
	do(t, srv, http.MethodPost, "/api/v1/search", bruno.Token,
		map[string]any{"pickup": riverside, "dropoff": aus}, &antes)
	if antes["count"].(float64) != 1 {
		t.Fatalf("resultados antes de bloquear = %v, esperaba 1", antes["count"])
	}

	if code := do(t, srv, http.MethodPost, "/api/v1/users/"+ana.User.ID+"/bloquear",
		bruno.Token, nil, nil); code != http.StatusOK {
		t.Fatalf("bloquear: código = %d", code)
	}

	var despues map[string]any
	do(t, srv, http.MethodPost, "/api/v1/search", bruno.Token,
		map[string]any{"pickup": riverside, "dropoff": aus}, &despues)
	if despues["count"].(float64) != 0 {
		t.Fatalf("resultados tras bloquear = %v, esperaba 0", despues["count"])
	}

	code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/bookings", bruno.Token,
		map[string]any{"pickup": lugar("Riverside", riverside), "dropoff": lugar("AUS", aus)}, nil)
	if code != http.StatusForbidden {
		t.Fatalf("reserva tras bloquear: código = %d, esperaba 403", code)
	}
}

func TestNoSePuedeRefrescarLaVerificacionDeOtraPersona(t *testing.T) {
	srv := newTestServer(t)
	ana := registrarSinVerificar(t, srv, "Ana", "ana@example.com")
	intrusa := registrarSinVerificar(t, srv, "Eva", "eva@example.com")

	var abierta struct {
		Verificacion struct {
			ProviderRef string `json:"provider_ref"`
		} `json:"verificacion"`
	}
	do(t, srv, http.MethodPost, "/api/v1/me/verificaciones", ana.Token,
		map[string]string{"kind": "government_id"}, &abierta)

	code := do(t, srv, http.MethodPost,
		"/api/v1/me/verificaciones/"+abierta.Verificacion.ProviderRef+"/refrescar",
		intrusa.Token, nil, nil)
	if code != http.StatusForbidden {
		t.Fatalf("código = %d, esperaba 403", code)
	}
}

func TestLasVerificacionesPropiasSonPrivadas(t *testing.T) {
	srv := newTestServer(t)
	ana := registrar(t, srv, "Ana", "ana@example.com")

	var mias map[string]any
	if code := do(t, srv, http.MethodGet, "/api/v1/me/verificaciones", ana.Token, nil, &mias); code != http.StatusOK {
		t.Fatalf("código = %d, esperaba 200", code)
	}
	if len(mias["verificaciones"].([]any)) != 4 {
		t.Fatalf("verificaciones = %v, esperaba 4", mias["verificaciones"])
	}
	// Sin token no se llega.
	if code := do(t, srv, http.MethodGet, "/api/v1/me/verificaciones", "", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("sin token: código = %d, esperaba 401", code)
	}
}

func TestUnTipoDeComprobacionDesconocidoSeRechaza(t *testing.T) {
	srv := newTestServer(t)
	ana := registrarSinVerificar(t, srv, "Ana", "ana@example.com")

	code := do(t, srv, http.MethodPost, "/api/v1/me/verificaciones", ana.Token,
		map[string]string{"kind": "huella_dactilar"}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("código = %d, esperaba 422", code)
	}
}

// --- Avisos del proveedor de identidad ---

// entornoPersona monta el servidor con el proveedor real conectado contra un
// Persona simulado.
func entornoPersona(t *testing.T, secreto string) (*entorno, *trust.Persona) {
	t.Helper()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "generate-one-time-link") {
			_, _ = w.Write([]byte(`{"meta":{"one-time-link":"https://x.withpersona.com/verify?i=1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"inq_test","attributes":{"status":"created"}}}`))
	}))
	t.Cleanup(fake.Close)

	persona, err := trust.NewPersona(trust.PersonaConfig{
		APIKey: "k", WebhookSecret: secreto, BaseURL: fake.URL,
		Plantillas: map[trust.CheckKind]string{
			trust.CheckGovernmentID: "itmpl_doc_y_cara",
			trust.CheckSelfie:       "itmpl_doc_y_cara",
		},
	})
	if err != nil {
		t.Fatalf("NewPersona: %v", err)
	}

	secretoJWT, _ := auth.GenerateSecret()
	tokens, _ := auth.NewTokenIssuer(secretoJWT, time.Hour)
	svc := service.New(store.NewMemory(), service.Config{Tokens: tokens, Identidad: persona})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(api.NewServer(svc, tokens, log, api.WithPersona(persona)))
	t.Cleanup(srv.Close)
	return &entorno{Server: srv}, persona
}

func firmaPersona(cuerpo []byte, cuando time.Time, secreto string) string {
	marca := strconv.FormatInt(cuando.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secreto))
	mac.Write([]byte(marca + "."))
	mac.Write(cuerpo)
	return "t=" + marca + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func enviarAviso(t *testing.T, e *entorno, cuerpo []byte, firma string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.URL+"/api/v1/webhooks/identidad", bytes.NewReader(cuerpo))
	req.Header.Set("Content-Type", "application/json")
	if firma != "" {
		req.Header.Set("Persona-Signature", firma)
	}
	resp, err := e.Client().Do(req)
	if err != nil {
		t.Fatalf("petición: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestUnAvisoSinFirmaNoVerificaANadie(t *testing.T) {
	// La única barrera de esta ruta es la firma: es pública porque la llama el
	// proveedor, no un usuario con sesión.
	const secreto = "whsec_prueba_123456"
	e, _ := entornoPersona(t, secreto)
	ana := registrarSinVerificar(t, e, "Ana", "ana@example.com")

	var abierta struct {
		Verificacion struct {
			ProviderRef string `json:"provider_ref"`
		} `json:"verificacion"`
	}
	if code := do(t, e, http.MethodPost, "/api/v1/me/verificaciones", ana.Token,
		map[string]string{"kind": "government_id"}, &abierta); code != http.StatusCreated {
		t.Fatalf("abrir verificación: código = %d", code)
	}

	aviso := []byte(`{"data":{"attributes":{"name":"inquiry.approved","payload":{"data":{
		"id":"` + abierta.Verificacion.ProviderRef + `",
		"attributes":{"status":"approved"},
		"relationships":{"inquiry-template":{"data":{"id":"itmpl_doc_y_cara"}}}}}}}}`)

	casos := map[string]string{
		"sin firma":                "",
		"firma inventada":          "t=1,v1=aabbcc",
		"firmado con otro secreto": firmaPersona(aviso, time.Now(), "secreto-del-atacante"),
	}
	for nombre, firma := range casos {
		if code := enviarAviso(t, e, aviso, firma); code != http.StatusUnauthorized {
			t.Errorf("%s: código = %d, esperaba 401", nombre, code)
		}
	}

	// Y nadie ha quedado verificado por el camino.
	var perfil map[string]any
	do(t, e, http.MethodGet, "/api/v1/users/"+ana.User.ID+"/confianza", "", nil, &perfil)
	if perfil["nivel"] != "nuevo" {
		t.Fatalf("nivel = %v: un aviso sin firma no puede acreditar a nadie", perfil["nivel"])
	}
}

func TestUnAvisoFirmadoAcreditaDocumentoYCaraALaVez(t *testing.T) {
	const secreto = "whsec_prueba_123456"
	e, _ := entornoPersona(t, secreto)
	ana := registrarSinVerificar(t, e, "Ana", "ana@example.com")

	// Se abren las dos comprobaciones; una sola plantilla las resuelve.
	var doc struct {
		Verificacion struct {
			ProviderRef string `json:"provider_ref"`
		} `json:"verificacion"`
	}
	do(t, e, http.MethodPost, "/api/v1/me/verificaciones", ana.Token,
		map[string]string{"kind": "government_id"}, &doc)
	do(t, e, http.MethodPost, "/api/v1/me/verificaciones", ana.Token,
		map[string]string{"kind": "selfie_liveness"}, nil)

	aviso := []byte(`{"data":{"attributes":{"name":"inquiry.approved","payload":{"data":{
		"id":"` + doc.Verificacion.ProviderRef + `",
		"attributes":{"status":"approved","reference-id":"` + ana.User.ID + `"},
		"relationships":{"inquiry-template":{"data":{"id":"itmpl_doc_y_cara"}}}}}}}}`)

	if code := enviarAviso(t, e, aviso, firmaPersona(aviso, time.Now(), secreto)); code != http.StatusOK {
		t.Fatalf("código = %d, esperaba 200", code)
	}

	var mias map[string]any
	do(t, e, http.MethodGet, "/api/v1/me/verificaciones", ana.Token, nil, &mias)
	verificadas := 0
	for _, v := range mias["verificaciones"].([]any) {
		if v.(map[string]any)["status"] == "verified" {
			verificadas++
		}
	}
	if verificadas != 2 {
		t.Fatalf("verificadas = %d, esperaba que un trámite acreditara documento y cara", verificadas)
	}
}

func TestUnAvisoDeAlgoDesconocidoNoEsUnError(t *testing.T) {
	// Devolver error haría que el proveedor lo reintentara para siempre.
	const secreto = "whsec_prueba_123456"
	e, _ := entornoPersona(t, secreto)

	aviso := []byte(`{"data":{"attributes":{"name":"inquiry.approved","payload":{"data":{
		"id":"inq_que_no_conocemos","attributes":{"status":"approved"}}}}}}`)

	if code := enviarAviso(t, e, aviso, firmaPersona(aviso, time.Now(), secreto)); code != http.StatusOK {
		t.Fatalf("código = %d, esperaba 200 para que no se reintente eternamente", code)
	}
}

func TestUnRechazoNoSePisaConUnAvisoPosterior(t *testing.T) {
	const secreto = "whsec_prueba_123456"
	e, _ := entornoPersona(t, secreto)
	ana := registrarSinVerificar(t, e, "Ana", "ana@example.com")

	var doc struct {
		Verificacion struct {
			ProviderRef string `json:"provider_ref"`
		} `json:"verificacion"`
	}
	do(t, e, http.MethodPost, "/api/v1/me/verificaciones", ana.Token,
		map[string]string{"kind": "government_id"}, &doc)
	ref := doc.Verificacion.ProviderRef

	construir := func(estado string) []byte {
		return []byte(`{"data":{"attributes":{"name":"inquiry.` + estado + `","payload":{"data":{
			"id":"` + ref + `","attributes":{"status":"` + estado + `"},
			"relationships":{"inquiry-template":{"data":{"id":"itmpl_doc_y_cara"}}}}}}}}`)
	}

	rechazo := construir("declined")
	enviarAviso(t, e, rechazo, firmaPersona(rechazo, time.Now(), secreto))

	// Reenviar un "aprobado" después no puede convertir el no en un sí.
	aprobado := construir("approved")
	enviarAviso(t, e, aprobado, firmaPersona(aprobado, time.Now(), secreto))

	var perfil map[string]any
	do(t, e, http.MethodGet, "/api/v1/users/"+ana.User.ID+"/confianza", "", nil, &perfil)
	if perfil["nivel"] != "nuevo" {
		t.Fatalf("nivel = %v: un rechazo resuelto no se puede reabrir", perfil["nivel"])
	}
}

func TestSinProveedorRealElWebhookNoExiste(t *testing.T) {
	e := newTestServer(t) // modo manual
	if code := enviarAviso(t, e, []byte(`{}`), "t=1,v1=aa"); code != http.StatusNotFound {
		t.Fatalf("código = %d, esperaba 404", code)
	}
}
