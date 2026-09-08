package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/auth"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
)

// ErrRecuperacionInvalida se devuelve cuando el enlace no existe, ya se usó o
// ha caducado. Los tres casos dan el mismo error a propósito: distinguirlos por
// escrito solo sirve para que alguien afine su siguiente intento.
var ErrRecuperacionInvalida = errors.New("el enlace no sirve: pide otro")

// SolicitarRecuperacion manda un enlace para cambiar la contraseña.
//
// Responde igual exista o no ese correo. La alternativa —decir "no hay nadie
// con ese email"— convierte el formulario en un buscador de quién está
// registrado, y en una app donde la gente se sube al mismo coche eso es un dato
// que no le corresponde a nadie.
func (s *Service) SolicitarRecuperacion(email string) error {
	u, err := s.store.GetUserByEmail(email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}

	testigo, hash, err := nuevoTestigo()
	if err != nil {
		return err
	}
	now := s.cfg.Now()
	r := &domain.Recuperacion{
		ID:        newID("rec"),
		UserID:    u.ID,
		TokenHash: hash,
		CreatedAt: now,
		ExpiraAt:  now.Add(domain.VigenciaRecuperacion),
	}
	if err := s.store.CrearRecuperacion(r); err != nil {
		return err
	}

	s.avisar(u.ID, notify.SucesoRecuperacionPedida, map[string]string{
		"enlace":  s.enlaceDeRecuperacion(testigo),
		"minutos": fmt.Sprintf("%d", int(domain.VigenciaRecuperacion.Minutes())),
	})
	return nil
}

// CambiarContrasenaConTestigo gasta el enlace y pone la contraseña nueva.
func (s *Service) CambiarContrasenaConTestigo(testigo, nueva string) error {
	testigo = strings.TrimSpace(testigo)
	if testigo == "" {
		return ErrRecuperacionInvalida
	}
	// Se valida la contraseña antes de gastar el enlace: si no, una contraseña
	// demasiado corta quemaría el único enlace que esa persona tiene.
	hashClave, err := auth.HashPassword(nueva)
	if err != nil {
		return fmt.Errorf("%w: %s", domain.ErrValidation, err)
	}

	r, err := s.store.RecuperacionPorHash(hashDeTestigo(testigo))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrRecuperacionInvalida
		}
		return err
	}
	now := s.cfg.Now()
	if !r.Vigente(now) {
		return ErrRecuperacionInvalida
	}
	if err := s.store.UsarRecuperacion(r.ID, now); err != nil {
		if errors.Is(err, store.ErrRecuperacionUsada) || errors.Is(err, store.ErrNotFound) {
			return ErrRecuperacionInvalida
		}
		return err
	}
	if err := s.store.CambiarContrasena(r.UserID, hashClave); err != nil {
		return err
	}
	// Los demás enlaces vivos dejan de servir: si alguien pidió varios, o si
	// quien entró no era el dueño, no queda ninguno abierto por ahí.
	if err := s.store.AnularRecuperaciones(r.UserID, now); err != nil {
		s.log("no se pudieron anular los enlaces de recuperación restantes", err)
	}

	// Este aviso es el que descubre el robo: si alguien cambia la contraseña de
	// una cuenta que no es suya, su dueño se entera en el acto.
	s.avisar(r.UserID, notify.SucesoContrasenaCambiada, nil)
	return nil
}

// enlaceDeRecuperacion compone la dirección que abre la app con el testigo.
func (s *Service) enlaceDeRecuperacion(testigo string) string {
	return strings.TrimSuffix(s.cfg.PublicURL, "/") + "/#recuperar=" + testigo
}

// nuevoTestigo genera el secreto del enlace y el hash que se guarda.
//
// 32 bytes de aleatoriedad criptográfica: adivinarlo no es una posibilidad que
// haya que sopesar, así que el enlace puede viajar por correo sin más.
func nuevoTestigo() (testigo, hash string, err error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	testigo = base64.RawURLEncoding.EncodeToString(b[:])
	return testigo, hashDeTestigo(testigo), nil
}

// hashDeTestigo es SHA-256 y no bcrypt a propósito: el testigo ya es aleatorio
// y largo, no hay diccionario contra el que defenderlo, y buscar por hash tiene
// que ser una consulta con índice, no un recorrido de toda la tabla.
func hashDeTestigo(testigo string) string {
	sum := sha256.Sum256([]byte(testigo))
	return hex.EncodeToString(sum[:])
}
