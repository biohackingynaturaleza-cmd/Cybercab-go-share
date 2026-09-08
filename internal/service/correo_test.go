package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// cuentaNueva da de alta a alguien y devuelve su id y el grabador de correos.
func cuentaNueva(t *testing.T) (*Service, *notify.Grabador, string) {
	t.Helper()
	svc, _, avisos := newTestServiceConAvisos(t)
	sess, err := svc.Register(RegisterInput{
		Name: "Ana", Email: "ana@ejemplo.test", Password: testPassword,
		Idioma: "es", AceptaTerminos: true,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return svc, avisos, sess.User.ID
}

func codigoDelCorreo(t *testing.T, avisos *notify.Grabador) string {
	t.Helper()
	a, ok := avisos.Ultimo(notify.SucesoCodigoCorreo)
	if !ok {
		t.Fatal("no se mandó ningún código")
	}
	codigo := a.Datos["codigo"]
	if len(codigo) != domain.LongitudCodigoCorreo {
		t.Fatalf("código = %q, esperaba %d dígitos", codigo, domain.LongitudCodigoCorreo)
	}
	return codigo
}

func TestDarseDeAltaMandaElCodigoDelBuzon(t *testing.T) {
	// El primer peldaño de la confianza empieza en el momento en que la
	// persona acaba de teclear su dirección y la tiene delante.
	svc, avisos, userID := cuentaNueva(t)

	a, ok := avisos.Ultimo(notify.SucesoCodigoCorreo)
	if !ok {
		t.Fatal("el alta no mandó código")
	}
	if a.Para != "ana@ejemplo.test" || a.Idioma != "es" {
		t.Fatalf("aviso = %+v", a)
	}
	asunto, cuerpo := notify.Componer(a)
	if !strings.Contains(cuerpo, a.Datos["codigo"]) {
		t.Fatalf("el correo no lleva el código:\n%s", cuerpo)
	}
	if strings.Contains(asunto+cuerpo, "{") {
		t.Fatalf("quedaron huecos sin rellenar:\n%s\n%s", asunto, cuerpo)
	}

	// Y queda la comprobación abierta, esperando el código.
	p, err := svc.PerfilDe(userID)
	if err != nil {
		t.Fatalf("PerfilDe: %v", err)
	}
	if p.Nivel != trust.LevelNuevo {
		t.Fatalf("nivel = %v: mandar el código no acredita nada por sí solo", p.Nivel)
	}
}

func TestElCodigoCorrectoAcreditaElBuzon(t *testing.T) {
	svc, avisos, userID := cuentaNueva(t)

	check, err := svc.ConfirmarCorreo(userID, codigoDelCorreo(t, avisos))
	if err != nil {
		t.Fatalf("ConfirmarCorreo: %v", err)
	}
	if check.Kind != trust.CheckEmail || check.Status != trust.StatusVerified {
		t.Fatalf("comprobación = %+v", check)
	}
	if check.VerifiedAt.IsZero() {
		t.Fatal("no quedó constancia de cuándo se acreditó")
	}
	// Un correo no caduca: no lleva fecha de caducidad.
	if !check.ExpiresAt.IsZero() {
		t.Fatalf("el buzón trae caducidad %v, y un correo no caduca", check.ExpiresAt)
	}

	p, err := svc.PerfilDe(userID)
	if err != nil {
		t.Fatalf("PerfilDe: %v", err)
	}
	if len(p.Verificaciones) != 1 || p.Verificaciones[0] != trust.CheckEmail.Label() {
		t.Fatalf("verificaciones = %v", p.Verificaciones)
	}
}

func TestUnCodigoEquivocadoGastaUnIntentoYLoDice(t *testing.T) {
	svc, avisos, userID := cuentaNueva(t)
	bueno := codigoDelCorreo(t, avisos)

	_, err := svc.ConfirmarCorreo(userID, "000000")
	var malo *CodigoNoValido
	if !errors.As(err, &malo) {
		t.Fatalf("err = %v, esperaba CodigoNoValido", err)
	}
	if malo.Restantes != domain.MaxIntentosCodigo-1 {
		t.Fatalf("intentos restantes = %d, esperaba %d", malo.Restantes, domain.MaxIntentosCodigo-1)
	}
	if !errors.Is(err, ErrCodigoInvalido) {
		t.Fatal("el error no se reconoce como código inválido")
	}

	// Equivocarse no invalida el bueno.
	if _, err := svc.ConfirmarCorreo(userID, bueno); err != nil {
		t.Fatalf("el código bueno dejó de valer tras un fallo: %v", err)
	}
}

func TestElCodigoSeAgotaAFuerzaDeIntentos(t *testing.T) {
	// Seis dígitos son un millón de combinaciones: sin tope se prueban en
	// minutos.
	svc, avisos, userID := cuentaNueva(t)
	bueno := codigoDelCorreo(t, avisos)

	for i := range domain.MaxIntentosCodigo {
		fallo := "00000" + string(rune('0'+i%10))
		if fallo == bueno {
			continue
		}
		svc.ConfirmarCorreo(userID, fallo) //nolint:errcheck // el efecto se comprueba después
	}

	if _, err := svc.ConfirmarCorreo(userID, bueno); !errors.Is(err, ErrCodigoAgotado) {
		t.Fatalf("tras agotar los intentos, el código bueno = %v, esperaba ErrCodigoAgotado", err)
	}
}

func TestUnCodigoCaducadoNoSirve(t *testing.T) {
	svc, avisos, userID := cuentaNueva(t)
	codigo := codigoDelCorreo(t, avisos)

	base := svc.cfg.Now()
	svc.cfg.Now = func() time.Time { return base.Add(domain.VigenciaCodigoCorreo + time.Minute) }

	if _, err := svc.ConfirmarCorreo(userID, codigo); !errors.Is(err, ErrCodigoAgotado) {
		t.Fatalf("código caducado = %v, esperaba ErrCodigoAgotado", err)
	}
}

func TestPedirOtroCodigoInvalidaElAnterior(t *testing.T) {
	// Si el anterior siguiera vivo, pedir códigos multiplicaría los intentos
	// disponibles en vez de sustituir el código.
	svc, avisos, userID := cuentaNueva(t)
	primero := codigoDelCorreo(t, avisos)

	if err := svc.ReenviarCodigoCorreo(userID); err != nil {
		t.Fatalf("ReenviarCodigoCorreo: %v", err)
	}
	segundo := codigoDelCorreo(t, avisos)
	if primero == segundo {
		t.Fatal("el reenvío mandó el mismo código")
	}

	if _, err := svc.ConfirmarCorreo(userID, primero); err == nil {
		t.Fatal("el código anterior sigue valiendo")
	}
	if _, err := svc.ConfirmarCorreo(userID, segundo); err != nil {
		t.Fatalf("el código nuevo no vale: %v", err)
	}
}

func TestReenviarNoAbreUnaComprobacionNueva(t *testing.T) {
	// Pedir el código otra vez —porque el primer correo tardó— no puede dejar
	// un rastro de comprobaciones abiertas que nadie va a cerrar.
	svc, _, userID := cuentaNueva(t)
	for range 3 {
		if err := svc.ReenviarCodigoCorreo(userID); err != nil {
			t.Fatalf("ReenviarCodigoCorreo: %v", err)
		}
	}

	checks, err := svc.store.ChecksByUser(userID)
	if err != nil {
		t.Fatalf("ChecksByUser: %v", err)
	}
	var correos int
	for _, c := range checks {
		if c.Kind == trust.CheckEmail {
			correos++
		}
	}
	if correos != 1 {
		t.Fatalf("comprobaciones de correo = %d, esperaba 1", correos)
	}
}

func TestElBuzonAcreditadoNoSeVuelveAComprobar(t *testing.T) {
	svc, avisos, userID := cuentaNueva(t)
	if _, err := svc.ConfirmarCorreo(userID, codigoDelCorreo(t, avisos)); err != nil {
		t.Fatalf("ConfirmarCorreo: %v", err)
	}
	if err := svc.ReenviarCodigoCorreo(userID); err == nil {
		t.Fatal("se abrió otra comprobación de un buzón ya acreditado")
	}
}

func TestElCodigoNoSeGuardaEnClaro(t *testing.T) {
	svc, avisos, userID := cuentaNueva(t)
	codigo := codigoDelCorreo(t, avisos)

	guardado, err := svc.store.CodigoCorreoVivo(userID)
	if err != nil {
		t.Fatalf("CodigoCorreoVivo: %v", err)
	}
	if guardado.CodigoHash == codigo {
		t.Fatal("el código está guardado en claro")
	}
	if !mismoCodigo(guardado.CodigoHash, codigo) {
		t.Fatal("el hash guardado no corresponde al código enviado")
	}
}

func TestElBuzonNoPasaPorElProveedorDeIdentidad(t *testing.T) {
	// Comprobar un correo es mandarle algo y ver si vuelve. Pagarle a un
	// proveedor de identidad por eso sería gastar en la parte fácil.
	svc, identidad, _ := newTestServiceConAvisos(t)
	sess, err := svc.Register(RegisterInput{
		Name: "Ana", Email: "ana@ejemplo.test", Password: testPassword,
		Idioma: "es", AceptaTerminos: true,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	abierta, _, err := svc.IniciarVerificacion(t.Context(), sess.User.ID, trust.CheckEmail)
	if err != nil {
		t.Fatalf("IniciarVerificacion: %v", err)
	}
	if _, err := identidad.Result(t.Context(), abierta.Ref); err == nil {
		t.Fatal("el proveedor de identidad conoce la comprobación del buzón")
	}
}

func TestSinCorreoAcreditadoNoSeLlegaANivelBasico(t *testing.T) {
	// La escalera empieza aquí: si el primer peldaño fuese de mentira, todo lo
	// que se apoya en él lo sería también.
	svc, identidad, avisos := newTestServiceConAvisos(t)
	sess, err := svc.Register(RegisterInput{
		Name: "Ana", Email: "ana@ejemplo.test", Password: testPassword,
		Idioma: "es", AceptaTerminos: true,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID := sess.User.ID

	// Teléfono acreditado, correo no.
	abierta, _, err := svc.IniciarVerificacion(t.Context(), userID, trust.CheckPhone)
	if err != nil {
		t.Fatalf("IniciarVerificacion: %v", err)
	}
	if err := identidad.Resolve(abierta.Ref, trust.Outcome{Status: trust.StatusVerified}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := svc.RefrescarVerificacion(t.Context(), abierta.Ref); err != nil {
		t.Fatalf("RefrescarVerificacion: %v", err)
	}

	nivel, err := svc.NivelDe(userID)
	if err != nil {
		t.Fatalf("NivelDe: %v", err)
	}
	if nivel != trust.LevelNuevo {
		t.Fatalf("nivel sin correo acreditado = %v, esperaba nuevo", nivel)
	}

	if _, err := svc.ConfirmarCorreo(userID, codigoDelCorreo(t, avisos)); err != nil {
		t.Fatalf("ConfirmarCorreo: %v", err)
	}
	nivel, err = svc.NivelDe(userID)
	if err != nil {
		t.Fatalf("NivelDe: %v", err)
	}
	if nivel != trust.LevelBasico {
		t.Fatalf("nivel con correo y teléfono = %v, esperaba básico", nivel)
	}
}
