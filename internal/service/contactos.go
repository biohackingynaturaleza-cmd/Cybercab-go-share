package service

import (
	"fmt"
	"strings"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
)

// ErrDemasiadosContactos se devuelve al pasar del tope.
var ErrDemasiadosContactos = fmt.Errorf("no puedes tener más de %d contactos de confianza", domain.MaxContactos)

// ContactoInput son los datos de un contacto de confianza.
type ContactoInput struct {
	UserID string
	Nombre string
	Email  string
	// AvisarAlSalir manda el enlace de seguimiento al empezar cada viaje.
	AvisarAlSalir bool
}

// AñadirContacto da de alta a alguien a quien avisar si algo va mal.
func (s *Service) AñadirContacto(in ContactoInput) (*domain.ContactoDeConfianza, error) {
	ya, err := s.store.ContactosDe(in.UserID)
	if err != nil {
		return nil, err
	}
	if len(ya) >= domain.MaxContactos {
		return nil, ErrDemasiadosContactos
	}

	c := &domain.ContactoDeConfianza{
		ID:            newID("ctc"),
		UserID:        in.UserID,
		Nombre:        strings.TrimSpace(in.Nombre),
		Email:         strings.TrimSpace(in.Email),
		AvisarAlSalir: in.AvisarAlSalir,
		CreatedAt:     s.cfg.Now(),
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrValidation, err)
	}
	// Avisarse a uno mismo no es tener un contacto de confianza: si el aviso
	// llega a tu propio buzón, no lo va a leer nadie más que tú.
	if u, err := s.store.GetUser(in.UserID); err == nil && strings.EqualFold(u.Email, c.Email) {
		return nil, fmt.Errorf("%w: un contacto de confianza tiene que ser otra persona", domain.ErrValidation)
	}
	if err := s.store.CrearContacto(c); err != nil {
		return nil, err
	}
	return c, nil
}

// MisContactos devuelve a quién se avisaría.
func (s *Service) MisContactos(userID string) ([]*domain.ContactoDeConfianza, error) {
	return s.store.ContactosDe(userID)
}

// BorrarContacto quita a alguien de la lista. El dueño va en la consulta: nadie
// borra el contacto de otro.
func (s *Service) BorrarContacto(id, userID string) error {
	return s.store.BorrarContacto(id, userID)
}
