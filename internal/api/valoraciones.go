package api

import (
	"net/http"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
)

type valorarRequest struct {
	Estrellas  int    `json:"estrellas"`
	Comentario string `json:"comentario"`
}

// valorar deja la opinión de una persona sobre la otra tras compartir viaje.
func (s *Server) valorar(w http.ResponseWriter, r *http.Request) {
	var req valorarRequest
	if !decode(w, r, &req) {
		return
	}
	v, err := s.svc.Valorar(service.ValorarInput{
		BookingID:  r.PathValue("id"),
		AutorID:    actor(r),
		Estrellas:  req.Estrellas,
		Comentario: req.Comentario,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	// La valoración vuelve tal cual se guardó, sin decir si ya es visible: eso
	// depende de lo que haga la otra parte y no es asunto de quien la escribe.
	writeJSON(w, http.StatusCreated, v)
}

// valoracionesDe devuelve las valoraciones ya publicadas de una persona.
//
// Es pública a propósito: la reputación solo sirve para decidir si te subes a
// un coche con alguien, y esa decisión se toma antes de reservar.
func (s *Server) valoracionesDe(w http.ResponseWriter, r *http.Request) {
	vs, err := s.svc.ValoracionesSobre(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valoraciones": nonNil(vs)})
}

// misValoracionesPendientes son los viajes hechos que aún esperan opinión.
func (s *Server) misValoracionesPendientes(w http.ResponseWriter, r *http.Request) {
	pendientes, err := s.svc.PendientesDeValorar(actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pendientes": nonNil(pendientes)})
}
