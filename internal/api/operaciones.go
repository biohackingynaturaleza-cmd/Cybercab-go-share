package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
)

// WithPanelDeOperaciones abre la cola de revisión de denuncias.
//
// Sin ella, denunciar es escribir en un buzón que nadie abre, y eso es peor que
// no tener denuncias: le promete a quien lo pasó mal que alguien va a mirarlo.
//
// El token es un secreto compartido y no una sesión de usuario a propósito:
// operaciones no es un rol dentro de la app, es gente de dentro, y no queremos
// que exista una cuenta capaz de leer denuncias ajenas si alguien la roba.
func WithPanelDeOperaciones(token string) Option {
	return func(s *Server) {
		if token != "" {
			s.tokenOperaciones = token
		}
	}
}

// operaciones exige el token del panel antes de dejar pasar.
func (s *Server) operaciones(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.tokenOperaciones == "" {
			// Sin token configurado, la cola no existe. Devolver 404 y no 403
			// evita confirmar que estas rutas están ahí.
			writeProblem(w, http.StatusNotFound, "no encontrado")
			return
		}
		dado := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !igualEnTiempoConstante(dado, s.tokenOperaciones) {
			writeProblem(w, http.StatusUnauthorized, "no autorizado")
			return
		}
		h(w, r)
	}
}

// igualEnTiempoConstante compara sin filtrar por el tiempo cuántos caracteres
// acertó quien lo intenta. Se comparan los hashes para que la longitud del
// token tampoco se note.
func igualEnTiempoConstante(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

// colaDeDenuncias son las denuncias por revisar, lo urgente primero.
func (s *Server) colaDeDenuncias(w http.ResponseWriter, _ *http.Request) {
	ds, err := s.svc.DenunciasPendientes()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"denuncias": nonNil(ds)})
}

type resolverDenunciaRequest struct {
	Confirmada bool   `json:"confirmada"`
	Resolucion string `json:"resolucion"`
	// SuspensionDias sobrescribe la duración por defecto de la suspensión.
	SuspensionDias int `json:"suspension_dias"`
	// Permanente cierra la cuenta para siempre: violencia o suplantación.
	Permanente bool `json:"permanente"`
}

func (s *Server) resolverDenuncia(w http.ResponseWriter, r *http.Request) {
	var req resolverDenunciaRequest
	if !decode(w, r, &req) {
		return
	}
	d, err := s.svc.ResolverDenuncia(service.ResolucionInput{
		DenunciaID: r.PathValue("id"),
		Confirmada: req.Confirmada,
		Resolucion: req.Resolucion,
		Suspension: time.Duration(req.SuspensionDias) * 24 * time.Hour,
		Permanente: req.Permanente,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// levantarSuspension devuelve a alguien al servicio, para cuando nos hayamos
// equivocado. Una suspensión que no se puede deshacer convierte cada error en
// definitivo.
func (s *Server) levantarSuspension(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.LevantarSuspension(r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"estado": "levantada"})
}
