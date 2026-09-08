package api

import (
	"net/http"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
)

// --- Contactos de confianza ---

type contactoRequest struct {
	Nombre        string `json:"nombre"`
	Email         string `json:"email"`
	AvisarAlSalir bool   `json:"avisar_al_salir"`
}

func (s *Server) añadirContacto(w http.ResponseWriter, r *http.Request) {
	var req contactoRequest
	if !decode(w, r, &req) {
		return
	}
	c, err := s.svc.AñadirContacto(service.ContactoInput{
		UserID: actor(r), Nombre: req.Nombre, Email: req.Email,
		AvisarAlSalir: req.AvisarAlSalir,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) misContactos(w http.ResponseWriter, r *http.Request) {
	cs, err := s.svc.MisContactos(actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"contactos": nonNil(cs),
		"maximo":    domain.MaxContactos,
	})
}

func (s *Server) borrarContacto(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.BorrarContacto(r.PathValue("id"), actor(r)); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"estado": "borrado"})
}

// --- Seguimiento del viaje ---

func (s *Server) compartirViaje(w http.ResponseWriter, r *http.Request) {
	enlace, err := s.svc.CompartirViaje(actor(r), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	// El testigo va dentro de la URL y solo se devuelve aquí, una vez: de la
	// base de datos solo se puede sacar su hash.
	writeJSON(w, http.StatusCreated, enlace)
}

// dejarDeCompartir cierra todos los enlaces de ese viaje: dejar de compartir
// tiene que significar eso, no cerrar uno y dejar otro abierto.
func (s *Server) dejarDeCompartir(w http.ResponseWriter, r *http.Request) {
	n, err := s.svc.DejarDeCompartir(actor(r), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"estado": "revocado", "enlaces": n})
}

type posicionRequest struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// apuntarPosicion guarda dónde está quien comparte el viaje.
func (s *Server) apuntarPosicion(w http.ResponseWriter, r *http.Request) {
	var req posicionRequest
	if !decode(w, r, &req) {
		return
	}
	punto := geo.Point{Lat: req.Lat, Lng: req.Lng}
	if !punto.Valid() {
		writeProblem(w, http.StatusUnprocessableEntity, "esas coordenadas no existen")
		return
	}
	if err := s.svc.ApuntarPosicion(actor(r), r.PathValue("id"), punto); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"estado": "apuntada"})
}

// verSeguimiento es la ruta pública que abre quien recibe el enlace.
//
// No lleva sesión a propósito: el contacto de confianza de alguien no tiene por
// qué registrarse en esta app para saber que su hija llegó bien.
func (s *Server) verSeguimiento(w http.ResponseWriter, r *http.Request) {
	vista, err := s.svc.VerSeguimiento(r.URL.Query().Get("t"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, vista)
}

// misViajesActivos son los trayectos en los que esa persona va dentro.
//
// Es lo que sostiene la parte de seguridad de la interfaz: sin saber en qué
// viaje estás, no se puede enseñar el botón de emergencia.
func (s *Server) misViajesActivos(w http.ResponseWriter, r *http.Request) {
	vs, err := s.svc.ViajesActivos(actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"viajes": nonNil(vs)})
}

// --- Botón de emergencia ---

type emergenciaRequest struct {
	// Lat y Lng son opcionales: sin permiso de ubicación el botón sigue
	// funcionando, porque lo que no puede pasar es que falle justo cuando hace
	// falta.
	Lat  *float64 `json:"lat"`
	Lng  *float64 `json:"lng"`
	Nota string   `json:"nota"`
}

func (s *Server) emergencia(w http.ResponseWriter, r *http.Request) {
	var req emergenciaRequest
	if !decode(w, r, &req) {
		return
	}
	in := service.AlertaInput{UserID: actor(r), TripID: r.PathValue("id"), Nota: req.Nota}
	if req.Lat != nil && req.Lng != nil {
		punto := geo.Point{Lat: *req.Lat, Lng: *req.Lng}
		if punto.Valid() {
			in.Posicion = &punto
		}
	}

	a, err := s.svc.Emergencia(in)
	if err != nil {
		writeError(w, err)
		return
	}
	// El teléfono vuelve en la respuesta para que la interfaz lo enseñe sin
	// tenerlo escrito por su cuenta: el número al que llamar es un dato del
	// servicio, no una constante de la pantalla.
	writeJSON(w, http.StatusCreated, map[string]any{
		"alerta":               a,
		"telefono_emergencias": domain.TelefonoEmergencias,
	})
}

func (s *Server) retirarAlerta(w http.ResponseWriter, r *http.Request) {
	a, err := s.svc.RetirarAlerta(r.PathValue("id"), actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// --- Operaciones ---

func (s *Server) colaDeAlertas(w http.ResponseWriter, _ *http.Request) {
	as, err := s.svc.AlertasPendientes()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"alertas": nonNil(as)})
}

type atenderAlertaRequest struct {
	Resolucion string `json:"resolucion"`
}

func (s *Server) atenderAlerta(w http.ResponseWriter, r *http.Request) {
	var req atenderAlertaRequest
	if !decode(w, r, &req) {
		return
	}
	a, err := s.svc.AtenderAlerta(r.PathValue("id"), req.Resolucion)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}
