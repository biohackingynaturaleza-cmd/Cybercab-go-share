package service

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/billing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/fleet"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pricing"
)

// ComisionPorDefectoBps es la comisión de servicio: 20 % (2000 puntos básicos).
//
// La paga el pasajero sobre su parte del coste, y es ingreso de la plataforma.
// No entra en el reparto: quien organiza sigue recibiendo solo su coste, que es
// lo que mantiene el servicio dentro de la excepción de gastos compartidos.
const ComisionPorDefectoBps int64 = 2000

// ErrViajeNoCompletable se devuelve al cerrar un viaje que no está en estado de
// cerrarse.
var ErrViajeNoCompletable = errors.New("este trayecto no se puede dar por completado")

// CierreInput son los datos con los que quien organiza cierra un trayecto.
type CierreInput struct {
	TripID string
	HostID string
	// ImporteRealCents es lo que de verdad cobró la flota. Si se informa,
	// manda sobre nuestra estimación.
	ImporteRealCents int64
	// RefViaje es la referencia del viaje en la app de Tesla. En el modelo de
	// traspaso es lo único que ata este trayecto con el viaje real, así que sin
	// ella no se puede auditar ni reclamar nada.
	RefViaje string
}

// CompletarViaje cierra un trayecto y escribe en el libro lo que cada cual debe.
//
// No cobra nada: solo anota. El dinero se mueve una vez por periodo, en la
// liquidación, porque cobrar viaje a viaje se lleva en comisiones del
// procesador prácticamente todo el ingreso.
func (s *Service) CompletarViaje(in CierreInput) (*domain.Trip, []billing.Entry, error) {
	tripID, hostID, importeRealCents := in.TripID, in.HostID, in.ImporteRealCents
	t, err := s.store.GetTrip(tripID)
	if err != nil {
		return nil, nil, err
	}
	if t.HostID != hostID {
		return nil, nil, fmt.Errorf("%w: solo quien organiza puede cerrar el trayecto", ErrNoAutorizado)
	}
	if t.Status == domain.TripCompleted {
		return nil, nil, fmt.Errorf("%w: ya estaba completado", ErrViajeNoCompletable)
	}
	if t.Status == domain.TripCancelled {
		return nil, nil, fmt.Errorf("%w: está anulado", ErrViajeNoCompletable)
	}

	fb, err := s.FareBreakdownFor(tripID)
	if err != nil {
		return nil, nil, err
	}
	// Si la flota informa del importe realmente cobrado, el reparto se rehace
	// sobre él: la estimación de la tarifa solo vale hasta que se sabe el
	// precio de verdad.
	if importeRealCents > 0 && importeRealCents != fb.TotalCents {
		fb, err = s.repartirSobre(t, importeRealCents)
		if err != nil {
			return nil, nil, err
		}
	}

	bookings, err := s.store.BookingsByTrip(tripID)
	if err != nil {
		return nil, nil, err
	}
	porPasajero := map[string]*domain.Booking{}
	for _, b := range bookings {
		if b.Status == domain.BookingConfirmed {
			porPasajero[b.PassengerID] = b
		}
	}

	now := s.cfg.Now()
	var entries []billing.Entry
	for _, share := range fb.Shares {
		if share.Role != "passenger" || share.AmountCents <= 0 {
			continue
		}
		b, ok := porPasajero[share.UserID]
		if !ok {
			// Solo se cobra a quien tenía la plaza confirmada.
			continue
		}

		entries = append(entries, billing.Entry{
			ID:             newID("ent"),
			UserID:         share.UserID,
			TripID:         t.ID,
			BookingID:      b.ID,
			Kind:           billing.EntryCostShare,
			AmountCents:    share.AmountCents,
			CounterpartyID: t.HostID,
			CreatedAt:      now,
		})

		if fee := comision(share.AmountCents, s.comisionBps()); fee > 0 {
			entries = append(entries, billing.Entry{
				ID:          newID("ent"),
				UserID:      share.UserID,
				TripID:      t.ID,
				BookingID:   b.ID,
				Kind:        billing.EntryServiceFee,
				AmountCents: fee,
				CreatedAt:   now,
			})
		}
	}

	if len(entries) > 0 {
		if err := s.store.CreateEntries(entries); err != nil {
			return nil, nil, err
		}
	}

	if in.RefViaje != "" {
		t.FleetRideRef = in.RefViaje
		// Se registra también en el proveedor de flota, que es quien lleva el
		// estado del viaje real.
		if s.cfg.Flota != nil {
			if tp, ok := s.cfg.Flota.(*fleet.Traspaso); ok {
				if _, err := tp.Registrar(in.RefViaje); err != nil {
					// Que la referencia ya estuviera registrada no debe impedir
					// cerrar el viaje: el almacén tiene su propia unicidad.
					s.log("no se pudo registrar la referencia en la flota", err)
				}
			}
		}
	}

	t.Status = domain.TripCompleted
	if err := s.store.UpdateTrip(t); err != nil {
		return nil, nil, err
	}
	return t, entries, nil
}

// log deja constancia de un problema que no impide seguir.
func (s *Service) log(msg string, err error) {
	if s.cfg.Log != nil {
		s.cfg.Log.Warn(msg, "err", err)
	}
}

// LiquidarPeriodo cierra un periodo: compensa saldos y produce una sola
// instrucción de cobro o pago por persona.
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
	// Los apuntes se marcan después de que la liquidación haya cuadrado: si
	// cuadrar fallara, nada se da por cobrado.
	if len(incluidos) > 0 {
		if err := s.store.MarkSettled(incluidos, liq.ID); err != nil {
			return nil, err
		}
	}
	return liq, nil
}

// AhorroDeAgrupar mide lo que se ahorra liquidando por periodos en lugar de
// cobrar viaje a viaje, sobre los apuntes que hay ahora mismo.
func (s *Service) AhorroDeAgrupar(desde, hasta time.Time) (*billing.Ahorro, error) {
	pendientes, err := s.store.PendingEntries()
	if err != nil {
		return nil, err
	}
	liq, _, err := billing.Liquidar("simulacion", pendientes, desde, hasta, s.cfg.Now())
	if err != nil {
		return nil, err
	}
	a := billing.StripeEstandar().Comparar(liq, pendientes)
	return &a, nil
}

// repartirSobre rehace el desglose con un coste distinto al estimado.
func (s *Service) repartirSobre(t *domain.Trip, totalCents int64) (*FareBreakdown, error) {
	fb, err := s.FareBreakdownFor(t.ID)
	if err != nil {
		return nil, err
	}
	if fb.TotalCents == totalCents || fb.TotalCents == 0 {
		return fb, nil
	}

	// Se reescalan las partes y se corrige el redondeo contra quien organiza,
	// que es quien paga el resto del viaje: así la suma sigue cuadrando con el
	// importe real y nadie acaba pagando de más.
	factor := float64(totalCents) / float64(fb.TotalCents)
	var repartido int64
	for i := range fb.Shares {
		if fb.Shares[i].Role == "host" {
			continue
		}
		fb.Shares[i].AmountCents = int64(math.Round(float64(fb.Shares[i].AmountCents) * factor))
		repartido += fb.Shares[i].AmountCents
	}
	for i := range fb.Shares {
		if fb.Shares[i].Role == "host" {
			fb.Shares[i].AmountCents = totalCents - repartido
		}
	}
	fb.TotalCents = totalCents
	fb.SoloCostCents = totalCents

	amounts := map[string]int64{}
	for _, sh := range fb.Shares {
		amounts[sh.UserID] += sh.AmountCents
	}
	if err := pricing.VerificarSinLucro(totalCents, amounts, t.HostID); err != nil {
		return nil, err
	}
	return fb, nil
}

func (s *Service) comisionBps() int64 {
	if s.cfg.ComisionBps > 0 {
		return s.cfg.ComisionBps
	}
	return ComisionPorDefectoBps
}

func comision(baseCents, bps int64) int64 {
	return int64(math.Round(float64(baseCents) * float64(bps) / 10000))
}

// ApuntesDe devuelve los apuntes en los que interviene una persona, en
// cualquiera de las dos direcciones.
func (s *Service) ApuntesDe(userID string) ([]billing.Entry, error) {
	todos, err := s.store.PendingEntries()
	if err != nil {
		return nil, err
	}
	var mios []billing.Entry
	for _, e := range todos {
		if e.UserID == userID || e.CounterpartyID == userID {
			mios = append(mios, e)
		}
	}
	return mios, nil
}
