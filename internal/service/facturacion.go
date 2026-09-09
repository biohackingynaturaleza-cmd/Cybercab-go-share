package service

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/billing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/fleet"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
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
	t, err := s.store.GetTrip(in.TripID)
	if err != nil {
		return nil, nil, err
	}
	if t.HostID != in.HostID {
		return nil, nil, fmt.Errorf("%w: solo quien organiza puede cerrar el trayecto", ErrNoAutorizado)
	}
	if t.Status == domain.TripCompleted {
		return nil, nil, fmt.Errorf("%w: ya estaba completado", ErrViajeNoCompletable)
	}
	if t.Status == domain.TripCancelled {
		return nil, nil, fmt.Errorf("%w: está anulado", ErrViajeNoCompletable)
	}
	if in.ImporteRealCents < 0 {
		return nil, nil, fmt.Errorf("%w: el importe no puede ser negativo", domain.ErrValidation)
	}

	bookings, err := s.store.BookingsByTrip(in.TripID)
	if err != nil {
		return nil, nil, err
	}
	confirmadas := make([]*domain.Booking, 0, len(bookings))
	for _, b := range bookings {
		if b.Status == domain.BookingConfirmed {
			confirmadas = append(confirmadas, b)
		}
	}

	total := s.costeDelViaje(t)
	if in.ImporteRealCents > 0 {
		total = in.ImporteRealCents
		t.TarifaRealCents = in.ImporteRealCents
	}

	partes := s.partesFinales(confirmadas, total)

	// La comprobación de la que depende que esto sea gasto compartido y no
	// transporte comercial. partesFinales lo garantiza por construcción, pero
	// esa garantía no puede quedar solo en la cabeza de quien la escribió.
	amounts := map[string]int64{}
	for _, b := range confirmadas {
		amounts[b.PassengerID] += partes[b.ID]
	}
	amounts[t.HostID] = total - sumaDe(amounts)
	if err := pricing.VerificarSinLucro(total, amounts, t.HostID); err != nil {
		return nil, nil, err
	}

	now := s.cfg.Now()
	var entries []billing.Entry
	for _, b := range confirmadas {
		importe := partes[b.ID]
		if importe <= 0 {
			continue
		}
		entries = append(entries, billing.Entry{
			ID:             newID("ent"),
			UserID:         b.PassengerID,
			TripID:         t.ID,
			BookingID:      b.ID,
			Kind:           billing.EntryCostShare,
			AmountCents:    importe,
			CounterpartyID: t.HostID,
			CreatedAt:      now,
		})
		if fee := comision(importe, s.comisionBps()); fee > 0 {
			entries = append(entries, billing.Entry{
				ID:          newID("ent"),
				UserID:      b.PassengerID,
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
		if tp, ok := s.cfg.Flota.(*fleet.Traspaso); ok {
			if _, err := tp.Registrar(in.RefViaje); err != nil {
				// Que la referencia ya estuviera registrada no debe impedir
				// cerrar el viaje: el almacén tiene su propia unicidad.
				s.log("no se pudo registrar la referencia en la flota", err)
			}
		}
	}

	t.Status = domain.TripCompleted
	if err := s.store.UpdateTrip(t); err != nil {
		return nil, nil, err
	}

	// El historial de todos los que iban dentro. Se suma aquí y solo aquí:
	// cerrar el trayecto es una transición que su propio estado impide repetir,
	// así que nadie puede contar dos veces el mismo viaje.
	participantes := []string{t.HostID}
	for _, b := range confirmadas {
		participantes = append(participantes, b.PassengerID)
	}
	if err := s.store.IncrementarViajes(participantes); err != nil {
		s.log("no se pudo apuntar el viaje en el historial", err)
	}

	s.pedirValoraciones(t, confirmadas)
	return t, entries, nil
}

// pedirValoraciones le pide su opinión a cada pareja del viaje.
//
// Sin este recordatorio casi nadie valora, y una reputación sostenida por tres
// valoraciones no dice nada de nadie.
func (s *Service) pedirValoraciones(t *domain.Trip, confirmadas []*domain.Booking) {
	host, err := s.store.GetUser(t.HostID)
	if err != nil {
		s.log("no se pudo pedir la valoración", err)
		return
	}
	for _, b := range confirmadas {
		pasajero, err := s.store.GetUser(b.PassengerID)
		if err != nil {
			continue
		}
		datos := s.datosDelViaje(t)
		datos["quien"] = pasajero.Name
		s.avisar(t.HostID, notify.SucesoPideValoracion, datos)

		delPasajero := s.datosDelViaje(t)
		delPasajero["quien"] = host.Name
		s.avisar(b.PassengerID, notify.SucesoPideValoracion, delPasajero)
	}
}

// partesFinales decide lo que paga cada pasajero al cerrar el viaje.
//
// La regla es una sola y lo resuelve todo: **el precio que aceptó el pasajero
// nunca sube**. Si la flota acaba cobrando más de lo presupuestado, la
// diferencia la asume quien organiza, que es quien vio el presupuesto y eligió
// la ruta. Con eso, inflar la tarifa al cerrar deja de dar dinero y el
// incentivo a mentir desaparece.
//
// Y baja si el viaje salió más barato: si los pasajeros pagaran lo pactado
// cuando la flota cobró menos, quien organiza ganaría dinero, y eso rompería la
// excepción de gastos compartidos de la que depende la legalidad del servicio.
func (s *Service) partesFinales(confirmadas []*domain.Booking, totalReal int64) map[string]int64 {
	partes := make(map[string]int64, len(confirmadas))

	var pactado int64
	for _, b := range confirmadas {
		partes[b.ID] = b.PriceCents
		pactado += b.PriceCents
	}
	if pactado <= totalReal || pactado == 0 {
		return partes
	}

	// Lo que aportan los pasajeros no puede superar el coste del viaje: se
	// reescala a la baja, repartiendo el resto por el mayor resto para que la
	// suma cuadre al céntimo.
	restos := make([]struct {
		id    string
		resto float64
	}, 0, len(confirmadas))
	var asignado int64

	for _, b := range confirmadas {
		exacto := float64(b.PriceCents) * float64(totalReal) / float64(pactado)
		partes[b.ID] = int64(math.Floor(exacto))
		asignado += partes[b.ID]
		restos = append(restos, struct {
			id    string
			resto float64
		}{b.ID, exacto - math.Floor(exacto)})
	}

	sort.SliceStable(restos, func(i, j int) bool { return restos[i].resto > restos[j].resto })
	for i := 0; asignado < totalReal && len(restos) > 0; i++ {
		partes[restos[i%len(restos)].id]++
		asignado++
	}
	return partes
}

func sumaDe(m map[string]int64) int64 {
	var total int64
	for _, v := range m {
		total += v
	}
	return total
}

// costeDelViaje es lo que cuesta el trayecto: la tarifa que declaró quien
// organiza si la hay, y si no nuestra estimación.
func (s *Service) costeDelViaje(t *domain.Trip) int64 {
	if t.TarifaRealCents > 0 {
		return t.TarifaRealCents
	}
	if t.TarifaDeclaradaCents > 0 {
		return t.TarifaDeclaradaCents
	}
	return s.tripCost(t.DistanceKm())
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

// log deja constancia de un problema que no impide seguir.
func (s *Service) log(msg string, err error) {
	if s.cfg.Log != nil {
		s.cfg.Log.Warn(msg, "err", err)
	}
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
