package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// Perfil es lo que se sabe de la fiabilidad de una persona.
type Perfil struct {
	UserID string      `json:"user_id"`
	Nombre string      `json:"nombre"`
	Nivel  trust.Level `json:"nivel"`
	// Verificaciones son las comprobaciones vigentes, por su nombre. Nunca
	// contiene datos del documento: solo qué se ha acreditado.
	Verificaciones []string    `json:"verificaciones"`
	Stats          trust.Stats `json:"estadisticas"`
	MiembroDesde   time.Time   `json:"miembro_desde"`
}

// PerfilDe reúne el nivel de confianza de una persona y lo que lo sostiene.
func (s *Service) PerfilDe(userID string) (*Perfil, error) {
	u, err := s.store.GetUser(userID)
	if err != nil {
		return nil, err
	}
	checks, err := s.store.ChecksByUser(userID)
	if err != nil {
		return nil, err
	}

	now := s.cfg.Now()
	stats, err := s.estadisticasDe(u)
	if err != nil {
		return nil, err
	}

	vigentes := make([]string, 0, len(checks))
	for _, c := range checks {
		if c.Active(now) {
			vigentes = append(vigentes, c.Kind.Label())
		}
	}

	return &Perfil{
		UserID:         u.ID,
		Nombre:         u.Name,
		Nivel:          trust.LevelOf(checks, stats, now),
		Verificaciones: vigentes,
		Stats:          stats,
		MiembroDesde:   u.CreatedAt,
	}, nil
}

// NivelDe calcula el nivel de confianza de una persona.
func (s *Service) NivelDe(userID string) (trust.Level, error) {
	p, err := s.PerfilDe(userID)
	if err != nil {
		return trust.LevelNuevo, err
	}
	return p.Nivel, nil
}

// IniciarVerificacion abre una comprobación de identidad y devuelve adónde hay
// que mandar a la persona para completarla.
func (s *Service) IniciarVerificacion(ctx context.Context, userID string, kind trust.CheckKind) (*trust.Session, *trust.Check, error) {
	if !kind.Valid() {
		return nil, nil, fmt.Errorf("%w: %s", domain.ErrValidation, trust.ErrTipoDesconocido)
	}
	if _, err := s.store.GetUser(userID); err != nil {
		return nil, nil, err
	}
	// El buzón lo comprobamos nosotros, con un código. Ver internal/service/correo.go.
	if kind == trust.CheckEmail {
		return s.iniciarCorreo(userID)
	}
	if s.cfg.Identidad == nil {
		return nil, nil, errors.New("el servicio no tiene configurado un proveedor de identidad")
	}

	sess, err := s.cfg.Identidad.Start(ctx, userID, kind)
	if err != nil {
		return nil, nil, fmt.Errorf("abriendo la verificación: %w", err)
	}

	check := &trust.Check{
		ID:          newID("chk"),
		UserID:      userID,
		Kind:        kind,
		Status:      trust.StatusPending,
		ProviderRef: sess.Ref,
		CreatedAt:   s.cfg.Now(),
	}
	if err := s.store.CreateCheck(check); err != nil {
		return nil, nil, err
	}
	return sess, check, nil
}

// RefrescarVerificacion consulta el veredicto del proveedor y lo aplica.
//
// Se llama tanto desde el webhook del proveedor como a petición de la persona,
// para no depender de que el webhook llegue.
func (s *Service) RefrescarVerificacion(ctx context.Context, providerRef string) (*trust.Check, error) {
	if s.cfg.Identidad == nil {
		return nil, errors.New("el servicio no tiene configurado un proveedor de identidad")
	}
	check, err := s.store.GetCheckByRef(providerRef)
	if err != nil {
		return nil, err
	}
	if check.Status != trust.StatusPending {
		// Ya resuelta: no se reabre. Aceptar un segundo veredicto permitiría
		// pisar un rechazo con una respuesta posterior.
		return check, nil
	}
	if check.Kind == trust.CheckEmail {
		// El proveedor no sabe nada de esta: la resuelve el código, no él.
		return check, nil
	}

	outcome, err := s.cfg.Identidad.Result(ctx, providerRef)
	if err != nil {
		return nil, fmt.Errorf("consultando el veredicto: %w", err)
	}
	if outcome.Status == trust.StatusPending {
		return check, nil
	}

	if err := s.aplicarVeredicto(check, outcome); err != nil {
		return nil, err
	}
	return check, nil
}

// aplicarVeredicto escribe el resultado en la comprobación y en las demás que
// ese mismo trámite acredite.
//
// Un mismo trámite puede resolver varias: los proveedores comprueban el
// documento y la cara en un solo paso, y hacer repetirlo sería absurdo.
func (s *Service) aplicarVeredicto(check *trust.Check, outcome *trust.Outcome) error {
	// El nivel de antes, para poder avisar solo cuando de verdad cambia.
	// Avisar por cada comprobación mandaba cuatro correos idénticos a quien
	// acababa de verificarse: eso no es informar, es hacer spam.
	nivelAntes, _ := s.NivelDe(check.UserID)

	aplicar := func(c *trust.Check) error {
		c.Status = outcome.Status
		c.RejectionReason = outcome.Reason
		if outcome.Status == trust.StatusVerified {
			c.VerifiedAt = s.cfg.Now()
			c.ExpiresAt = outcome.DocumentExpiresAt
		}
		return s.store.UpdateCheck(c)
	}
	if err := aplicar(check); err != nil {
		return err
	}

	if outcome.Status == trust.StatusRejected {
		s.avisar(check.UserID, notify.SucesoIdentidadRechazada, nil)
	}

	if outcome.Status != trust.StatusVerified {
		return nil
	}
	if len(outcome.Cubre) == 0 {
		s.avisarSiSubeDeNivel(check.UserID, nivelAntes)
		return nil
	}

	// Las demás comprobaciones que cubre este trámite. Solo se tocan las que
	// siguen pendientes: un rechazo anterior no se pisa.
	otras, err := s.store.ChecksByUser(check.UserID)
	if err != nil {
		return err
	}
	for i := range otras {
		c := otras[i]
		if c.ID == check.ID || c.Status != trust.StatusPending || !outcome.Acredita(c.Kind) {
			continue
		}
		if err := aplicar(&c); err != nil {
			return err
		}
	}

	s.avisarSiSubeDeNivel(check.UserID, nivelAntes)
	return nil
}

// avisarSiSubeDeNivel manda el correo solo cuando la persona cruza a
// verificado, no cada vez que supera una comprobación suelta.
func (s *Service) avisarSiSubeDeNivel(userID string, antes trust.Level) {
	ahora, err := s.NivelDe(userID)
	if err != nil {
		return
	}
	if antes < trust.LevelVerificado && ahora >= trust.LevelVerificado {
		s.avisar(userID, notify.SucesoIdentidadVerificada, nil)
	}
}

// AplicarAvisoDeIdentidad procesa el aviso que envía el proveedor cuando
// resuelve una verificación.
//
// Es el camino normal; RefrescarVerificacion existe para reconciliar cuando el
// aviso no llega. Los dos acaban en el mismo sitio, y ninguno reabre algo ya
// resuelto.
func (s *Service) AplicarAvisoDeIdentidad(ref string, outcome *trust.Outcome) (*trust.Check, error) {
	check, err := s.store.GetCheckByRef(ref)
	if err != nil {
		return nil, err
	}
	if check.Status != trust.StatusPending || outcome.Status == trust.StatusPending {
		return check, nil
	}
	if err := s.aplicarVeredicto(check, outcome); err != nil {
		return nil, err
	}
	return check, nil
}

// VerificacionesDe lista las comprobaciones de una persona. Solo para uso
// propio: son datos sensibles y no se enseñan en el perfil público.
func (s *Service) VerificacionesDe(userID string) ([]trust.Check, error) {
	return s.store.ChecksByUser(userID)
}

// --- Bloqueos ---

// Bloquear impide todo contacto entre dos personas en ambos sentidos.
func (s *Service) Bloquear(actorID, otroID string) error {
	if actorID == otroID {
		return fmt.Errorf("%w: no puedes bloquearte a ti mismo", domain.ErrValidation)
	}
	if _, err := s.store.GetUser(otroID); err != nil {
		return err
	}
	return s.store.CreateBlock(actorID, otroID)
}

// Desbloquear retira un bloqueo.
func (s *Service) Desbloquear(actorID, otroID string) error {
	return s.store.DeleteBlock(actorID, otroID)
}

// bloqueados devuelve con quién no puede coincidir esta persona.
func (s *Service) bloqueados(userID string) (map[string]bool, error) {
	if userID == "" {
		return map[string]bool{}, nil
	}
	return s.store.BlockedPairs(userID)
}

// --- Aplicación de los requisitos ---

// ErrBloqueado se devuelve cuando dos personas se han bloqueado.
var ErrBloqueado = errors.New("no puedes compartir trayecto con esta persona")

// RequisitoNoCumplido explica por qué alguien no puede subirse, para poder
// decírselo en vez de limitarse a negarle el acceso.
type RequisitoNoCumplido struct {
	Exigido  trust.Level       `json:"exigido"`
	Actual   trust.Level       `json:"actual"`
	TeFaltan []trust.CheckKind `json:"te_faltan"`
	Motivo   string            `json:"motivo"`
}

func (e *RequisitoNoCumplido) Error() string {
	return fmt.Sprintf("%s: el trayecto exige nivel %s y tienes %s",
		trust.ErrConfianzaInsuficiente, e.Exigido, e.Actual)
}

// Unwrap permite tratarlo con errors.Is como una falta de confianza.
func (e *RequisitoNoCumplido) Unwrap() error { return trust.ErrConfianzaInsuficiente }

// comprobarConfianza verifica que alguien puede subirse a un trayecto.
func (s *Service) comprobarConfianza(t *domain.Trip, pasajeroID string) error {
	bloqueos, err := s.bloqueados(pasajeroID)
	if err != nil {
		return err
	}
	if bloqueos[t.HostID] {
		return ErrBloqueado
	}

	exigido := t.NivelExigido()

	// El requisito corre en las dos direcciones: quien organiza tiene que
	// alcanzar el mismo nivel que exige. En un biplaza eso significa que
	// ambas partes han acreditado su identidad, que es justo lo que hace
	// aceptable un cara a cara sin testigos.
	for _, id := range []string{pasajeroID, t.HostID} {
		checks, err := s.store.ChecksByUser(id)
		if err != nil {
			return err
		}
		u, err := s.store.GetUser(id)
		if err != nil {
			return err
		}
		stats, err := s.estadisticasDe(u)
		if err != nil {
			return err
		}
		nivel := trust.LevelOf(checks, stats, s.cfg.Now())

		if nivel < exigido {
			motivo := trust.ExplicarSuelo(t.AforoTotal())
			if id != pasajeroID {
				motivo = "Quien organiza este trayecto todavía no alcanza el nivel que el propio trayecto exige."
			}
			return &RequisitoNoCumplido{
				Exigido:  exigido,
				Actual:   nivel,
				TeFaltan: trust.Missing(checks, exigido, s.cfg.Now()),
				Motivo:   motivo,
			}
		}
	}
	return nil
}

// filtrarBloqueados quita de una lista de trayectos los de personas con las que
// no se puede coincidir.
func filtrarBloqueados(trips []*domain.Trip, bloqueos map[string]bool) []*domain.Trip {
	if len(bloqueos) == 0 {
		return trips
	}
	out := make([]*domain.Trip, 0, len(trips))
	for _, t := range trips {
		if !bloqueos[t.HostID] {
			out = append(out, t)
		}
	}
	return out
}

var _ = store.ErrNotFound // el paquete se usa desde los otros ficheros del servicio
