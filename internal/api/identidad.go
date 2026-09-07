package api

import (
	"io"
	"net/http"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// tamanoMaximoAviso acota lo que se lee de un aviso entrante: sin límite,
// cualquiera puede agotar la memoria del servidor con una petición enorme.
const tamanoMaximoAviso = 1 << 20

// webhookPersona recibe los avisos del proveedor de identidad.
//
// Es una ruta pública porque la llama Persona, no un usuario: lo que la
// protege no es un token de sesión sino la firma del cuerpo. Un aviso sin
// firma válida no se mira siquiera.
func (s *Server) webhookPersona(w http.ResponseWriter, r *http.Request) {
	if s.persona == nil {
		writeProblem(w, http.StatusNotFound, "no hay proveedor de identidad configurado")
		return
	}

	cuerpo, err := io.ReadAll(io.LimitReader(r.Body, tamanoMaximoAviso))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "no se pudo leer el aviso")
		return
	}

	if err := s.persona.VerificarFirma(r.Header.Get("Persona-Signature"), cuerpo, time.Now()); err != nil {
		s.log.Warn("aviso de identidad con firma inválida", "err", err, "ip", r.RemoteAddr)
		// 401 y nada más: no se dice qué falló, para no ayudar a quien lo
		// esté intentando a ciegas.
		writeProblem(w, http.StatusUnauthorized, "firma no válida")
		return
	}

	aviso, err := s.persona.LeerAviso(cuerpo)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, err.Error())
		return
	}

	check, err := s.svc.AplicarAvisoDeIdentidad(aviso.Ref, &aviso.Outcome)
	if err != nil {
		// Un aviso de algo que no conocemos no es culpa del proveedor, y
		// devolverle un error haría que lo reintentara para siempre.
		s.log.Warn("aviso de identidad no aplicable", "ref", aviso.Ref, "err", err)
		writeJSON(w, http.StatusOK, map[string]string{"estado": "ignorado"})
		return
	}

	s.log.Info("verificación de identidad resuelta",
		"evento", aviso.Evento, "estado", string(check.Status))
	writeJSON(w, http.StatusOK, map[string]string{"estado": "aplicado"})
}

// WithPersona conecta el proveedor real para poder recibir sus avisos.
func WithPersona(p *trust.Persona) Option {
	return func(s *Server) { s.persona = p }
}
