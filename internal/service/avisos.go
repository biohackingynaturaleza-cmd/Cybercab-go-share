package service

import (
	"fmt"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
)

// avisar manda un correo a alguien sobre algo que le ha pasado.
//
// Nunca devuelve error: los avisos se encolan y se envían aparte, porque un
// servidor de correo caído no puede deshacer una reserva que ya existe.
func (s *Service) avisar(userID string, suceso notify.Suceso, datos map[string]string) {
	u, err := s.store.GetUser(userID)
	if err != nil {
		s.log("no se pudo avisar: usuario desconocido", err)
		return
	}
	if datos == nil {
		datos = map[string]string{}
	}
	if _, ok := datos["enlace"]; !ok {
		datos["enlace"] = s.cfg.PublicURL
	}

	s.cfg.Avisos.Notificar(notify.Aviso{
		Suceso: suceso,
		Para:   u.Email,
		Nombre: u.Name,
		Idioma: u.Idioma,
		Datos:  datos,
	})
}

// datosDelViaje son los campos que aparecen en casi todos los correos.
func (s *Service) datosDelViaje(t *domain.Trip) map[string]string {
	return map[string]string{
		"destino": t.Destination.Name,
		"origen":  t.Origin.Name,
		"salida":  t.DepartureTime.Format("02/01/2006 15:04") + " UTC",
		"enlace":  s.cfg.PublicURL,
	}
}

// avisarPlazaPedida le dice a quien organiza que alguien quiere subirse, con lo
// que necesita para decidir sin tener que ir a buscarlo.
func (s *Service) avisarPlazaPedida(t *domain.Trip, b *domain.Booking) {
	datos := s.datosDelViaje(t)
	datos["km"] = fmt.Sprintf("%.1f", b.SharedKm())
	datos["importe"] = euros(b.PriceCents)

	if p, err := s.PerfilDe(b.PassengerID); err == nil {
		datos["pasajero"] = p.Nombre
		datos["nivel"] = p.Nivel.Label()
	}
	s.avisar(t.HostID, notify.SucesoPlazaPedida, datos)
}

// avisarDecision le dice al pasajero si tiene sitio o no.
func (s *Service) avisarDecision(t *domain.Trip, b *domain.Booking, aceptada bool) {
	datos := s.datosDelViaje(t)
	if !aceptada {
		s.avisar(b.PassengerID, notify.SucesoPlazaRechazada, datos)
		return
	}

	datos["km"] = fmt.Sprintf("%.1f", b.SharedKm())
	datos["importe"] = euros(b.PriceCents)
	// Lo que le habría costado ir solo: el aviso es también el recordatorio de
	// para qué sirve todo esto.
	datos["solo"] = euros(s.tripCost(b.SharedKm()))
	if h, err := s.store.GetUser(t.HostID); err == nil {
		datos["host"] = h.Name
	}
	s.avisar(b.PassengerID, notify.SucesoPlazaAceptada, datos)
}

// avisarReservaAnulada avisa a la otra parte de la anulación.
func (s *Service) avisarReservaAnulada(t *domain.Trip, b *domain.Booking, quienAnula string) {
	otra := t.HostID
	if quienAnula == t.HostID {
		otra = b.PassengerID
	}
	s.avisar(otra, notify.SucesoReservaAnulada, s.datosDelViaje(t))
}

// avisarTrayectoAnulado avisa a quienes se quedan sin plaza.
func (s *Service) avisarTrayectoAnulado(t *domain.Trip, afectados []*domain.Booking) {
	datos := s.datosDelViaje(t)
	if h, err := s.store.GetUser(t.HostID); err == nil {
		datos["host"] = h.Name
	}
	for _, b := range afectados {
		s.avisar(b.PassengerID, notify.SucesoTrayectoAnulado, datos)
	}
}

func euros(cents int64) string {
	return fmt.Sprintf("$%.2f", float64(cents)/100)
}
