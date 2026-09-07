package routing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
)

var (
	downtown = geo.Point{Lat: 30.2685, Lng: -97.7425}
	airport  = geo.Point{Lat: 30.1975, Lng: -97.6664}
)

// respuestaOSRM es una respuesta con la forma exacta que devuelve OSRM para
// una ruta de Austin centro al aeropuerto (recortada a unos pocos puntos).
const respuestaOSRM = `{
  "code": "Ok",
  "routes": [{
    "distance": 13284.6,
    "duration": 1043.2,
    "geometry": {
      "type": "LineString",
      "coordinates": [
        [-97.7425, 30.2685],
        [-97.7401, 30.2652],
        [-97.7208, 30.2411],
        [-97.6961, 30.2195],
        [-97.6664, 30.1975]
      ]
    }
  }],
  "waypoints": [{"name": "Congress Avenue"}, {"name": "Presidential Blvd"}]
}`

// servidorOSRM levanta un OSRM de mentira que responde lo que se le indique.
func servidorOSRM(t *testing.T, status int, body string, capture *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			*capture = r.URL.String()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- Línea recta ---

func TestStraightLineUneLosPuntos(t *testing.T) {
	r, err := NewStraightLine(45).Route(context.Background(), []geo.Point{downtown, airport})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if r.Source != SourceStraightLine {
		t.Errorf("Source = %q, esperaba %q", r.Source, SourceStraightLine)
	}
	if len(r.Geometry) != 2 {
		t.Errorf("puntos = %d, esperaba 2", len(r.Geometry))
	}
	if r.DistanceKm < 10 || r.DistanceKm > 12 {
		t.Errorf("distancia = %.2f km, esperaba ~11 km", r.DistanceKm)
	}
	// 11 km a 45 km/h ≈ 14,7 min.
	if r.DurationMin < 13 || r.DurationMin > 17 {
		t.Errorf("duración = %.1f min, esperaba ~15 min", r.DurationMin)
	}
}

func TestStraightLineNecesitaDosPuntos(t *testing.T) {
	_, err := NewStraightLine(45).Route(context.Background(), []geo.Point{downtown})
	if !errors.Is(err, ErrRutaInsuficiente) {
		t.Fatalf("error = %v, esperaba ErrRutaInsuficiente", err)
	}
}

// --- OSRM ---

func TestOSRMDevuelveLaRutaDelProveedor(t *testing.T) {
	srv := servidorOSRM(t, http.StatusOK, respuestaOSRM, nil)

	r, err := NewOSRM(srv.URL).Route(context.Background(), []geo.Point{downtown, airport})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if r.Source != SourceOSRM {
		t.Errorf("Source = %q, esperaba %q", r.Source, SourceOSRM)
	}
	// OSRM da metros y segundos; nosotros trabajamos en km y minutos.
	if r.DistanceKm < 13.28 || r.DistanceKm > 13.29 {
		t.Errorf("distancia = %.4f km, esperaba 13.2846", r.DistanceKm)
	}
	if r.DurationMin < 17.38 || r.DurationMin > 17.39 {
		t.Errorf("duración = %.4f min, esperaba 17.3867", r.DurationMin)
	}
	if len(r.Geometry) != 5 {
		t.Fatalf("puntos = %d, esperaba 5", len(r.Geometry))
	}
	// GeoJSON viene como [lng, lat]: invertirlo dejaría la ruta en Somalia.
	if r.Geometry[0].Lat != 30.2685 || r.Geometry[0].Lng != -97.7425 {
		t.Errorf("primer punto = %+v, esperaba lat 30.2685 / lng -97.7425", r.Geometry[0])
	}
	if r.Geometry[4].Lat != 30.1975 {
		t.Errorf("último punto = %+v, esperaba terminar en el aeropuerto", r.Geometry[4])
	}
}

func TestOSRMPideLasCoordenadasEnElOrdenCorrecto(t *testing.T) {
	var url string
	srv := servidorOSRM(t, http.StatusOK, respuestaOSRM, &url)

	if _, err := NewOSRM(srv.URL).Route(context.Background(), []geo.Point{downtown, airport}); err != nil {
		t.Fatalf("Route: %v", err)
	}
	// OSRM espera lng,lat separados por punto y coma.
	if !strings.Contains(url, "-97.742500,30.268500;-97.666400,30.197500") {
		t.Fatalf("URL = %q, no lleva las coordenadas en orden lng,lat", url)
	}
	if !strings.Contains(url, "geometries=geojson") || !strings.Contains(url, "overview=full") {
		t.Errorf("URL = %q, faltan los parámetros de geometría", url)
	}
}

func TestOSRMPasaLosWaypointsIntermedios(t *testing.T) {
	var url string
	srv := servidorOSRM(t, http.StatusOK, respuestaOSRM, &url)
	medio := geo.Point{Lat: 30.2380, Lng: -97.7180}

	if _, err := NewOSRM(srv.URL).Route(context.Background(), []geo.Point{downtown, medio, airport}); err != nil {
		t.Fatalf("Route: %v", err)
	}
	if strings.Count(url, ";") != 2 {
		t.Fatalf("URL = %q, esperaba tres coordenadas", url)
	}
}

func TestOSRMTraduceNoRoute(t *testing.T) {
	srv := servidorOSRM(t, http.StatusOK, `{"code":"NoRoute","message":"sin camino"}`, nil)

	_, err := NewOSRM(srv.URL).Route(context.Background(), []geo.Point{downtown, airport})
	if !errors.Is(err, ErrSinRuta) {
		t.Fatalf("error = %v, esperaba ErrSinRuta", err)
	}
}

func TestOSRMFallaConErrorDelServidor(t *testing.T) {
	srv := servidorOSRM(t, http.StatusInternalServerError, `{}`, nil)

	if _, err := NewOSRM(srv.URL).Route(context.Background(), []geo.Point{downtown, airport}); err == nil {
		t.Fatal("esperaba un error con un 500 del proveedor")
	}
}

func TestOSRMFallaConRespuestaIlegible(t *testing.T) {
	srv := servidorOSRM(t, http.StatusOK, `no soy json`, nil)

	if _, err := NewOSRM(srv.URL).Route(context.Background(), []geo.Point{downtown, airport}); err == nil {
		t.Fatal("esperaba un error con una respuesta ilegible")
	}
}

func TestOSRMRespetaLaCancelacionDelContexto(t *testing.T) {
	srv := servidorOSRM(t, http.StatusOK, respuestaOSRM, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := NewOSRM(srv.URL).Route(ctx, []geo.Point{downtown, airport}); err == nil {
		t.Fatal("esperaba un error con el contexto ya cancelado")
	}
}

// --- Respaldo ---

type routerRoto struct{ err error }

func (r routerRoto) Route(context.Context, []geo.Point) (*Route, error) { return nil, r.err }

func TestFallbackUsaLaLineaRectaSiElProveedorFalla(t *testing.T) {
	r := WithFallback(routerRoto{err: errors.New("proveedor caído")}, NewStraightLine(45), nil)

	got, err := r.Route(context.Background(), []geo.Point{downtown, airport})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if got.Source != SourceStraightLine {
		t.Fatalf("Source = %q, esperaba el respaldo", got.Source)
	}
}

func TestFallbackNoTapaUnDestinoInalcanzable(t *testing.T) {
	// Si de verdad no hay ruta, dar una línea recta sería mentir al usuario.
	r := WithFallback(routerRoto{err: ErrSinRuta}, NewStraightLine(45), nil)

	if _, err := r.Route(context.Background(), []geo.Point{downtown, airport}); !errors.Is(err, ErrSinRuta) {
		t.Fatalf("error = %v, esperaba ErrSinRuta", err)
	}
}

func TestFallbackPrefiereElProveedorCuandoFunciona(t *testing.T) {
	srv := servidorOSRM(t, http.StatusOK, respuestaOSRM, nil)
	r := WithFallback(NewOSRM(srv.URL), NewStraightLine(45), nil)

	got, err := r.Route(context.Background(), []geo.Point{downtown, airport})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if got.Source != SourceOSRM {
		t.Fatalf("Source = %q, esperaba la ruta real", got.Source)
	}
}
