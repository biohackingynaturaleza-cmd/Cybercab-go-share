package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/billing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pagos"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
)

// ErrNadaQueLiquidar se devuelve cuando el periodo no tiene apuntes.
var ErrNadaQueLiquidar = errors.New("no hay nada pendiente en ese periodo")

// PeriodoDe devuelve el mes natural al que pertenece una fecha.
//
// El periodo es el mes y no una ventana móvil porque la gente entiende su
// extracto por meses, y porque un periodo con bordes fijos se puede reclamar:
// «lo de marzo» significa lo mismo para las dos partes.
func PeriodoDe(t time.Time) (desde, hasta time.Time) {
	desde = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	return desde, desde.AddDate(0, 1, 0)
}

// LiquidarPeriodo cierra un periodo: compensa saldos, guarda la liquidación con
// una sola instrucción de cobro o pago por persona, y avisa a cada uno.
//
// No mueve dinero. Eso lo hace EjecutarLiquidacion contra el procesador, que es
// un paso aparte a propósito: calcular es reversible mientras nadie lo haya
// ejecutado, y cobrar no.
func (s *Service) LiquidarPeriodo(desde, hasta time.Time) (*billing.Liquidacion, error) {
	if !hasta.After(desde) {
		return nil, fmt.Errorf("%w: el periodo está del revés", domain.ErrValidation)
	}
	pendientes, err := s.store.PendingEntries()
	if err != nil {
		return nil, err
	}

	liq, incluidos, err := billing.Liquidar(newID("liq"), pendientes, desde, hasta, s.cfg.Now())
	if err != nil {
		return nil, err
	}
	if len(incluidos) == 0 {
		return nil, ErrNadaQueLiquidar
	}
	for i := range liq.Instrucciones {
		liq.Instrucciones[i].ID = newID("ins")
	}

	// Guardar cierra los apuntes en la misma transacción: una liquidación
	// guardada cuyos apuntes siguieran abiertos los cobraría otra vez el mes
	// que viene.
	if err := s.store.GuardarLiquidacion(liq, incluidos); err != nil {
		return nil, err
	}
	s.avisarDeLaLiquidacion(liq)
	return liq, nil
}

// LiquidarPendientes cierra todos los periodos completos que aún no se han
// liquidado.
//
// Recorre los meses en vez de mirar solo el anterior: si el programador estuvo
// parado dos meses, al arrancar tiene que ponerse al día, no saltarse uno. El
// mes en curso no se toca, porque todavía le pueden entrar apuntes.
func (s *Service) LiquidarPendientes(now time.Time) ([]*billing.Liquidacion, error) {
	pendientes, err := s.store.PendingEntries()
	if err != nil {
		return nil, err
	}
	if len(pendientes) == 0 {
		return nil, nil
	}

	masAntiguo := pendientes[0].CreatedAt
	for _, e := range pendientes {
		if e.CreatedAt.Before(masAntiguo) {
			masAntiguo = e.CreatedAt
		}
	}
	desde, _ := PeriodoDe(masAntiguo)
	corte, _ := PeriodoDe(now)

	var hechas []*billing.Liquidacion
	for desde.Before(corte) {
		hasta := desde.AddDate(0, 1, 0)
		liq, err := s.LiquidarPeriodo(desde, hasta)
		switch {
		case err == nil:
			hechas = append(hechas, liq)
		case errors.Is(err, ErrNadaQueLiquidar), errors.Is(err, store.ErrPeriodoYaLiquidado):
			// Un mes sin apuntes, o ya cerrado por otra ejecución: se sigue.
		default:
			return hechas, err
		}
		desde = hasta
	}
	return hechas, nil
}

// EjecutarLiquidacion manda sus instrucciones al procesador de pagos.
//
// Se puede llamar tantas veces como haga falta: las instrucciones ya ejecutadas
// se saltan, y las demás van con la misma clave de idempotencia, así que un
// reintento no puede duplicar un cargo.
func (s *Service) EjecutarLiquidacion(ctx context.Context, liquidacionID string) (*billing.Liquidacion, error) {
	liq, err := s.store.GetLiquidacion(liquidacionID)
	if err != nil {
		return nil, err
	}

	concepto := pagos.ConceptoDe(liq.Desde, liq.Hasta)
	for i := range liq.Instrucciones {
		in := &liq.Instrucciones[i]
		if in.Estado == string(pagos.EstadoEjecutado) {
			continue
		}
		res, err := s.cfg.Pagos.Ejecutar(ctx, pagos.Movimiento{
			Clave:         pagos.ClaveDe(liq.ID, in.UserID),
			LiquidacionID: liq.ID,
			UserID:        in.UserID,
			Tipo:          in.Tipo,
			AmountCents:   in.AmountCents,
			Concepto:      concepto,
		})
		if err != nil {
			// Un fallo del procesador no aborta el resto: a los demás hay que
			// pagarles igual, y esta se reintenta luego con su misma clave.
			in.Estado, in.Motivo = string(pagos.EstadoFallido), err.Error()
			s.log("no se pudo ejecutar un movimiento de la liquidación", err)
		} else {
			in.Estado, in.Ref, in.Motivo = string(res.Estado), res.Ref, res.Motivo
			if res.Estado == pagos.EstadoEjecutado {
				in.EjecutadaAt = s.cfg.Now()
			}
		}
		if err := s.store.ActualizarInstruccion(liq.ID, *in); err != nil {
			return nil, err
		}
	}

	estado := liq.EstadoSegunInstrucciones(string(pagos.EstadoEjecutado))
	if estado != liq.Estado {
		if err := s.store.ActualizarEstadoLiquidacion(liq.ID, estado, s.cfg.Now()); err != nil {
			return nil, err
		}
		liq.Estado = estado
	}
	return liq, nil
}

// NombreDelProcesador identifica quién mueve el dinero. Con el que solo anota,
// nada de lo que haya en las liquidaciones se ha cobrado.
func (s *Service) NombreDelProcesador() string { return s.cfg.Pagos.Nombre() }

// Liquidaciones son todas, para operaciones.
func (s *Service) Liquidaciones() ([]*billing.Liquidacion, error) {
	return s.store.Liquidaciones()
}

// GetLiquidacion recupera una.
func (s *Service) GetLiquidacion(id string) (*billing.Liquidacion, error) {
	return s.store.GetLiquidacion(id)
}

// MiSaldo es lo que una persona ve de su dinero.
type MiSaldo struct {
	// PendienteCents es el neto de lo que todavía no se ha liquidado: positivo
	// se le cobrará, negativo se le pagará.
	PendienteCents int64 `json:"pendiente_cents"`
	// DebeCents y LeDebenCents desglosan ese neto, porque «debes 2 €» sin
	// explicar que son 12 menos 10 no se entiende.
	DebeCents     int64 `json:"debe_cents"`
	LeDebenCents  int64 `json:"le_deben_cents"`
	ComisionCents int64 `json:"comision_cents"`
	Apuntes       int   `json:"apuntes"`
	// ProximoCierre es cuándo se cierra el periodo en curso.
	ProximoCierre time.Time `json:"proximo_cierre"`
	// Movimientos son las instrucciones de periodos ya liquidados.
	Movimientos []billing.Instruccion `json:"movimientos"`
}

// SaldoDe reúne lo que esa persona debe o le deben, liquidado y sin liquidar.
func (s *Service) SaldoDe(userID string) (*MiSaldo, error) {
	apuntes, err := s.ApuntesDe(userID)
	if err != nil {
		return nil, err
	}
	_, hasta := PeriodoDe(s.cfg.Now())
	saldo := &MiSaldo{ProximoCierre: hasta, Movimientos: []billing.Instruccion{}}

	for _, e := range apuntes {
		if !e.Pendiente() {
			continue
		}
		saldo.Apuntes++
		switch {
		case e.UserID == userID && e.Kind == billing.EntryServiceFee:
			saldo.DebeCents += e.AmountCents
			saldo.ComisionCents += e.AmountCents
		case e.UserID == userID:
			saldo.DebeCents += e.AmountCents
		default:
			saldo.LeDebenCents += e.AmountCents
		}
	}
	saldo.PendienteCents = saldo.DebeCents - saldo.LeDebenCents

	movs, err := s.store.InstruccionesDe(userID)
	if err != nil {
		return nil, err
	}
	if len(movs) > 0 {
		saldo.Movimientos = movs
	}
	return saldo, nil
}

// avisarDeLaLiquidacion le dice a cada uno lo que se le va a cobrar o pagar.
//
// Solo a quien tiene movimiento: a quien se le compensa todo no hay nada que
// contarle, y un correo mensual de «tu saldo es cero» es el correo que enseña a
// la gente a no leer los nuestros.
func (s *Service) avisarDeLaLiquidacion(l *billing.Liquidacion) {
	for _, in := range l.Instrucciones {
		suceso := notify.SucesoLiquidacionCobro
		if in.Tipo == billing.Pago {
			suceso = notify.SucesoLiquidacionPago
		}
		s.avisar(in.UserID, suceso, map[string]string{
			"importe": euros(in.AmountCents),
			"periodo": l.Desde.Format("01/2006"),
		})
	}
}
