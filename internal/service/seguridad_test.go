package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// viajeConfirmado deja un trayecto con una plaza confirmada, que es la
// situación en la que alguien va dentro de un coche con un desconocido.
func viajeConfirmado(t *testing.T) (escenario, *notify.Grabador) {
	t.Helper()
	svc, identidad, avisos := newTestServiceConAvisos(t)
	e := escenarioCon(t, svc, identidad, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, svc, identidad, e.pasajero.ID, trust.LevelVerificado)

	b, err := e.reservar()
	if err != nil {
		t.Fatalf("RequestBooking: %v", err)
	}
	if _, err := svc.DecideBooking(DecisionInput{
		BookingID: b.ID, HostID: e.host.ID, Accept: true, AceptaResponsabilidad: true,
	}); err != nil {
		t.Fatalf("DecideBooking: %v", err)
	}
	return e, avisos
}

func contactoDe(t *testing.T, svc *Service, userID, email string, avisar bool) *domain.ContactoDeConfianza {
	t.Helper()
	c, err := svc.AñadirContacto(ContactoInput{
		UserID: userID, Nombre: "Madre", Email: email, AvisarAlSalir: avisar,
	})
	if err != nil {
		t.Fatalf("AñadirContacto: %v", err)
	}
	return c
}

// --- Contactos ---

func TestNoSePuedenTenerMasDeTresContactos(t *testing.T) {
	// Una lista larga diluye la responsabilidad: cada uno supone que ya habrá
	// reaccionado otro.
	svc, _, _ := newTestServiceConAvisos(t)
	sess, err := svc.Register(RegisterInput{
		Name: "Ana", Email: "ana@ejemplo.test", Password: testPassword,
		Idioma: "es", AceptaTerminos: true,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	for i := range domain.MaxContactos {
		contactoDe(t, svc, sess.User.ID, string(rune('a'+i))+"@ejemplo.test", true)
	}
	_, err = svc.AñadirContacto(ContactoInput{
		UserID: sess.User.ID, Nombre: "Uno más", Email: "z@ejemplo.test",
	})
	if !errors.Is(err, ErrDemasiadosContactos) {
		t.Fatalf("err = %v, esperaba ErrDemasiadosContactos", err)
	}
}

func TestUnContactoDeConfianzaTieneQueSerOtraPersona(t *testing.T) {
	// Si el aviso llega a tu propio buzón, no lo va a leer nadie más que tú.
	svc, _, _ := newTestServiceConAvisos(t)
	sess, err := svc.Register(RegisterInput{
		Name: "Ana", Email: "ana@ejemplo.test", Password: testPassword,
		Idioma: "es", AceptaTerminos: true,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, err = svc.AñadirContacto(ContactoInput{
		UserID: sess.User.ID, Nombre: "Yo", Email: "ANA@ejemplo.test",
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, esperaba un fallo de validación", err)
	}
}

func TestNadieBorraElContactoDeOtro(t *testing.T) {
	e, _ := viajeConfirmado(t)
	c := contactoDe(t, e.svc, e.host.ID, "madre@ejemplo.test", true)

	if err := e.svc.BorrarContacto(c.ID, e.pasajero.ID); err == nil {
		t.Fatal("un tercero borró un contacto ajeno")
	}
	if cs, _ := e.svc.MisContactos(e.host.ID); len(cs) != 1 {
		t.Fatalf("contactos = %d, esperaba que siguiera ahí", len(cs))
	}
	if err := e.svc.BorrarContacto(c.ID, e.host.ID); err != nil {
		t.Fatalf("su dueño no pudo borrarlo: %v", err)
	}
}

// --- Seguimiento ---

func testigoDelEnlace(t *testing.T, url string) string {
	t.Helper()
	_, testigo, ok := strings.Cut(url, "?t=")
	if !ok || testigo == "" {
		t.Fatalf("el enlace no lleva testigo: %q", url)
	}
	return testigo
}

func TestCompartirElViajeEnsenaQuienVaDentro(t *testing.T) {
	e, _ := viajeConfirmado(t)

	enlace, err := e.svc.CompartirViaje(e.pasajero.ID, e.trip.ID)
	if err != nil {
		t.Fatalf("CompartirViaje: %v", err)
	}
	vista, err := e.svc.VerSeguimiento(testigoDelEnlace(t, enlace.URL))
	if err != nil {
		t.Fatalf("VerSeguimiento: %v", err)
	}

	if vista.Destino.Name != e.trip.Destination.Name || len(vista.Ruta) < 2 {
		t.Fatalf("vista = %+v", vista)
	}
	if len(vista.Ocupantes) != 2 {
		t.Fatalf("ocupantes = %d, esperaba a quien organiza y al pasajero", len(vista.Ocupantes))
	}
	if vista.Telefono != domain.TelefonoEmergencias {
		t.Fatalf("teléfono = %q", vista.Telefono)
	}
}

func TestElEnlaceSoloEnsenaElPrimerNombreDeLosDemas(t *testing.T) {
	// Quien comparte comparte su viaje, no la identidad completa de quien va a
	// su lado.
	e, _ := viajeConfirmado(t)
	enlace, err := e.svc.CompartirViaje(e.pasajero.ID, e.trip.ID)
	if err != nil {
		t.Fatalf("CompartirViaje: %v", err)
	}
	vista, err := e.svc.VerSeguimiento(testigoDelEnlace(t, enlace.URL))
	if err != nil {
		t.Fatalf("VerSeguimiento: %v", err)
	}
	for _, o := range vista.Ocupantes {
		if strings.Contains(o.Nombre, " ") {
			t.Fatalf("ocupante %q: se enseña el nombre completo", o.Nombre)
		}
	}
}

func TestSoloComparteElViajeQuienVaEnEl(t *testing.T) {
	e, _ := viajeConfirmado(t)
	extraño, err := e.svc.Register(RegisterInput{
		Name: "Eva", Email: "eva@ejemplo.test", Password: testPassword,
		Idioma: "es", AceptaTerminos: true,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := e.svc.CompartirViaje(extraño.User.ID, e.trip.ID); !errors.Is(err, ErrNoVasEnEsteViaje) {
		t.Fatalf("err = %v, esperaba ErrNoVasEnEsteViaje", err)
	}
}

func TestUnEnlaceRevocadoDejaDeEnsenarNada(t *testing.T) {
	e, _ := viajeConfirmado(t)
	enlace, err := e.svc.CompartirViaje(e.pasajero.ID, e.trip.ID)
	if err != nil {
		t.Fatalf("CompartirViaje: %v", err)
	}
	testigo := testigoDelEnlace(t, enlace.URL)

	if _, err := e.svc.DejarDeCompartir(e.pasajero.ID, e.trip.ID); err != nil {
		t.Fatalf("DejarDeCompartir: %v", err)
	}
	if _, err := e.svc.VerSeguimiento(testigo); !errors.Is(err, ErrSeguimientoInvalido) {
		t.Fatalf("err = %v, esperaba ErrSeguimientoInvalido", err)
	}
}

func TestElEnlaceCaducaSolo(t *testing.T) {
	e, _ := viajeConfirmado(t)
	enlace, err := e.svc.CompartirViaje(e.pasajero.ID, e.trip.ID)
	if err != nil {
		t.Fatalf("CompartirViaje: %v", err)
	}
	testigo := testigoDelEnlace(t, enlace.URL)

	e.svc.cfg.Now = func() time.Time { return enlace.Expira.Add(time.Minute) }
	if _, err := e.svc.VerSeguimiento(testigo); !errors.Is(err, ErrSeguimientoInvalido) {
		t.Fatalf("err = %v, esperaba que caducara solo", err)
	}
}

func TestVariosEnlacesDelMismoViajeConviven(t *testing.T) {
	// Compartir otra vez no puede matar al anterior: el testigo se guarda
	// hasheado, así que un enlace ya mandado no se puede volver a escribir en
	// un correo, y matarlo dejaría tirado a quien lo tenga. Es justo lo que
	// pasaría con el correo del botón de emergencia.
	e, _ := viajeConfirmado(t)
	primero, err := e.svc.CompartirViaje(e.pasajero.ID, e.trip.ID)
	if err != nil {
		t.Fatalf("primero: %v", err)
	}
	segundo, err := e.svc.CompartirViaje(e.pasajero.ID, e.trip.ID)
	if err != nil {
		t.Fatalf("segundo: %v", err)
	}

	for nombre, url := range map[string]string{"primero": primero.URL, "segundo": segundo.URL} {
		if _, err := e.svc.VerSeguimiento(testigoDelEnlace(t, url)); err != nil {
			t.Fatalf("el %s no vale: %v", nombre, err)
		}
	}

	// Y dejar de compartir los cierra todos: uno y otro no.
	n, err := e.svc.DejarDeCompartir(e.pasajero.ID, e.trip.ID)
	if err != nil {
		t.Fatalf("DejarDeCompartir: %v", err)
	}
	if n != 2 {
		t.Fatalf("cerrados = %d, esperaba los dos", n)
	}
	for nombre, url := range map[string]string{"primero": primero.URL, "segundo": segundo.URL} {
		if _, err := e.svc.VerSeguimiento(testigoDelEnlace(t, url)); !errors.Is(err, ErrSeguimientoInvalido) {
			t.Fatalf("el %s sigue abierto: %v", nombre, err)
		}
	}
}

func TestLaAlertaMandaUnEnlaceUtilizableAunqueYaHubieraOtro(t *testing.T) {
	// El fallo que esto evita: el correo del botón de emergencia llevaba a la
	// portada en vez de al seguimiento cuando ya existía un enlace, porque su
	// dirección no se puede reconstruir.
	e, avisos := viajeConfirmado(t)
	contactoDe(t, e.svc, e.pasajero.ID, "madre@ejemplo.test", false)
	viejo, err := e.svc.CompartirViaje(e.pasajero.ID, e.trip.ID)
	if err != nil {
		t.Fatalf("CompartirViaje: %v", err)
	}

	if _, err := e.svc.Emergencia(AlertaInput{UserID: e.pasajero.ID, TripID: e.trip.ID}); err != nil {
		t.Fatalf("Emergencia: %v", err)
	}
	aviso, _ := avisos.Ultimo(notify.SucesoAlertaDisparada)
	vista, err := e.svc.VerSeguimiento(testigoDelEnlace(t, aviso.Datos["enlace"]))
	if err != nil {
		t.Fatalf("el enlace del aviso no sirve: %v", err)
	}
	if vista.Alerta == nil {
		t.Fatal("el enlace del aviso no enseña la alerta")
	}
	// Y quien ya tenía el anterior lo sigue teniendo: es justo ahora cuando
	// más falta le hace.
	anterior, err := e.svc.VerSeguimiento(testigoDelEnlace(t, viejo.URL))
	if err != nil {
		t.Fatalf("el enlace anterior dejó de servir: %v", err)
	}
	if anterior.Alerta == nil {
		t.Fatal("el enlace anterior no enseña la alerta")
	}
}

func TestLaPosicionSoloSeGuardaSiHayEnlaceAbierto(t *testing.T) {
	// Guardar posiciones que nadie va a mirar es rastrear.
	e, _ := viajeConfirmado(t)
	punto := geo.Point{Lat: 30.2672, Lng: -97.7431}

	if err := e.svc.ApuntarPosicion(e.pasajero.ID, e.trip.ID, punto); err != nil {
		t.Fatalf("ApuntarPosicion sin enlace: %v", err)
	}
	if vivos, _ := e.svc.store.SeguimientosVivos(e.trip.ID, e.pasajero.ID); len(vivos) != 0 {
		t.Fatal("se guardó una posición sin enlace que la enseñe")
	}

	enlace, err := e.svc.CompartirViaje(e.pasajero.ID, e.trip.ID)
	if err != nil {
		t.Fatalf("CompartirViaje: %v", err)
	}
	if err := e.svc.ApuntarPosicion(e.pasajero.ID, e.trip.ID, punto); err != nil {
		t.Fatalf("ApuntarPosicion: %v", err)
	}
	vista, err := e.svc.VerSeguimiento(testigoDelEnlace(t, enlace.URL))
	if err != nil {
		t.Fatalf("VerSeguimiento: %v", err)
	}
	if vista.UltimaPosicion == nil || vista.UltimaPosicion.Lat != punto.Lat {
		t.Fatalf("posición = %+v", vista.UltimaPosicion)
	}
}

func TestConfirmarLaPlazaAvisaALosContactos(t *testing.T) {
	// Esperar a que la gente se acuerde de compartir el viaje es esperar a que
	// no lo haga.
	svc, identidad, avisos := newTestServiceConAvisos(t)
	e := escenarioCon(t, svc, identidad, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, svc, identidad, e.pasajero.ID, trust.LevelVerificado)
	contactoDe(t, svc, e.pasajero.ID, "madre@ejemplo.test", true)

	b, err := e.reservar()
	if err != nil {
		t.Fatalf("RequestBooking: %v", err)
	}
	if _, err := svc.DecideBooking(DecisionInput{
		BookingID: b.ID, HostID: e.host.ID, Accept: true, AceptaResponsabilidad: true,
	}); err != nil {
		t.Fatalf("DecideBooking: %v", err)
	}

	a, ok := avisos.Ultimo(notify.SucesoViajeCompartido)
	if !ok {
		t.Fatal("no se avisó a ningún contacto de confianza")
	}
	if a.Para != "madre@ejemplo.test" {
		t.Fatalf("aviso a %q", a.Para)
	}
	// Y el enlace del aviso funciona de verdad.
	if _, err := svc.VerSeguimiento(testigoDelEnlace(t, a.Datos["enlace"])); err != nil {
		t.Fatalf("el enlace del aviso no sirve: %v", err)
	}
}

func TestSinAvisarAlSalirNoSeAvisaANadie(t *testing.T) {
	svc, identidad, avisos := newTestServiceConAvisos(t)
	e := escenarioCon(t, svc, identidad, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, svc, identidad, e.pasajero.ID, trust.LevelVerificado)
	contactoDe(t, svc, e.pasajero.ID, "madre@ejemplo.test", false)

	b, _ := e.reservar()
	if _, err := svc.DecideBooking(DecisionInput{
		BookingID: b.ID, HostID: e.host.ID, Accept: true, AceptaResponsabilidad: true,
	}); err != nil {
		t.Fatalf("DecideBooking: %v", err)
	}
	if _, ok := avisos.Ultimo(notify.SucesoViajeCompartido); ok {
		t.Fatal("se avisó a un contacto que no lo pidió")
	}
}

// --- Botón de emergencia ---

func TestElBotonAvisaALosContactosConElEnlaceYLaPosicion(t *testing.T) {
	e, avisos := viajeConfirmado(t)
	contactoDe(t, e.svc, e.pasajero.ID, "madre@ejemplo.test", false)
	punto := geo.Point{Lat: 30.2672, Lng: -97.7431}

	a, err := e.svc.Emergencia(AlertaInput{
		UserID: e.pasajero.ID, TripID: e.trip.ID, Posicion: &punto,
		Nota: "Me ha pedido que me baje en otro sitio",
	})
	if err != nil {
		t.Fatalf("Emergencia: %v", err)
	}
	if a.Estado != domain.AlertaAbierta || a.Posicion == nil {
		t.Fatalf("alerta = %+v", a)
	}

	aviso, ok := avisos.Ultimo(notify.SucesoAlertaDisparada)
	if !ok {
		t.Fatal("no se avisó a los contactos")
	}
	if aviso.Para != "madre@ejemplo.test" {
		t.Fatalf("aviso a %q", aviso.Para)
	}
	// El enlace del aviso lleva a un seguimiento que existe y trae la alerta.
	vista, err := e.svc.VerSeguimiento(testigoDelEnlace(t, aviso.Datos["enlace"]))
	if err != nil {
		t.Fatalf("el enlace del aviso no sirve: %v", err)
	}
	if vista.Alerta == nil || vista.Alerta.ID != a.ID {
		t.Fatalf("el enlace no enseña la alerta: %+v", vista.Alerta)
	}
	if vista.UltimaPosicion == nil {
		t.Fatal("el enlace no enseña dónde estaba")
	}

	// Y el correo dice a qué número llamar, sin fingir que llamamos nosotros.
	asunto, cuerpo := notify.Componer(aviso)
	if !strings.Contains(cuerpo, domain.TelefonoEmergencias) {
		t.Fatalf("el aviso no dice a qué número llamar:\n%s", cuerpo)
	}
	if strings.Contains(asunto+cuerpo, "{") {
		t.Fatalf("quedaron huecos sin rellenar:\n%s\n%s", asunto, cuerpo)
	}
}

func TestElBotonFuncionaSinPermisoDeUbicacion(t *testing.T) {
	// Lo que no puede pasar es que el botón falle justo cuando hace falta.
	e, avisos := viajeConfirmado(t)
	contactoDe(t, e.svc, e.pasajero.ID, "madre@ejemplo.test", false)

	a, err := e.svc.Emergencia(AlertaInput{UserID: e.pasajero.ID, TripID: e.trip.ID})
	if err != nil {
		t.Fatalf("Emergencia sin posición: %v", err)
	}
	if a.Posicion != nil {
		t.Fatalf("posición = %+v, esperaba ninguna", a.Posicion)
	}
	if _, ok := avisos.Ultimo(notify.SucesoAlertaDisparada); !ok {
		t.Fatal("sin posición no se avisó a nadie")
	}
}

func TestElBotonAbreElEnlaceSiNoLoHabia(t *testing.T) {
	// Quien pulsa el botón no está en condiciones de acordarse de compartir el
	// viaje antes.
	e, avisos := viajeConfirmado(t)
	contactoDe(t, e.svc, e.pasajero.ID, "madre@ejemplo.test", false)

	if vivos, _ := e.svc.store.SeguimientosVivos(e.trip.ID, e.pasajero.ID); len(vivos) != 0 {
		t.Fatal("ya había enlace antes de la alerta")
	}
	if _, err := e.svc.Emergencia(AlertaInput{UserID: e.pasajero.ID, TripID: e.trip.ID}); err != nil {
		t.Fatalf("Emergencia: %v", err)
	}
	aviso, _ := avisos.Ultimo(notify.SucesoAlertaDisparada)
	if _, err := e.svc.VerSeguimiento(testigoDelEnlace(t, aviso.Datos["enlace"])); err != nil {
		t.Fatalf("el botón no abrió un enlace utilizable: %v", err)
	}
}

func TestSoloPulsaElBotonQuienVaEnElViaje(t *testing.T) {
	e, _ := viajeConfirmado(t)
	extraño, err := e.svc.Register(RegisterInput{
		Name: "Eva", Email: "eva@ejemplo.test", Password: testPassword,
		Idioma: "es", AceptaTerminos: true,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, err = e.svc.Emergencia(AlertaInput{UserID: extraño.User.ID, TripID: e.trip.ID})
	if !errors.Is(err, ErrNoVasEnEsteViaje) {
		t.Fatalf("err = %v, esperaba ErrNoVasEnEsteViaje", err)
	}
}

func TestRetirarLaAlertaMandaLaCorreccion(t *testing.T) {
	// Los correos ya salieron y no se pueden recoger: lo único honesto es
	// mandar otro diciendo que no pasa nada.
	e, avisos := viajeConfirmado(t)
	contactoDe(t, e.svc, e.pasajero.ID, "madre@ejemplo.test", false)
	a, err := e.svc.Emergencia(AlertaInput{UserID: e.pasajero.ID, TripID: e.trip.ID})
	if err != nil {
		t.Fatalf("Emergencia: %v", err)
	}

	retirada, err := e.svc.RetirarAlerta(a.ID, e.pasajero.ID)
	if err != nil {
		t.Fatalf("RetirarAlerta: %v", err)
	}
	if retirada.Estado != domain.AlertaRetirada || retirada.ResueltaAt.IsZero() {
		t.Fatalf("alerta = %+v", retirada)
	}
	aviso, ok := avisos.Ultimo(notify.SucesoAlertaRetirada)
	if !ok {
		t.Fatal("no se mandó la corrección")
	}
	if aviso.Para != "madre@ejemplo.test" {
		t.Fatalf("corrección a %q", aviso.Para)
	}
	// Y deja de salir en la cola de operaciones.
	cola, err := e.svc.AlertasPendientes()
	if err != nil {
		t.Fatalf("AlertasPendientes: %v", err)
	}
	if len(cola) != 0 {
		t.Fatalf("cola = %d, esperaba vacía", len(cola))
	}
}

func TestNadieAjenoRetiraTuAlerta(t *testing.T) {
	e, _ := viajeConfirmado(t)
	a, err := e.svc.Emergencia(AlertaInput{UserID: e.pasajero.ID, TripID: e.trip.ID})
	if err != nil {
		t.Fatalf("Emergencia: %v", err)
	}
	if _, err := e.svc.RetirarAlerta(a.ID, e.host.ID); !errors.Is(err, ErrNoAutorizado) {
		t.Fatalf("err = %v, esperaba ErrNoAutorizado", err)
	}
}

func TestLaAlertaEntraEnLaColaDeOperaciones(t *testing.T) {
	e, _ := viajeConfirmado(t)
	a, err := e.svc.Emergencia(AlertaInput{UserID: e.pasajero.ID, TripID: e.trip.ID})
	if err != nil {
		t.Fatalf("Emergencia: %v", err)
	}

	cola, err := e.svc.AlertasPendientes()
	if err != nil {
		t.Fatalf("AlertasPendientes: %v", err)
	}
	if len(cola) != 1 || cola[0].ID != a.ID {
		t.Fatalf("cola = %+v", cola)
	}

	atendida, err := e.svc.AtenderAlerta(a.ID, "Hablado con ella por teléfono, está bien.")
	if err != nil {
		t.Fatalf("AtenderAlerta: %v", err)
	}
	if atendida.Estado != domain.AlertaAtendida || atendida.Resolucion == "" {
		t.Fatalf("alerta = %+v", atendida)
	}
	if _, err := e.svc.AtenderAlerta(a.ID, "otra vez"); !errors.Is(err, ErrAlertaResuelta) {
		t.Fatalf("atender dos veces = %v, esperaba ErrAlertaResuelta", err)
	}
}
