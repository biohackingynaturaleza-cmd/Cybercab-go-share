package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
)

// ErrSinTratoPrevio se devuelve al denunciar a alguien con quien no se ha
// compartido nada.
//
// Denunciar a un desconocido convertiría el sistema en un arma: bastaría con
// registrarse y repartir denuncias contra quien molesta. Solo se puede
// denunciar a quien estuvo en el mismo coche.
var ErrSinTratoPrevio = errors.New("solo puedes denunciar a alguien con quien hayas compartido un viaje")

// ErrDenunciaResuelta se devuelve al resolver una denuncia ya cerrada.
var ErrDenunciaResuelta = errors.New("esa denuncia ya está resuelta")

// ErrSuspendido se devuelve a quien está apartado de compartir viajes.
var ErrSuspendido = errors.New("tu cuenta no puede compartir viajes ahora mismo")

// DenunciarInput son los datos de una denuncia.
type DenunciarInput struct {
	DenuncianteID string
	DenunciadoID  string
	Motivo        domain.MotivoDenuncia
	Descripcion   string
	// TripID es opcional: ata la denuncia a un viaje concreto cuando lo hay.
	TripID string
}

// Denunciar registra lo que alguien cuenta sobre otra persona.
//
// No decide nada por sí sola: la revisa una persona. Lo automático sería
// convertir la denuncia en un arma, porque el que denuncia elige a quién y
// cuándo, y nadie comprueba nada por él.
func (s *Service) Denunciar(in DenunciarInput) (*domain.Denuncia, error) {
	if _, err := s.store.GetUser(in.DenunciadoID); err != nil {
		return nil, err
	}
	compartido, err := s.hanCompartidoViaje(in.DenuncianteID, in.DenunciadoID)
	if err != nil {
		return nil, err
	}
	if !compartido {
		return nil, ErrSinTratoPrevio
	}

	d := &domain.Denuncia{
		ID:            newID("den"),
		TripID:        in.TripID,
		DenuncianteID: in.DenuncianteID,
		DenunciadoID:  in.DenunciadoID,
		Motivo:        in.Motivo,
		Descripcion:   domain.NormalizarComentario(in.Descripcion),
		Estado:        domain.DenunciaAbierta,
		CreatedAt:     s.cfg.Now(),
	}
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrValidation, err)
	}
	if err := s.store.CrearDenuncia(d); err != nil {
		return nil, err
	}

	// Denunciar implica no querer volver a ver a esa persona. Pedirlo aparte
	// sería dejar que quien acaba de pasar un mal rato se cruce otra vez con
	// quien se lo hizo pasar por no haber marcado una casilla.
	if err := s.store.CreateBlock(in.DenuncianteID, in.DenunciadoID); err != nil {
		s.log("no se pudo bloquear tras la denuncia", err)
	}

	// El registro es lo que hace que operaciones se entere de una denuncia de
	// seguridad sin esperar a que alguien abra la cola.
	if s.cfg.Log != nil {
		s.cfg.Log.Warn("denuncia recibida",
			"id", d.ID, "motivo", string(d.Motivo), "urgente", d.Motivo.Urgente(),
			"denunciado", d.DenunciadoID)
	}
	s.avisar(in.DenuncianteID, notify.SucesoDenunciaRecibida, nil)
	return d, nil
}

// hanCompartidoViaje dice si esas dos personas llegaron a ir en el mismo coche.
//
// Vale una reserva confirmada en cualquiera de los dos sentidos: quien organizó
// puede denunciar a quien llevó, y al revés.
func (s *Service) hanCompartidoViaje(unoID, otroID string) (bool, error) {
	deUno, err := s.store.BookingsByPassenger(unoID)
	if err != nil {
		return false, err
	}
	for _, b := range deUno {
		if b.Status != domain.BookingConfirmed {
			continue
		}
		t, err := s.store.GetTrip(b.TripID)
		if err != nil {
			continue
		}
		if t.HostID == otroID {
			return true, nil
		}
	}

	// Al revés: el otro fue pasajero de un trayecto de este.
	mios, err := s.store.TripsByHost(unoID)
	if err != nil {
		return false, err
	}
	for _, t := range mios {
		reservas, err := s.store.BookingsByTrip(t.ID)
		if err != nil {
			continue
		}
		for _, b := range reservas {
			if b.Status == domain.BookingConfirmed && b.PassengerID == otroID {
				return true, nil
			}
		}
	}
	return false, nil
}

// MisDenuncias devuelve las que ha puesto esa persona.
//
// Solo las suyas: nadie puede consultar las que hay contra él. Una denuncia que
// llega a oídos del denunciado es una denuncia que nadie pone.
func (s *Service) MisDenuncias(userID string) ([]*domain.Denuncia, error) {
	return s.store.DenunciasDe(userID)
}

// DenunciasPendientes es la cola de revisión de operaciones.
func (s *Service) DenunciasPendientes() ([]*domain.Denuncia, error) {
	return s.store.DenunciasAbiertas()
}

// ResolucionInput es lo que decide quien revisa una denuncia.
type ResolucionInput struct {
	DenunciaID string
	// Confirmada distingue darle la razón de desestimarla.
	Confirmada bool
	// Resolucion es lo que se le cuenta a quien denunció, y a quien queda
	// suspendido. Una decisión sin motivo no se puede recurrir.
	Resolucion string
	// Suspension es cuánto se aparta de compartir viajes a quien fue
	// denunciado. Cero aplica la duración por defecto; solo se usa si la
	// denuncia se confirma.
	Suspension time.Duration
	// Permanente cierra la puerta para siempre. Para lo que no admite segunda
	// oportunidad: violencia o suplantación de identidad.
	Permanente bool
}

// ResolverDenuncia cierra una denuncia y aplica su consecuencia.
//
// La consecuencia es lo que hace que denunciar sirva para algo: un sistema de
// denuncias que solo archiva es un buzón de quejas, y la gente deja de usarlo
// en cuanto se da cuenta.
func (s *Service) ResolverDenuncia(in ResolucionInput) (*domain.Denuncia, error) {
	d, err := s.store.GetDenuncia(in.DenunciaID)
	if err != nil {
		return nil, err
	}
	if d.Estado != domain.DenunciaAbierta {
		return nil, ErrDenunciaResuelta
	}

	now := s.cfg.Now()
	d.Estado = domain.DenunciaDesestimada
	if in.Confirmada {
		d.Estado = domain.DenunciaConfirmada
	}
	d.ResueltaAt = now
	d.Resolucion = domain.NormalizarComentario(in.Resolucion)
	if err := s.store.UpdateDenuncia(d); err != nil {
		return nil, err
	}

	if in.Confirmada {
		if err := s.suspender(d, in, now); err != nil {
			return nil, err
		}
	}

	resultado := "desestimada"
	if in.Confirmada {
		resultado = "confirmada"
	}
	s.avisar(d.DenuncianteID, notify.SucesoDenunciaResuelta, map[string]string{
		"resultado":  resultado,
		"resolucion": d.Resolucion,
	})
	return d, nil
}

// suspender aparta de compartir viajes a quien fue denunciado.
func (s *Service) suspender(d *domain.Denuncia, in ResolucionInput, now time.Time) error {
	hasta := now.Add(domain.SuspensionPorDenuncia)
	if in.Suspension > 0 {
		hasta = now.Add(in.Suspension)
	}
	if in.Permanente {
		hasta = domain.SuspensionPermanente
	}
	if err := s.store.SuspenderUsuario(d.DenunciadoID, &hasta); err != nil {
		return err
	}

	// Se le dice por qué y hasta cuándo. Una suspensión sin explicación no se
	// puede recurrir, y no poder recurrir la convierte en un error definitivo
	// cada vez que nos equivocamos.
	s.avisar(d.DenunciadoID, notify.SucesoCuentaSuspendida, map[string]string{
		"hasta":      hasta.Format("02/01/2006"),
		"resolucion": d.Resolucion,
	})
	return nil
}

// LevantarSuspension devuelve a alguien al servicio.
func (s *Service) LevantarSuspension(userID string) error {
	return s.store.SuspenderUsuario(userID, nil)
}

// comprobarNoSuspendido corta las acciones que meten a alguien en un coche.
//
// No corta el acceso a la cuenta: quien está suspendido sigue teniendo saldos
// que liquidar e incidencias que responder, y dejarle fuera de todo solo
// consigue que no responda de nada.
func (s *Service) comprobarNoSuspendido(userID string) error {
	u, err := s.store.GetUser(userID)
	if err != nil {
		return err
	}
	if !u.Suspendido(s.cfg.Now()) {
		return nil
	}
	return fmt.Errorf("%w (hasta el %s)", ErrSuspendido, u.SuspendidoHasta.Format("02/01/2006"))
}
