package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/api"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/auth"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
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

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	tokens, err := auth.NewTokenIssuer(secret, time.Hour)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	svc := service.New(store.NewMemory(), service.Config{Tokens: tokens})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(api.NewServer(svc, tokens, log))
	t.Cleanup(srv.Close)
	return srv
}

// do lanza una petición autenticada con token (vacío = sin autenticar) y
// descodifica la respuesta JSON en out.
func do(t *testing.T, srv *httptest.Server, method, path, token string, body, out any) int {
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

type sesion struct {
	Token string `json:"token"`
	User  struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"user"`
}

func registrar(t *testing.T, srv *httptest.Server, name, email string) sesion {
	t.Helper()
	var s sesion
	code := do(t, srv, http.MethodPost, "/api/v1/auth/register", "",
		map[string]string{"name": name, "email": email, "password": password}, &s)
	if code != http.StatusCreated {
		t.Fatalf("registro de %s: código = %d", name, code)
	}
	if s.Token == "" {
		t.Fatalf("el registro de %s no devolvió token", name)
	}
	return s
}

// publicarTrayecto crea un viaje centro → aeropuerto y devuelve su id.
func publicarTrayecto(t *testing.T, srv *httptest.Server, token string) string {
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
		map[string]string{"name": "Otra Ana", "email": "ANA@example.com", "password": password}, nil)
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
		ana.Token, map[string]any{"accept": true}, &decided)
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
		intrusa.Token, map[string]any{"accept": true}, nil)
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
