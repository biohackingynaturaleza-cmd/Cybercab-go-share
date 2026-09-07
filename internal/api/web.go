package api

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/simulacion"
)

//go:embed all:web
var webFiles embed.FS

// montarWeb sirve la interfaz desde el propio binario.
//
// Va embebida a propósito: un solo artefacto que desplegar, sin un servidor de
// estáticos aparte ni un despliegue de frontend que pueda quedar
// desincronizado con la API que consume.
func montarWeb(mux *http.ServeMux) error {
	sub, err := fs.Sub(webFiles, "web")
	if err != nil {
		return err
	}
	archivos := http.FileServer(http.FS(sub))

	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// La interfaz es una sola página: cualquier ruta que no sea un fichero
		// existente la sirve, para que recargar en una vista interna funcione.
		if _, err := fs.Stat(sub, sanear(r.URL.Path)); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		cabecerasDeSeguridad(w)
		archivos.ServeHTTP(w, r)
	}))
	return nil
}

// sanear convierte la ruta de la petición en un nombre que fs.Stat entienda.
func sanear(p string) string {
	if p == "" || p == "/" {
		return "index.html"
	}
	return p[1:]
}

// cabecerasDeSeguridad pone las protecciones que no cuestan nada y evitan las
// clases de ataque más comunes contra una página que maneja sesiones.
func cabecerasDeSeguridad(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
	// Los mapas necesitan cargar teselas e imágenes de OpenStreetMap; todo lo
	// demás sale de este mismo servidor.
	h.Set("Content-Security-Policy",
		"default-src 'self'; "+
			"img-src 'self' data: https://*.tile.openstreetmap.org; "+
			"style-src 'self' 'unsafe-inline'; "+
			"script-src 'self'; "+
			"connect-src 'self'")
}

// zonas devuelve los puntos de referencia del área de servicio.
//
// Salen del mismo sitio que usa la simulación: si la interfaz tuviera su propia
// lista, las dos acabarían diciendo cosas distintas sobre dónde opera el
// servicio.
func (s *Server) zonas(w http.ResponseWriter, _ *http.Request) {
	type zona struct {
		Nombre string  `json:"nombre"`
		Lat    float64 `json:"lat"`
		Lng    float64 `json:"lng"`
	}
	out := make([]zona, 0, len(simulacion.ZonasAustin))
	for _, z := range simulacion.ZonasAustin {
		out = append(out, zona{Nombre: z.Nombre, Lat: z.Punto.Lat, Lng: z.Punto.Lng})
	}
	writeJSON(w, http.StatusOK, map[string]any{"zonas": out})
}
