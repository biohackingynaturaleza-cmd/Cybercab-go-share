package api

import (
	"net/http"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// --- Verificación de identidad ---

type iniciarVerificacionRequest struct {
	Kind trust.CheckKind `json:"kind"`
}

// iniciarVerificacion abre una comprobación y devuelve adónde ir a completarla.
func (s *Server) iniciarVerificacion(w http.ResponseWriter, r *http.Request) {
	var req iniciarVerificacionRequest
	if !decode(w, r, &req) {
		return
	}
	sess, check, err := s.svc.IniciarVerificacion(r.Context(), actor(r), req.Kind)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"verificacion": check,
		"continuar_en": sess.RedirectURL,
		"caduca":       sess.ExpiresAt,
	})
}

// refrescarVerificacion consulta el veredicto del proveedor y lo aplica.
func (s *Server) refrescarVerificacion(w http.ResponseWriter, r *http.Request) {
	check, err := s.svc.RefrescarVerificacion(r.Context(), r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	// Solo puede refrescar sus propias comprobaciones.
	if check.UserID != actor(r) {
		writeProblem(w, http.StatusForbidden, "esa verificación no es tuya")
		return
	}
	writeJSON(w, http.StatusOK, check)
}

// misVerificaciones lista las comprobaciones propias, con su estado.
func (s *Server) misVerificaciones(w http.ResponseWriter, r *http.Request) {
	checks, err := s.svc.VerificacionesDe(actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"verificaciones": nonNil(checks)})
}

// --- Perfil de confianza ---

// perfilDeConfianza es lo que se ve de otra persona antes de compartir coche.
// No incluye email ni datos de contacto: solo lo que hace falta para decidir si
// te subes al coche con ella.
func (s *Server) perfilDeConfianza(w http.ResponseWriter, r *http.Request) {
	p, err := s.svc.PerfilDe(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// --- Bloqueos ---

func (s *Server) bloquear(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Bloquear(actor(r), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bloqueado": r.PathValue("id")})
}

func (s *Server) desbloquear(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Desbloquear(actor(r), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"desbloqueado": r.PathValue("id")})
}
