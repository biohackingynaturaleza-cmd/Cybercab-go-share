package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
)

// SourceOSRM identifica las rutas calculadas por OSRM.
const SourceOSRM = "osrm"

// PublicOSRM es el servidor de demostración del proyecto OSRM. Sirve para
// probar, pero no tiene garantías de servicio: en producción hay que levantar
// un OSRM propio o contratar un proveedor.
const PublicOSRM = "https://router.project-osrm.org"

// OSRM calcula rutas por carretera contra un servidor OSRM.
type OSRM struct {
	baseURL string
	client  *http.Client
	profile string
}

// OSRMOption configura el cliente.
type OSRMOption func(*OSRM)

// WithHTTPClient sustituye el cliente HTTP (para pruebas o para ajustar el
// transporte).
func WithHTTPClient(c *http.Client) OSRMOption {
	return func(o *OSRM) { o.client = c }
}

// WithProfile elige el perfil de OSRM ("driving" por defecto).
func WithProfile(p string) OSRMOption {
	return func(o *OSRM) { o.profile = p }
}

// NewOSRM construye el cliente contra el servidor indicado.
func NewOSRM(baseURL string, opts ...OSRMOption) *OSRM {
	o := &OSRM{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
		profile: "driving",
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

var _ Router = (*OSRM)(nil)

// osrmResponse es la parte de la respuesta de OSRM que nos interesa.
type osrmResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Routes  []struct {
		Distance float64 `json:"distance"` // metros
		Duration float64 `json:"duration"` // segundos
		Geometry struct {
			// GeoJSON usa el orden [longitud, latitud], al revés de lo habitual.
			Coordinates [][2]float64 `json:"coordinates"`
		} `json:"geometry"`
	} `json:"routes"`
}

// Route pide a OSRM la ruta por carretera que pasa por todos los puntos.
func (o *OSRM) Route(ctx context.Context, points []geo.Point) (*Route, error) {
	if len(points) < 2 {
		return nil, ErrRutaInsuficiente
	}

	// OSRM espera las coordenadas como "lng,lat;lng,lat", en ese orden.
	coords := make([]string, 0, len(points))
	for _, p := range points {
		if !p.Valid() {
			return nil, fmt.Errorf("coordenada no válida: %+v", p)
		}
		coords = append(coords,
			strconv.FormatFloat(p.Lng, 'f', 6, 64)+","+strconv.FormatFloat(p.Lat, 'f', 6, 64))
	}

	url := fmt.Sprintf("%s/route/v1/%s/%s?overview=full&geometries=geojson",
		o.baseURL, o.profile, strings.Join(coords, ";"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("consultando OSRM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OSRM respondió %d", resp.StatusCode)
	}

	var body osrmResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("respuesta de OSRM ilegible: %w", err)
	}
	if body.Code != "Ok" {
		if body.Code == "NoRoute" {
			return nil, ErrSinRuta
		}
		return nil, fmt.Errorf("OSRM: %s (%s)", body.Code, body.Message)
	}
	if len(body.Routes) == 0 || len(body.Routes[0].Geometry.Coordinates) < 2 {
		return nil, ErrSinRuta
	}

	r := body.Routes[0]
	line := make(geo.Route, 0, len(r.Geometry.Coordinates))
	for _, c := range r.Geometry.Coordinates {
		line = append(line, geo.Point{Lat: c[1], Lng: c[0]})
	}

	return &Route{
		Geometry:    line,
		DistanceKm:  r.Distance / 1000,
		DurationMin: r.Duration / 60,
		Source:      SourceOSRM,
	}, nil
}

// WithFallback devuelve un Router que intenta primary y, si falla por causas de
// red o de servicio, recurre a backup.
//
// Un fallo de OSRM no debe impedir publicar un trayecto: es preferible una ruta
// aproximada, claramente marcada como tal, que un error en la cara del usuario.
func WithFallback(primary, backup Router, log Logger) Router {
	return &fallbackRouter{primary: primary, backup: backup, log: log}
}

// Logger es el mínimo que necesita el router para avisar de un respaldo.
type Logger interface {
	Warn(msg string, args ...any)
}

type fallbackRouter struct {
	primary Router
	backup  Router
	log     Logger
}

func (f *fallbackRouter) Route(ctx context.Context, points []geo.Point) (*Route, error) {
	r, err := f.primary.Route(ctx, points)
	if err == nil {
		return r, nil
	}
	// Si no hay ruta posible, el respaldo tampoco va a inventarla bien: es un
	// error del usuario (un punto inalcanzable), no una avería del proveedor.
	if err == ErrSinRuta || err == ErrRutaInsuficiente {
		return nil, err
	}
	if f.log != nil {
		f.log.Warn("el motor de rutas falló, usando línea recta", "err", err)
	}
	return f.backup.Route(ctx, points)
}
