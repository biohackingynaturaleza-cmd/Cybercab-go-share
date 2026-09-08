package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/auth"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
)

// testigoDelCorreo saca el enlace del último aviso y devuelve solo el secreto,
// que es lo que la interfaz le pasa a la API.
func testigoDelCorreo(t *testing.T, avisos *notify.Grabador) string {
	t.Helper()
	a, ok := avisos.Ultimo(notify.SucesoRecuperacionPedida)
	if !ok {
		t.Fatal("no se envió el correo de recuperación")
	}
	_, testigo, found := strings.Cut(a.Datos["enlace"], "#recuperar=")
	if !found || testigo == "" {
		t.Fatalf("el enlace no lleva testigo: %q", a.Datos["enlace"])
	}
	return testigo
}

func conCuenta(t *testing.T) (*Service, *notify.Grabador, string) {
	t.Helper()
	svc, _, avisos := newTestServiceConAvisos(t)
	sess, err := svc.Register(RegisterInput{Name: "Ana", Email: "ana@ejemplo.test", Password: "contraseña-larga", Idioma: "es", AceptaTerminos: true})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return svc, avisos, sess.User.ID
}

func TestRecuperarContrasenaDejaEntrarConLaNueva(t *testing.T) {
	svc, avisos, _ := conCuenta(t)

	if err := svc.SolicitarRecuperacion("ana@ejemplo.test"); err != nil {
		t.Fatalf("SolicitarRecuperacion: %v", err)
	}
	testigo := testigoDelCorreo(t, avisos)

	if err := svc.CambiarContrasenaConTestigo(testigo, "otra-contraseña"); err != nil {
		t.Fatalf("CambiarContrasenaConTestigo: %v", err)
	}
	if _, err := svc.Login("ana@ejemplo.test", "otra-contraseña"); err != nil {
		t.Fatalf("la contraseña nueva no entra: %v", err)
	}
	if _, err := svc.Login("ana@ejemplo.test", "contraseña-larga"); err == nil {
		t.Fatal("la contraseña vieja sigue entrando")
	}
}

func TestElEnlaceDeRecuperacionSirveUnaSolaVez(t *testing.T) {
	svc, avisos, _ := conCuenta(t)
	if err := svc.SolicitarRecuperacion("ana@ejemplo.test"); err != nil {
		t.Fatalf("SolicitarRecuperacion: %v", err)
	}
	testigo := testigoDelCorreo(t, avisos)

	if err := svc.CambiarContrasenaConTestigo(testigo, "otra-contraseña"); err != nil {
		t.Fatalf("primer uso: %v", err)
	}
	err := svc.CambiarContrasenaConTestigo(testigo, "tercera-contraseña")
	if !errors.Is(err, ErrRecuperacionInvalida) {
		t.Fatalf("segundo uso = %v, esperaba ErrRecuperacionInvalida", err)
	}
	// Y la cuenta se quedó con la del primer uso, no con la del segundo.
	if _, err := svc.Login("ana@ejemplo.test", "otra-contraseña"); err != nil {
		t.Fatalf("la contraseña del primer cambio no entra: %v", err)
	}
}

func TestCambiarLaContrasenaAnulaLosDemasEnlaces(t *testing.T) {
	// Alguien pide el enlace dos veces —porque el primer correo tardó—. Al usar
	// uno, el otro tiene que morir: si no, un enlace filtrado sigue abriendo la
	// cuenta después de que su dueño ya la haya recuperado.
	svc, avisos, _ := conCuenta(t)
	if err := svc.SolicitarRecuperacion("ana@ejemplo.test"); err != nil {
		t.Fatalf("primera petición: %v", err)
	}
	primero := testigoDelCorreo(t, avisos)
	if err := svc.SolicitarRecuperacion("ana@ejemplo.test"); err != nil {
		t.Fatalf("segunda petición: %v", err)
	}
	segundo := testigoDelCorreo(t, avisos)
	if primero == segundo {
		t.Fatal("dos peticiones devolvieron el mismo testigo")
	}

	if err := svc.CambiarContrasenaConTestigo(segundo, "otra-contraseña"); err != nil {
		t.Fatalf("usar el segundo: %v", err)
	}
	if err := svc.CambiarContrasenaConTestigo(primero, "tercera-contraseña"); !errors.Is(err, ErrRecuperacionInvalida) {
		t.Fatalf("el enlace anterior = %v, esperaba ErrRecuperacionInvalida", err)
	}
}

func TestPedirRecuperacionDeUnCorreoDesconocidoNoDelataNada(t *testing.T) {
	svc, avisos, _ := conCuenta(t)

	if err := svc.SolicitarRecuperacion("nadie@ejemplo.test"); err != nil {
		t.Fatalf("un correo desconocido no puede dar error: %v", err)
	}
	if _, ok := avisos.Ultimo(notify.SucesoRecuperacionPedida); ok {
		t.Fatal("se mandó correo a una dirección sin cuenta")
	}
}

func TestUnTestigoInventadoNoCambiaNada(t *testing.T) {
	svc, _, _ := conCuenta(t)
	for _, testigo := range []string{"", "   ", "no-es-un-testigo"} {
		if err := svc.CambiarContrasenaConTestigo(testigo, "otra-contraseña"); !errors.Is(err, ErrRecuperacionInvalida) {
			t.Fatalf("testigo %q = %v, esperaba ErrRecuperacionInvalida", testigo, err)
		}
	}
	if _, err := svc.Login("ana@ejemplo.test", "contraseña-larga"); err != nil {
		t.Fatalf("la contraseña original dejó de valer: %v", err)
	}
}

func TestUnaContrasenaCortaNoQuemaElEnlace(t *testing.T) {
	// Si validar la contraseña fuese lo último, un error de tecleo dejaría a esa
	// persona fuera hasta pedir otro correo.
	svc, avisos, _ := conCuenta(t)
	if err := svc.SolicitarRecuperacion("ana@ejemplo.test"); err != nil {
		t.Fatalf("SolicitarRecuperacion: %v", err)
	}
	testigo := testigoDelCorreo(t, avisos)

	if err := svc.CambiarContrasenaConTestigo(testigo, "corta"); err == nil {
		t.Fatalf("una contraseña de %d caracteres debería fallar", len("corta"))
	}
	if err := svc.CambiarContrasenaConTestigo(testigo, "ya-si-que-es-larga"); err != nil {
		t.Fatalf("el enlace se quemó con el intento fallido: %v", err)
	}
}

func TestCambiarLaContrasenaAvisaASuDueno(t *testing.T) {
	svc, avisos, _ := conCuenta(t)
	if err := svc.SolicitarRecuperacion("ana@ejemplo.test"); err != nil {
		t.Fatalf("SolicitarRecuperacion: %v", err)
	}
	if err := svc.CambiarContrasenaConTestigo(testigoDelCorreo(t, avisos), "otra-contraseña"); err != nil {
		t.Fatalf("CambiarContrasenaConTestigo: %v", err)
	}

	a, ok := avisos.Ultimo(notify.SucesoContrasenaCambiada)
	if !ok {
		t.Fatal("nadie avisó de que la contraseña cambió")
	}
	if a.Para != "ana@ejemplo.test" {
		t.Fatalf("aviso a %q", a.Para)
	}
	asunto, cuerpo := notify.Componer(a)
	if asunto == "" || cuerpo == "" {
		t.Fatal("el aviso de contraseña cambiada no tiene plantilla")
	}
}

func TestElCorreoDeRecuperacionLlegaEnSuIdiomaYConEnlaceUtil(t *testing.T) {
	svc, avisos, _ := conCuenta(t)
	if err := svc.SolicitarRecuperacion("ana@ejemplo.test"); err != nil {
		t.Fatalf("SolicitarRecuperacion: %v", err)
	}
	a, _ := avisos.Ultimo(notify.SucesoRecuperacionPedida)
	if a.Idioma != "es" {
		t.Fatalf("idioma = %q, esperaba es", a.Idioma)
	}
	asunto, cuerpo := notify.Componer(a)
	if !strings.Contains(cuerpo, a.Datos["enlace"]) {
		t.Fatalf("el cuerpo no lleva el enlace:\n%s", cuerpo)
	}
	if strings.Contains(asunto+cuerpo, "{") {
		t.Fatalf("quedaron huecos sin rellenar:\n%s\n%s", asunto, cuerpo)
	}
}

func TestElTestigoNoSeGuardaEnClaro(t *testing.T) {
	// Quien lea la tabla no puede entrar en ninguna cuenta con lo que hay en
	// ella, igual que con la columna de contraseñas.
	svc, avisos, userID := conCuenta(t)
	if err := svc.SolicitarRecuperacion("ana@ejemplo.test"); err != nil {
		t.Fatalf("SolicitarRecuperacion: %v", err)
	}
	testigo := testigoDelCorreo(t, avisos)

	guardada, err := svc.store.RecuperacionPorHash(hashDeTestigo(testigo))
	if err != nil {
		t.Fatalf("RecuperacionPorHash: %v", err)
	}
	if guardada.UserID != userID {
		t.Fatalf("la recuperación es de %q, esperaba %q", guardada.UserID, userID)
	}
	if guardada.TokenHash == testigo {
		t.Fatal("el testigo está guardado en claro")
	}
	if _, err := svc.store.RecuperacionPorHash(testigo); err == nil {
		t.Fatal("se puede buscar por el testigo sin hashear")
	}
}

func TestLaContrasenaNuevaSeGuardaHasheada(t *testing.T) {
	svc, avisos, userID := conCuenta(t)
	if err := svc.SolicitarRecuperacion("ana@ejemplo.test"); err != nil {
		t.Fatalf("SolicitarRecuperacion: %v", err)
	}
	if err := svc.CambiarContrasenaConTestigo(testigoDelCorreo(t, avisos), "otra-contraseña"); err != nil {
		t.Fatalf("CambiarContrasenaConTestigo: %v", err)
	}

	u, err := svc.store.GetUser(userID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.PasswordHash == "otra-contraseña" {
		t.Fatal("la contraseña quedó en claro")
	}
	if err := auth.CheckPassword(u.PasswordHash, "otra-contraseña"); err != nil {
		t.Fatalf("el hash guardado no corresponde a la contraseña nueva: %v", err)
	}
}

func TestUnEnlaceCaducadoNoSirve(t *testing.T) {
	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	tokens, err := auth.NewTokenIssuer(secret, time.Hour)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	reloj := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	avisos := &notify.Grabador{}
	svc := New(store.NewMemory(), Config{
		Tokens: tokens, Avisos: avisos, PublicURL: "https://app.ejemplo.test",
		Now: func() time.Time { return reloj },
	})
	if _, err := svc.Register(RegisterInput{Name: "Ana", Email: "ana@ejemplo.test", Password: "contraseña-larga", Idioma: "es", AceptaTerminos: true}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := svc.SolicitarRecuperacion("ana@ejemplo.test"); err != nil {
		t.Fatalf("SolicitarRecuperacion: %v", err)
	}
	testigo := testigoDelCorreo(t, avisos)

	reloj = reloj.Add(domain.VigenciaRecuperacion + time.Minute)
	if err := svc.CambiarContrasenaConTestigo(testigo, "otra-contraseña"); !errors.Is(err, ErrRecuperacionInvalida) {
		t.Fatalf("enlace caducado = %v, esperaba ErrRecuperacionInvalida", err)
	}
	if _, err := svc.Login("ana@ejemplo.test", "contraseña-larga"); err != nil {
		t.Fatalf("la contraseña original dejó de valer: %v", err)
	}
}
