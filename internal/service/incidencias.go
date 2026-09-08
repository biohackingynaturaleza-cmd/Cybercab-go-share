package service

import (
	"errors"
	"fmt"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/billing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
)

// ErrIncidenciaResuelta se devuelve al tocar una incidencia ya cerrada.
var ErrIncidenciaResuelta = errors.New("esa incidencia ya está resuelta")

// DeclararIncidenciaInput son los datos de un cargo posterior al viaje.
type DeclararIncidenciaInput struct {
	TripID       string
	DeclaranteID string
	AtribuidaA   string
	Tipo         domain.TipoIncidencia
	ImporteCents int64
	Descripcion  string
}

// DeclararIncidencia registra un cargo que la flota hizo después del viaje y
// que quien organiza quiere repercutir a quien lo causó.
//
// La flota cobra las tasas de limpieza a quien pidió el coche, aunque el
// destrozo lo hiciera otro. Sin esto, quien organiza asume una responsabilidad
// de hasta 150 $ sin ninguna herramienta para repercutirla.
//
// Declararla no la cobra: hasta que la otra parte la acepta, no existe como
// deuda. Cargarle dinero a alguien por la sola palabra de otro sería un agujero
// evidente, y esta aplicación no puede arbitrar quién tiene razón.
func (s *Service) DeclararIncidencia(in DeclararIncidenciaInput) (*domain.Incidencia, error) {
	t, err := s.store.GetTrip(in.TripID)
	if err != nil {
		return nil, err
	}
	if t.HostID != in.DeclaranteID {
		return nil, fmt.Errorf("%w: solo quien organiza recibe los cargos de la flota", ErrNoAutorizado)
	}
	if t.Status != domain.TripCompleted {
		return nil, fmt.Errorf("%w: el viaje todavía no se ha cerrado", domain.ErrValidation)
	}

	// Solo se puede atribuir a quien iba a bordo.
	bookings, err := s.store.BookingsByTrip(in.TripID)
	if err != nil {
		return nil, err
	}
	viajaba := false
	for _, b := range bookings {
		if b.PassengerID == in.AtribuidaA && b.Status == domain.BookingConfirmed {
			viajaba = true
			break
		}
	}
	if !viajaba {
		return nil, fmt.Errorf("%w: esa persona no viajaba en este trayecto", domain.ErrValidation)
	}

	i := &domain.Incidencia{
		ID:           newID("inc"),
		TripID:       t.ID,
		DeclaranteID: in.DeclaranteID,
		AtribuidaA:   in.AtribuidaA,
		Tipo:         in.Tipo,
		ImporteCents: in.ImporteCents,
		Descripcion:  in.Descripcion,
		Estado:       domain.IncidenciaDeclarada,
		CreatedAt:    s.cfg.Now(),
	}
	if err := i.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrValidation, err)
	}
	if err := s.store.CreateIncidencia(i); err != nil {
		return nil, err
	}

	datos := s.datosDelViaje(t)
	datos["importe"] = euros(i.ImporteCents)
	datos["motivo"] = i.Descripcion
	s.avisar(i.AtribuidaA, notify.SucesoIncidenciaDeclarada, datos)

	return i, nil
}

// ResponderIncidencia deja que quien la recibe la acepte o la discuta.
//
// Aceptarla la convierte en deuda y entra en la siguiente liquidación.
// Discutirla la deja abierta: no se cobra nada y hace falta que lo resuelva una
// persona. Es el único final honesto cuando dos versiones se contradicen y la
// aplicación no puede saber cuál es cierta.
func (s *Service) ResponderIncidencia(incidenciaID, actorID string, acepta bool) (*domain.Incidencia, error) {
	i, err := s.store.GetIncidencia(incidenciaID)
	if err != nil {
		return nil, err
	}
	if i.AtribuidaA != actorID {
		return nil, fmt.Errorf("%w: esa incidencia no va contigo", ErrNoAutorizado)
	}
	if i.Estado != domain.IncidenciaDeclarada {
		return nil, fmt.Errorf("%w (%s)", ErrIncidenciaResuelta, i.Estado)
	}

	if !acepta {
		i.Estado = domain.IncidenciaDiscutida
		if err := s.store.UpdateIncidencia(i); err != nil {
			return nil, err
		}
		s.avisar(i.DeclaranteID, notify.SucesoIncidenciaDiscutida, map[string]string{
			"importe": euros(i.ImporteCents),
		})
		return i, nil
	}

	// Aceptada: se anota como deuda hacia quien organiza. No lleva comisión
	// nuestra — no hemos hecho nada por ese dinero, solo lo trasladamos.
	apunte := billing.Entry{
		ID:             newID("ent"),
		UserID:         i.AtribuidaA,
		TripID:         i.TripID,
		Kind:           billing.EntryCostShare,
		AmountCents:    i.ImporteCents,
		CounterpartyID: i.DeclaranteID,
		CreatedAt:      s.cfg.Now(),
	}
	if err := s.store.CreateEntries([]billing.Entry{apunte}); err != nil {
		return nil, err
	}

	i.Estado = domain.IncidenciaAceptada
	i.ResueltaAt = s.cfg.Now()
	if err := s.store.UpdateIncidencia(i); err != nil {
		return nil, err
	}
	s.avisar(i.DeclaranteID, notify.SucesoIncidenciaAceptada, map[string]string{
		"importe": euros(i.ImporteCents),
	})
	return i, nil
}

// RetirarIncidencia deja que quien la declaró se eche atrás.
func (s *Service) RetirarIncidencia(incidenciaID, actorID string) (*domain.Incidencia, error) {
	i, err := s.store.GetIncidencia(incidenciaID)
	if err != nil {
		return nil, err
	}
	if i.DeclaranteID != actorID {
		return nil, fmt.Errorf("%w: solo quien la declaró puede retirarla", ErrNoAutorizado)
	}
	if i.Estado == domain.IncidenciaAceptada {
		return nil, fmt.Errorf("%w: ya está aceptada y anotada", ErrIncidenciaResuelta)
	}
	i.Estado = domain.IncidenciaRetirada
	i.ResueltaAt = s.cfg.Now()
	if err := s.store.UpdateIncidencia(i); err != nil {
		return nil, err
	}
	return i, nil
}

// MisIncidencias son las que a una persona le afectan, en cualquier dirección.
func (s *Service) MisIncidencias(userID string) ([]*domain.Incidencia, error) {
	return s.store.IncidenciasDe(userID)
}
