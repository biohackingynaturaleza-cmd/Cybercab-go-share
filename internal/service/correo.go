package service

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// ErrCodigoInvalido se devuelve cuando el código no es el que se mandó.
var ErrCodigoInvalido = errors.New("ese código no es correcto")

// ErrCodigoAgotado se devuelve cuando el código caducó o se gastaron los
// intentos. Es distinto de equivocarse: aquí no vale seguir probando, hay que
// pedir otro.
var ErrCodigoAgotado = errors.New("ese código ya no sirve: pide otro")

// CodigoNoValido lleva además cuántos intentos quedan, para poder decírselo a
// quien se equivoca en vez de dejarle a ciegas.
type CodigoNoValido struct {
	Restantes int
}

func (e *CodigoNoValido) Error() string {
	return fmt.Sprintf("%s (te quedan %d intentos)", ErrCodigoInvalido, e.Restantes)
}

// Unwrap permite tratarlo con errors.Is como un código incorrecto.
func (e *CodigoNoValido) Unwrap() error { return ErrCodigoInvalido }

// iniciarCorreo abre la comprobación del buzón y manda el código.
//
// No pasa por el proveedor de identidad: comprobar un correo es mandarle algo y
// ver si vuelve, y eso ya sabemos hacerlo. Pagarle a un proveedor de identidad
// por un trámite sin documento ni cámara sería gastar dinero en la parte fácil.
func (s *Service) iniciarCorreo(userID string) (*trust.Session, *trust.Check, error) {
	u, err := s.store.GetUser(userID)
	if err != nil {
		return nil, nil, err
	}

	// Si ya hay una comprobación de correo esperando, se reutiliza y se manda
	// otro código. Abrir una segunda dejaría comprobaciones huérfanas, y
	// devolver un error a quien vuelve a pulsar "verificar" —porque el primer
	// correo tardó— sería castigarle por algo razonable.
	check, err := s.comprobacionDeCorreoPendiente(userID)
	if err != nil {
		return nil, nil, err
	}
	now := s.cfg.Now()
	if check == nil {
		check = &trust.Check{
			ID:          newID("chk"),
			UserID:      userID,
			Kind:        trust.CheckEmail,
			Status:      trust.StatusPending,
			ProviderRef: newID("correo"),
			CreatedAt:   now,
		}
		// Si el buzón ya está acreditado, el almacén lo rechaza aquí: no hay
		// nada que volver a comprobar.
		if err := s.store.CreateCheck(check); err != nil {
			return nil, nil, err
		}
	}
	if err := s.mandarCodigo(u, check.ProviderRef); err != nil {
		return nil, nil, err
	}

	// No hay adónde mandar a nadie: el código se teclea en la propia app. Se
	// devuelve la dirección de la app para que quien consuma la API no tenga
	// que tratar este caso distinto del resto.
	return &trust.Session{
		Ref:         check.ProviderRef,
		RedirectURL: s.cfg.PublicURL,
		ExpiresAt:   now.Add(domain.VigenciaCodigoCorreo),
	}, check, nil
}

// ReenviarCodigoCorreo manda otro código para la comprobación en curso, o abre
// una si no la había.
func (s *Service) ReenviarCodigoCorreo(userID string) error {
	_, _, err := s.iniciarCorreo(userID)
	return err
}

// comprobacionDeCorreoPendiente busca una comprobación de correo sin resolver.
func (s *Service) comprobacionDeCorreoPendiente(userID string) (*trust.Check, error) {
	checks, err := s.store.ChecksByUser(userID)
	if err != nil {
		return nil, err
	}
	for i := range checks {
		if checks[i].Kind == trust.CheckEmail && checks[i].Status == trust.StatusPending {
			return &checks[i], nil
		}
	}
	return nil, nil
}

// mandarCodigo genera un código nuevo, guarda su hash y lo envía.
func (s *Service) mandarCodigo(u *domain.User, checkRef string) error {
	codigo, err := nuevoCodigo()
	if err != nil {
		return err
	}
	now := s.cfg.Now()

	// Los códigos anteriores mueren: si quedaran vivos, pedir otro multiplicaría
	// los intentos disponibles en vez de sustituir el código.
	if err := s.store.AnularCodigosCorreo(u.ID, now); err != nil {
		return err
	}
	if err := s.store.CrearCodigoCorreo(&domain.CodigoCorreo{
		ID:         newID("cod"),
		UserID:     u.ID,
		CheckRef:   checkRef,
		CodigoHash: hashDeCodigo(codigo),
		CreatedAt:  now,
		ExpiraAt:   now.Add(domain.VigenciaCodigoCorreo),
	}); err != nil {
		return err
	}

	s.avisar(u.ID, notify.SucesoCodigoCorreo, map[string]string{
		"codigo":  codigo,
		"minutos": fmt.Sprintf("%d", int(domain.VigenciaCodigoCorreo.Minutes())),
	})
	return nil
}

// ConfirmarCorreo acredita el buzón si el código es el que se mandó.
func (s *Service) ConfirmarCorreo(userID, codigo string) (*trust.Check, error) {
	codigo = strings.TrimSpace(codigo)
	guardado, err := s.store.CodigoCorreoVivo(userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrCodigoAgotado
		}
		return nil, err
	}
	now := s.cfg.Now()
	if !guardado.Vigente(now) {
		return nil, ErrCodigoAgotado
	}

	// El intento se anota antes de comparar. Al revés, una petición que se
	// corta a mitad —o mil peticiones a la vez— probarían gratis.
	intentos, err := s.store.AnotarIntentoCodigo(guardado.ID)
	if err != nil {
		return nil, err
	}
	if !mismoCodigo(guardado.CodigoHash, codigo) {
		if intentos >= domain.MaxIntentosCodigo {
			return nil, ErrCodigoAgotado
		}
		return nil, &CodigoNoValido{Restantes: domain.MaxIntentosCodigo - intentos}
	}

	if err := s.store.UsarCodigoCorreo(guardado.ID, now); err != nil {
		if errors.Is(err, store.ErrCodigoGastado) {
			return nil, ErrCodigoAgotado
		}
		return nil, err
	}

	check, err := s.store.GetCheckByRef(guardado.CheckRef)
	if err != nil {
		return nil, err
	}
	if check.Status != trust.StatusPending {
		return check, nil
	}
	// Cubre vacío: este trámite acredita el buzón y nada más. Un correo no
	// caduca, así que tampoco lleva fecha de caducidad.
	if err := s.aplicarVeredicto(check, &trust.Outcome{Status: trust.StatusVerified}); err != nil {
		return nil, err
	}
	return check, nil
}

// nuevoCodigo genera los seis dígitos.
//
// Con crypto/rand y no math/rand: un código adivinable no protege de nada, y la
// diferencia de coste entre uno y otro es ninguna.
func nuevoCodigo() (string, error) {
	tope := new(big.Int).Exp(big.NewInt(10), big.NewInt(domain.LongitudCodigoCorreo), nil)
	n, err := rand.Int(rand.Reader, tope)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", domain.LongitudCodigoCorreo, n), nil
}

func hashDeCodigo(codigo string) string {
	sum := sha256.Sum256([]byte(codigo))
	return hex.EncodeToString(sum[:])
}

// mismoCodigo compara en tiempo constante: si tardáramos más en fallar cuanto
// más acertáramos, el propio retardo iría revelando el código dígito a dígito.
func mismoCodigo(hashGuardado, codigo string) bool {
	return subtle.ConstantTimeCompare([]byte(hashGuardado), []byte(hashDeCodigo(codigo))) == 1
}
