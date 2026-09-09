package api

import (
	"net/http"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pagos"
)

// miSaldo enseña a una persona lo que debe y lo que le deben.
func (s *Server) miSaldo(w http.ResponseWriter, r *http.Request) {
	saldo, err := s.svc.SaldoDe(actor(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saldo)
}

// --- Operaciones ---

func (s *Server) listarLiquidaciones(w http.ResponseWriter, _ *http.Request) {
	ls, err := s.svc.Liquidaciones()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"liquidaciones": nonNil(ls),
		// Qué procesador hay detrás es lo primero que quiere saber quien mire
		// esto: con el que solo anota, nada de lo que aparezca aquí se ha
		// cobrado.
		"procesador": s.svc.NombreDelProcesador(),
	})
}

// cerrarPeriodos lanza a mano el cierre de los periodos vencidos, sin esperar
// al programador.
func (s *Server) cerrarPeriodos(w http.ResponseWriter, _ *http.Request) {
	hechas, err := s.svc.LiquidarPendientes(time.Now().UTC())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"liquidaciones": nonNil(hechas)})
}

// ejecutarLiquidacion manda sus instrucciones al procesador de pagos.
func (s *Server) ejecutarLiquidacion(w http.ResponseWriter, r *http.Request) {
	liq, err := s.svc.EjecutarLiquidacion(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	cuerpo := map[string]any{"liquidacion": liq, "procesador": s.svc.NombreDelProcesador()}
	if liq.Estado != "ejecutada" {
		// Sin procesador de verdad esto no ha movido un céntimo, y la
		// respuesta tiene que decirlo: dar por hecho un cobro que no existe
		// solo se descubre cuando alguien reclama.
		cuerpo["aviso"] = "las instrucciones quedan " + string(pagos.EstadoAnotado) +
			" mientras no haya un procesador de pagos configurado"
	}
	writeJSON(w, http.StatusOK, cuerpo)
}
