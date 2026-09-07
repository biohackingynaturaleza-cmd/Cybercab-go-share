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
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	svc := service.New(store.NewMemory(), service.Config{})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(api.NewServer(svc, log))
	t.Cleanup(srv.Close)
	return srv
}

// do lanza una petición y descodifica la respuesta JSON en out.
func do(t *testing.T, srv *httptest.Server, method, path string, body, out any) int {
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

func TestHealthz(t *testing.T) {
	srv := newTestServer(t)
	var body map[string]string
	if code := do(t, srv, http.MethodGet, "/healthz", nil, &body); code != http.StatusOK {
		t.Fatalf("código = %d, esperaba 200", code)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

// TestRecorridoCompletoCentroAeropuerto reproduce el caso de uso central:
// Ana publica un trayecto al aeropuerto, Bruno lo encuentra, pide plaza,
// Ana la acepta y el coste queda repartido entre los dos.
func TestRecorridoCompletoCentroAeropuerto(t *testing.T) {
	srv := newTestServer(t)

	var ana, bruno map[string]any
	if code := do(t, srv, http.MethodPost, "/api/v1/users",
		map[string]string{"name": "Ana", "email": "ana@example.com"}, &ana); code != http.StatusCreated {
		t.Fatalf("alta de Ana: código = %d", code)
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/users",
		map[string]string{"name": "Bruno", "email": "bruno@example.com"}, &bruno); code != http.StatusCreated {
		t.Fatalf("alta de Bruno: código = %d", code)
	}

	departure := time.Now().UTC().Add(3 * time.Hour)
	var trip map[string]any
	code := do(t, srv, http.MethodPost, "/api/v1/trips", map[string]any{
		"host_id":        ana["id"],
		"origin":         map[string]any{"name": "Centro", "point": map[string]float64{"lat": 30.2685, "lng": -97.7425}},
		"destination":    map[string]any{"name": "AUS", "point": map[string]float64{"lat": 30.1975, "lng": -97.6664}},
		"departure_time": departure.Format(time.RFC3339),
		"vehicle":        "model_y",
		"max_detour_km":  2,
	}, &trip)
	if code != http.StatusCreated {
		t.Fatalf("alta del trayecto: código = %d (%v)", code, trip)
	}
	if trip["seats_available"].(float64) != 3 {
		t.Fatalf("plazas libres = %v, esperaba 3", trip["seats_available"])
	}

	var search map[string]any
	code = do(t, srv, http.MethodPost, "/api/v1/search", map[string]any{
		"pickup":  map[string]float64{"lat": 30.2380, "lng": -97.7180},
		"dropoff": map[string]float64{"lat": 30.1975, "lng": -97.6664},
	}, &search)
	if code != http.StatusOK {
		t.Fatalf("búsqueda: código = %d", code)
	}
	if search["count"].(float64) != 1 {
		t.Fatalf("resultados = %v, esperaba 1", search["count"])
	}

	tripID := trip["id"].(string)
	var booking map[string]any
	code = do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/bookings", map[string]any{
		"passenger_id": bruno["id"],
		"pickup":       map[string]any{"name": "Riverside", "point": map[string]float64{"lat": 30.2380, "lng": -97.7180}},
		"dropoff":      map[string]any{"name": "AUS", "point": map[string]float64{"lat": 30.1975, "lng": -97.6664}},
	}, &booking)
	if code != http.StatusCreated {
		t.Fatalf("reserva: código = %d (%v)", code, booking)
	}
	if booking["status"] != "pending" {
		t.Fatalf("estado de la reserva = %v, esperaba pending", booking["status"])
	}

	var decided map[string]any
	code = do(t, srv, http.MethodPost, "/api/v1/bookings/"+booking["id"].(string)+"/decision",
		map[string]any{"actor_id": ana["id"], "accept": true}, &decided)
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
	if code := do(t, srv, http.MethodGet, "/api/v1/trips/"+tripID+"/fare", nil, &fare); code != http.StatusOK {
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

func TestTrayectoInexistenteDevuelve404(t *testing.T) {
	srv := newTestServer(t)
	if code := do(t, srv, http.MethodGet, "/api/v1/trips/trip_no_existe", nil, nil); code != http.StatusNotFound {
		t.Fatalf("código = %d, esperaba 404", code)
	}
}

func TestAltaDeUsuarioSinDatosDevuelve422(t *testing.T) {
	srv := newTestServer(t)
	code := do(t, srv, http.MethodPost, "/api/v1/users", map[string]string{"name": ""}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("código = %d, esperaba 422", code)
	}
}

func TestJSONInvalidoDevuelve400(t *testing.T) {
	srv := newTestServer(t)
	resp, err := srv.Client().Post(srv.URL+"/api/v1/users", "application/json", bytes.NewBufferString("{no soy json"))
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
	code := do(t, srv, http.MethodPost, "/api/v1/search", map[string]any{
		"pickup":  map[string]float64{"lat": 999, "lng": -97.7425},
		"dropoff": map[string]float64{"lat": 30.1975, "lng": -97.6664},
	}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("código = %d, esperaba 400", code)
	}
}

func TestMetodoNoPermitido(t *testing.T) {
	srv := newTestServer(t)
	if code := do(t, srv, http.MethodDelete, "/api/v1/trips", nil, nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("código = %d, esperaba 405", code)
	}
}
