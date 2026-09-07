package api

import (
	"net/http"
	"time"
)

type completarViajeRequest struct {
	// ImporteRealCents es lo que de verdad cobró la flota. Si se informa,
	// manda sobre nuestra estimación.
	ImporteRealCents int64 `json:"importe_real_cents"`
	// RefViaje es la referencia del viaje en la app de Tesla.
	RefViaje string `json:"ref_viaje,omitempty"`
}

// completarViaje cierra un trayecto y anota lo que cada cual debe. No cobra:
// el dinero se mueve en la liquidación del periodo.
func (s *Server) completarViaje(w http.ResponseWriter, r *http.Request) {
	var req completarViajeRequest
	if !decode(w, r, &req) {
		return
	}
	t, entries, err := s.svc.CompletarViaje(r.PathValue("id"), actor(r), req.ImporteRealCents)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"trayecto": tripView(t),
		"apuntes":  nonNil(entries),
		"nota":     "Anotado. El cobro se hace en la liquidación del periodo, no viaje a viaje.",
	})
}

// misApuntes muestra a una persona lo que debe y lo que le deben.
func (s *Server) misApuntes(w http.ResponseWriter, r *http.Request) {
	todos, err := s.svc.ApuntesDe(actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"apuntes": nonNil(todos)})
}

// periodoDe lee las fechas del periodo de la consulta, con el mes en curso por
// defecto.
func periodoDe(r *http.Request, now time.Time) (time.Time, time.Time, error) {
	desde := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	hasta := desde.AddDate(0, 1, 0)

	if v := r.URL.Query().Get("desde"); v != "" {
		d, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		desde = d
	}
	if v := r.URL.Query().Get("hasta"); v != "" {
		h, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		hasta = h
	}
	return desde, hasta, nil
}

// ahorroDeAgrupar enseña con números por qué no se cobra viaje a viaje.
func (s *Server) ahorroDeAgrupar(w http.ResponseWriter, r *http.Request) {
	desde, hasta, err := periodoDe(r, time.Now().UTC())
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "fechas no válidas: "+err.Error())
		return
	}
	a, err := s.svc.AhorroDeAgrupar(desde, hasta)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}
