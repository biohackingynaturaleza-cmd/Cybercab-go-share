package service

import (
	"errors"
	"testing"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
)

func TestNoHayCuentaSinAceptarLasCondiciones(t *testing.T) {
	svc, _, _ := newTestServiceConAvisos(t)
	_, err := svc.Register(RegisterInput{
		Name: "Ana", Email: "ana@ejemplo.test", Password: "contraseña-larga", Idioma: "es",
	})
	if !errors.Is(err, ErrTerminosNoAceptados) {
		t.Fatalf("Register sin aceptar = %v, esperaba ErrTerminosNoAceptados", err)
	}
	// Y no se ha quedado a medias: nadie puede entrar con esa cuenta.
	if _, err := svc.Login("ana@ejemplo.test", "contraseña-larga"); err == nil {
		t.Fatal("la cuenta se creó pese a rechazar el alta")
	}
}

func TestAlDarseDeAltaQuedaConstanciaDeQueVersionAcepto(t *testing.T) {
	svc, _, _ := newTestServiceConAvisos(t)
	sess, err := svc.Register(RegisterInput{
		Name: "Ana", Email: "ana@ejemplo.test", Password: "contraseña-larga",
		Idioma: "es", AceptaTerminos: true,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	u := sess.User
	if u.TerminosVersion != domain.VersionTerminos {
		t.Fatalf("versión aceptada = %q, esperaba %q", u.TerminosVersion, domain.VersionTerminos)
	}
	if u.TerminosAt == nil || u.TerminosAt.IsZero() {
		t.Fatal("no quedó constancia de cuándo se aceptó")
	}
	if !u.TerminosAlDia() {
		t.Fatal("quien acaba de aceptar debería estar al día")
	}
}

func TestSePuedeRenovarLaAceptacionCuandoCambiaLaRedaccion(t *testing.T) {
	// Sin esto, publicar unas condiciones nuevas dejaría a todo el mundo con
	// una aceptación caducada y sin forma de renovarla.
	svc, _, _ := newTestServiceConAvisos(t)
	sess, err := svc.Register(RegisterInput{
		Name: "Ana", Email: "ana@ejemplo.test", Password: "contraseña-larga",
		Idioma: "es", AceptaTerminos: true,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Simulamos una cuenta antigua, de antes de que existieran las condiciones.
	if err := svc.store.AceptarTerminos(sess.User.ID, "", svc.cfg.Now()); err != nil {
		t.Fatalf("AceptarTerminos: %v", err)
	}
	antigua, _ := svc.GetUser(sess.User.ID)
	if antigua.TerminosAlDia() {
		t.Fatal("una cuenta sin versión no puede estar al día")
	}

	u, err := svc.AceptarTerminos(sess.User.ID)
	if err != nil {
		t.Fatalf("AceptarTerminos: %v", err)
	}
	if !u.TerminosAlDia() {
		t.Fatalf("tras aceptar sigue en %q", u.TerminosVersion)
	}
}
