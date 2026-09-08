package api

import (
	"net/http"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
)

type denunciarRequest struct {
	Motivo      domain.MotivoDenuncia `json:"motivo"`
	Descripcion string                `json:"descripcion"`
	TripID      string                `json:"trip_id"`
}

// denunciar registra lo que alguien cuenta sobre otra persona.
func (s *Server) denunciar(w http.ResponseWriter, r *http.Request) {
	var req denunciarRequest
	if !decode(w, r, &req) {
		return
	}
	d, err := s.svc.Denunciar(service.DenunciarInput{
		DenuncianteID: actor(r),
		DenunciadoID:  r.PathValue("id"),
		Motivo:        req.Motivo,
		Descripcion:   req.Descripcion,
		TripID:        req.TripID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// misDenuncias devuelve las que ha puesto quien pregunta.
//
// Solo las suyas. No hay ninguna ruta que enseñe las denuncias que hay contra
// una persona: una denuncia que llega a oídos del denunciado es una denuncia
// que nadie pone.
func (s *Server) misDenuncias(w http.ResponseWriter, r *http.Request) {
	ds, err := s.svc.MisDenuncias(actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"denuncias": nonNil(ds)})
}
