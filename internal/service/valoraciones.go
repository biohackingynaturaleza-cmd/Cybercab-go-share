package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// ErrViajeNoValorable se devuelve al valorar algo que todavía no ha pasado.
var ErrViajeNoValorable = errors.New("solo se puede valorar un viaje que ya se ha hecho")

// ValorarInput son los datos de una valoración.
type ValorarInput struct {
	BookingID  string
	AutorID    string
	Estrellas  int
	Comentario string
}

// Valorar deja la opinión de una persona sobre la otra tras compartir viaje.
//
// La valoración se guarda pero no se publica todavía: hasta que la otra parte
// valore, o venza el plazo, no cuenta para nadie ni se le enseña a nadie. Ver
// domain.Valoracion.
func (s *Service) Valorar(in ValorarInput) (*domain.Valoracion, error) {
	b, t, err := s.reservaValorable(in.BookingID)
	if err != nil {
		return nil, err
	}
	sobre, err := laOtraParte(b, t, in.AutorID)
	if err != nil {
		return nil, err
	}

	v := &domain.Valoracion{
		ID:         newID("val"),
		BookingID:  b.ID,
		TripID:     t.ID,
		AutorID:    in.AutorID,
		SobreID:    sobre,
		Estrellas:  in.Estrellas,
		Comentario: domain.NormalizarComentario(in.Comentario),
		CreatedAt:  s.cfg.Now(),
	}
	if err := v.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrValidation, err)
	}
	if err := s.store.CrearValoracion(v); err != nil {
		return nil, err
	}

	// Si con esta ya han valorado los dos, las dos se publican a la vez y cada
	// uno se entera de lo que le pusieron. Antes de eso no hay nada que contar:
	// enseñarla antes es justo lo que abre la puerta a la represalia.
	s.avisarSiSePublica(b.ID)
	return v, nil
}

// reservaValorable devuelve la reserva y su trayecto si ese viaje ya se puede
// valorar.
func (s *Service) reservaValorable(bookingID string) (*domain.Booking, *domain.Trip, error) {
	b, err := s.store.GetBooking(bookingID)
	if err != nil {
		return nil, nil, err
	}
	if b.Status != domain.BookingConfirmed {
		return nil, nil, fmt.Errorf("%w: esa plaza no llegó a confirmarse", ErrViajeNoValorable)
	}
	t, err := s.store.GetTrip(b.TripID)
	if err != nil {
		return nil, nil, err
	}
	if t.Status != domain.TripCompleted {
		return nil, nil, fmt.Errorf("%w: el trayecto todavía no está cerrado", ErrViajeNoValorable)
	}
	return b, t, nil
}

// laOtraParte devuelve a quién le toca valorar quien pide valorar.
//
// Solo se valoran las dos partes de una reserva: son las que tuvieron un trato
// entre sí y las únicas cuyo derecho a opinar se puede comprobar.
func laOtraParte(b *domain.Booking, t *domain.Trip, autorID string) (string, error) {
	switch autorID {
	case t.HostID:
		return b.PassengerID, nil
	case b.PassengerID:
		return t.HostID, nil
	}
	return "", fmt.Errorf("%w: no compartiste este viaje", ErrNoAutorizado)
}

// avisarSiSePublica manda los dos avisos cuando la segunda valoración cierra la
// pareja.
func (s *Service) avisarSiSePublica(bookingID string) {
	todas, err := s.store.ValoracionesDeBooking(bookingID)
	if err != nil {
		s.log("no se pudieron leer las valoraciones del viaje", err)
		return
	}
	if len(todas) < 2 {
		return
	}
	for _, v := range todas {
		datos := map[string]string{"estrellas": fmt.Sprintf("%d", v.Estrellas)}
		if autor, err := s.store.GetUser(v.AutorID); err == nil {
			datos["quien"] = autor.Name
		}
		s.avisar(v.SobreID, notify.SucesoValoracionRecibida, datos)
	}
}

// ValoracionPublica es una valoración ya visible, tal y como se enseña.
type ValoracionPublica struct {
	Estrellas  int       `json:"estrellas"`
	Comentario string    `json:"comentario,omitempty"`
	Autor      string    `json:"autor"`
	Fecha      time.Time `json:"fecha"`
}

// ValoracionesSobre devuelve las valoraciones ya publicadas de una persona.
func (s *Service) ValoracionesSobre(userID string) ([]ValoracionPublica, error) {
	visibles, err := s.valoracionesVisibles(userID)
	if err != nil {
		return nil, err
	}
	out := make([]ValoracionPublica, 0, len(visibles))
	for _, v := range visibles {
		p := ValoracionPublica{Estrellas: v.Estrellas, Comentario: v.Comentario, Fecha: v.CreatedAt}
		if autor, err := s.store.GetUser(v.AutorID); err == nil {
			p.Autor = autor.Name
		}
		out = append(out, p)
	}
	return out, nil
}

// valoracionesVisibles filtra las recibidas por la regla de publicación.
func (s *Service) valoracionesVisibles(userID string) ([]*domain.Valoracion, error) {
	recibidas, err := s.store.ValoracionesRecibidas(userID)
	if err != nil {
		return nil, err
	}
	if len(recibidas) == 0 {
		return nil, nil
	}
	emitidas, err := s.store.ValoracionesEmitidas(userID)
	if err != nil {
		return nil, err
	}
	// Con las emitidas basta para saber si existe la recíproca: la recíproca de
	// una valoración recibida es la que esta persona escribió sobre la misma
	// reserva.
	respondidas := make(map[string]bool, len(emitidas))
	for _, v := range emitidas {
		respondidas[v.BookingID] = true
	}

	now := s.cfg.Now()
	var out []*domain.Valoracion
	for _, v := range recibidas {
		if v.Visible(respondidas[v.BookingID], now) {
			out = append(out, v)
		}
	}
	return out, nil
}

// estadisticasDe reúne el historial que sostiene el nivel de confianza.
//
// La media sale de las valoraciones publicadas cada vez que se pregunta, no de
// un promedio guardado: una valoración cambia de invisible a visible sola, al
// vencer el plazo, y ningún contador guardado se entera de eso.
func (s *Service) estadisticasDe(u *domain.User) (trust.Stats, error) {
	visibles, err := s.valoracionesVisibles(u.ID)
	if err != nil {
		return trust.Stats{}, err
	}
	stats := trust.Stats{CompletedTrips: u.RideCount, RatingCount: len(visibles)}
	if len(visibles) == 0 {
		return stats, nil
	}
	var suma int
	for _, v := range visibles {
		suma += v.Estrellas
	}
	stats.Rating = float64(suma) / float64(len(visibles))
	return stats, nil
}

// PendienteDeValorar es un viaje hecho que todavía espera la opinión de alguien.
type PendienteDeValorar struct {
	BookingID string    `json:"booking_id"`
	TripID    string    `json:"trip_id"`
	SobreID   string    `json:"sobre_id"`
	Nombre    string    `json:"nombre"`
	Destino   string    `json:"destino"`
	Salida    time.Time `json:"salida"`
}

// PendientesDeValorar son los viajes completados que esa persona aún no ha
// valorado.
//
// Es lo que mueve el sistema entero: sin recordárselo a la gente, casi nadie
// valora, y una reputación construida sobre tres valoraciones no dice nada.
func (s *Service) PendientesDeValorar(userID string) ([]PendienteDeValorar, error) {
	emitidas, err := s.store.ValoracionesEmitidas(userID)
	if err != nil {
		return nil, err
	}
	yaValoradas := make(map[string]bool, len(emitidas))
	for _, v := range emitidas {
		yaValoradas[v.BookingID] = true
	}

	var out []PendienteDeValorar
	añadir := func(b *domain.Booking, t *domain.Trip) {
		if yaValoradas[b.ID] || b.Status != domain.BookingConfirmed || t.Status != domain.TripCompleted {
			return
		}
		sobre, err := laOtraParte(b, t, userID)
		if err != nil {
			return
		}
		p := PendienteDeValorar{
			BookingID: b.ID, TripID: t.ID, SobreID: sobre,
			Destino: t.Destination.Name, Salida: t.DepartureTime,
		}
		if u, err := s.store.GetUser(sobre); err == nil {
			p.Nombre = u.Name
		}
		out = append(out, p)
	}

	// Como pasajero: una reserva, una valoración.
	misReservas, err := s.store.BookingsByPassenger(userID)
	if err != nil {
		return nil, err
	}
	for _, b := range misReservas {
		t, err := s.store.GetTrip(b.TripID)
		if err != nil {
			continue
		}
		añadir(b, t)
	}

	// Como quien organiza: una valoración por cada pasajero que llevó.
	misTrayectos, err := s.store.TripsByHost(userID)
	if err != nil {
		return nil, err
	}
	for _, t := range misTrayectos {
		if t.Status != domain.TripCompleted {
			continue
		}
		reservas, err := s.store.BookingsByTrip(t.ID)
		if err != nil {
			continue
		}
		for _, b := range reservas {
			añadir(b, t)
		}
	}
	return out, nil
}
