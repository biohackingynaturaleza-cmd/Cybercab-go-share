package api

import (
	"net/http"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
)

type declararIncidenciaRequest struct {
	AtribuidaA   string                `json:"atribuida_a"`
	Tipo         domain.TipoIncidencia `json:"tipo"`
	ImporteCents int64                 `json:"importe_cents"`
	Descripcion  string                `json:"descripcion"`
}

// declararIncidencia registra un cargo que la flota hizo después del viaje.
func (s *Server) declararIncidencia(w http.ResponseWriter, r *http.Request) {
	var req declararIncidenciaRequest
	if !decode(w, r, &req) {
		return
	}
	i, err := s.svc.DeclararIncidencia(service.DeclararIncidenciaInput{
		TripID:       r.PathValue("id"),
		DeclaranteID: actor(r),
		AtribuidaA:   req.AtribuidaA,
		Tipo:         req.Tipo,
		ImporteCents: req.ImporteCents,
		Descripcion:  req.Descripcion,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"incidencia": i,
		"nota":       "Declarada, no cobrada. No se le carga nada a nadie hasta que lo acepta.",
	})
}

type responderIncidenciaRequest struct {
	Acepta bool `json:"acepta"`
}

// responderIncidencia deja que quien la recibe la acepte o la discuta.
func (s *Server) responderIncidencia(w http.ResponseWriter, r *http.Request) {
	var req responderIncidenciaRequest
	if !decode(w, r, &req) {
		return
	}
	i, err := s.svc.ResponderIncidencia(r.PathValue("id"), actor(r), req.Acepta)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, i)
}

// retirarIncidencia deja que quien la declaró se eche atrás.
func (s *Server) retirarIncidencia(w http.ResponseWriter, r *http.Request) {
	i, err := s.svc.RetirarIncidencia(r.PathValue("id"), actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, i)
}

// misIncidencias lista las que a una persona le afectan.
func (s *Server) misIncidencias(w http.ResponseWriter, r *http.Request) {
	is, err := s.svc.MisIncidencias(actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidencias": nonNil(is)})
}
