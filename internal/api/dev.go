package api

import (
	"net/http"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// Option configura el servidor.
type Option func(*Server)

// WithDevIdentityResolver abre un endpoint que resuelve verificaciones a mano.
//
// Existe porque el proveedor manual deja todo pendiente para siempre, y sin
// esto la aplicación no se puede probar de punta a punta en desarrollo. Solo
// debe activarse con el proveedor manual y nunca en producción: quien construye
// el servidor es responsable de no pasarlo, y main.go no lo hace si ENV es
// production.
func WithDevIdentityResolver(m *trust.Manual) Option {
	return func(s *Server) { s.devIdentidad = m }
}

type resolverRequest struct {
	// Verificar decide el veredicto: cierto la aprueba, falso la rechaza.
	Verificar bool   `json:"verificar"`
	Motivo    string `json:"motivo,omitempty"`
}

func (s *Server) devResolverVerificacion(w http.ResponseWriter, r *http.Request) {
	var req resolverRequest
	if !decode(w, r, &req) {
		return
	}
	outcome := trust.Outcome{Status: trust.StatusRejected, Reason: req.Motivo}
	if req.Verificar {
		outcome = trust.Outcome{
			Status:            trust.StatusVerified,
			DocumentExpiresAt: time.Now().UTC().AddDate(5, 0, 0),
		}
	}
	if err := s.devIdentidad.Resolve(r.PathValue("ref"), outcome); err != nil {
		writeProblem(w, http.StatusNotFound, err.Error())
		return
	}
	check, err := s.svc.RefrescarVerificacion(r.Context(), r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"verificacion": check,
		"aviso":        "Endpoint de desarrollo: no verifica nada de verdad.",
	})
}
