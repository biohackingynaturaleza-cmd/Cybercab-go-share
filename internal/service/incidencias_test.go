package service

import (
	"errors"
	"testing"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/billing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// viajeCerrado deja un trayecto completado con un pasajero confirmado, que es
// la única situación en la que se puede declarar una incidencia.
func viajeCerrado(t *testing.T) escenario {
	t.Helper()
	e, _ := viajeCompletado(t, 0)
	return e
}

func declarar(t *testing.T, e escenario, importe int64) *domain.Incidencia {
	t.Helper()
	i, err := e.svc.DeclararIncidencia(DeclararIncidenciaInput{
		TripID: e.trip.ID, DeclaranteID: e.host.ID, AtribuidaA: e.pasajero.ID,
		Tipo: domain.IncidenciaLimpieza, ImporteCents: importe,
		Descripcion: "Restos de comida en el asiento",
	})
	if err != nil {
		t.Fatalf("DeclararIncidencia: %v", err)
	}
	return i
}

func TestDeclararUnaIncidenciaNoCobraNada(t *testing.T) {
	// El punto entero del diseño: nadie puede cargarle dinero a otro por su
	// sola palabra.
	e := viajeCerrado(t)
	antes, _ := e.svc.ApuntesDe(e.pasajero.ID)

	i := declarar(t, e, 5000)
	if i.Estado != domain.IncidenciaDeclarada {
		t.Fatalf("estado = %q, esperaba declarada", i.Estado)
	}

	despues, _ := e.svc.ApuntesDe(e.pasajero.ID)
	if len(despues) != len(antes) {
		t.Fatalf("apuntes = %d, antes %d: declarar no puede generar deuda",
			len(despues), len(antes))
	}
}

func TestAceptarUnaIncidenciaLaConvierteEnDeuda(t *testing.T) {
	e := viajeCerrado(t)
	i := declarar(t, e, 5000)

	resuelta, err := e.svc.ResponderIncidencia(i.ID, e.pasajero.ID, true)
	if err != nil {
		t.Fatalf("ResponderIncidencia: %v", err)
	}
	if resuelta.Estado != domain.IncidenciaAceptada {
		t.Fatalf("estado = %q", resuelta.Estado)
	}

	apuntes, _ := e.svc.ApuntesDe(e.pasajero.ID)
	var encontrada bool
	for _, a := range apuntes {
		if a.AmountCents == 5000 && a.CounterpartyID == e.host.ID {
			encontrada = true
			// No lleva comisión nuestra: no hemos hecho nada por ese dinero,
			// solo lo trasladamos.
			if a.Kind != billing.EntryCostShare {
				t.Errorf("tipo de apunte = %q", a.Kind)
			}
		}
	}
	if !encontrada {
		t.Fatalf("no se anotó la deuda: %+v", apuntes)
	}
}

func TestDiscutirUnaIncidenciaNoCobraNada(t *testing.T) {
	// Cuando dos versiones se contradicen, la app no puede saber cuál es
	// cierta. Lo honesto es no cobrar y que lo mire una persona.
	e := viajeCerrado(t)
	i := declarar(t, e, 15000)

	resuelta, err := e.svc.ResponderIncidencia(i.ID, e.pasajero.ID, false)
	if err != nil {
		t.Fatalf("ResponderIncidencia: %v", err)
	}
	if resuelta.Estado != domain.IncidenciaDiscutida {
		t.Fatalf("estado = %q, esperaba discutida", resuelta.Estado)
	}

	apuntes, _ := e.svc.ApuntesDe(e.pasajero.ID)
	for _, a := range apuntes {
		if a.AmountCents == 15000 {
			t.Fatal("se ha cobrado una incidencia discutida")
		}
	}
}

func TestSoloQuienLaRecibePuedeResponder(t *testing.T) {
	e := viajeCerrado(t)
	i := declarar(t, e, 5000)

	// Ni quien la declaró puede aceptarla en nombre del otro.
	if _, err := e.svc.ResponderIncidencia(i.ID, e.host.ID, true); !errors.Is(err, ErrNoAutorizado) {
		t.Fatalf("error = %v, esperaba ErrNoAutorizado", err)
	}
}

func TestNoSePuedeResponderDosVeces(t *testing.T) {
	e := viajeCerrado(t)
	i := declarar(t, e, 5000)

	if _, err := e.svc.ResponderIncidencia(i.ID, e.pasajero.ID, false); err != nil {
		t.Fatalf("primera respuesta: %v", err)
	}
	if _, err := e.svc.ResponderIncidencia(i.ID, e.pasajero.ID, true); !errors.Is(err, ErrIncidenciaResuelta) {
		t.Fatalf("error = %v: discutir y luego aceptar sería reabrir lo cerrado", err)
	}
}

func TestSoloQuienOrganizaDeclara(t *testing.T) {
	// Es a quien la flota le cobra; nadie más tiene nada que repercutir.
	e := viajeCerrado(t)
	_, err := e.svc.DeclararIncidencia(DeclararIncidenciaInput{
		TripID: e.trip.ID, DeclaranteID: e.pasajero.ID, AtribuidaA: e.host.ID,
		Tipo: domain.IncidenciaLimpieza, ImporteCents: 5000,
	})
	if !errors.Is(err, ErrNoAutorizado) {
		t.Fatalf("error = %v, esperaba ErrNoAutorizado", err)
	}
}

func TestNoSePuedeCulparAQuienNoViajaba(t *testing.T) {
	e := viajeCerrado(t)
	tercero, _ := e.svc.Register(RegisterInput{Name: "Eva", Email: "eva@example.com", Password: testPassword, Idioma: "es", AceptaTerminos: true})

	_, err := e.svc.DeclararIncidencia(DeclararIncidenciaInput{
		TripID: e.trip.ID, DeclaranteID: e.host.ID, AtribuidaA: tercero.User.ID,
		Tipo: domain.IncidenciaLimpieza, ImporteCents: 5000,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error = %v, esperaba un error de validación", err)
	}
}

func TestElImporteEstaAcotado(t *testing.T) {
	// Sin tope, la app se convertiría en una herramienta de extorsión entre
	// desconocidos. La tasa más cara de la flota son 150 $.
	e := viajeCerrado(t)
	casos := map[string]int64{"cero": 0, "negativo": -100, "desorbitado": 500000}
	for nombre, importe := range casos {
		_, err := e.svc.DeclararIncidencia(DeclararIncidenciaInput{
			TripID: e.trip.ID, DeclaranteID: e.host.ID, AtribuidaA: e.pasajero.ID,
			Tipo: domain.IncidenciaLimpieza, ImporteCents: importe,
		})
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s (%d): error = %v", nombre, importe, err)
		}
	}
}

func TestNoSeAcumulanIncidenciasVivas(t *testing.T) {
	// Si hace falta otra, primero se resuelve la anterior.
	e := viajeCerrado(t)
	declarar(t, e, 5000)

	_, err := e.svc.DeclararIncidencia(DeclararIncidenciaInput{
		TripID: e.trip.ID, DeclaranteID: e.host.ID, AtribuidaA: e.pasajero.ID,
		Tipo: domain.IncidenciaDanos, ImporteCents: 3000,
	})
	if !errors.Is(err, store.ErrIncidenciaEnCurso) {
		t.Fatalf("error = %v, esperaba ErrIncidenciaEnCurso", err)
	}
}

func TestNoSePuedeDeclararEnUnViajeSinCerrar(t *testing.T) {
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	_, err := e.svc.DeclararIncidencia(DeclararIncidenciaInput{
		TripID: e.trip.ID, DeclaranteID: e.host.ID, AtribuidaA: e.pasajero.ID,
		Tipo: domain.IncidenciaLimpieza, ImporteCents: 5000,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error = %v, esperaba un error de validación", err)
	}
}

func TestRetirarUnaIncidenciaAceptadaNoDeshaceLaDeuda(t *testing.T) {
	e := viajeCerrado(t)
	i := declarar(t, e, 5000)
	if _, err := e.svc.ResponderIncidencia(i.ID, e.pasajero.ID, true); err != nil {
		t.Fatalf("ResponderIncidencia: %v", err)
	}

	if _, err := e.svc.RetirarIncidencia(i.ID, e.host.ID); !errors.Is(err, ErrIncidenciaResuelta) {
		t.Fatalf("error = %v: lo ya anotado no se borra retirando la incidencia", err)
	}
}

func TestSeAvisaEnCadaPasoDeLaIncidencia(t *testing.T) {
	e := viajeCerrado(t)
	grabadorDe(t, e.svc).Limpiar()

	i := declarar(t, e, 5000)
	if _, ok := grabadorDe(t, e.svc).Ultimo(notify.SucesoIncidenciaDeclarada); !ok {
		t.Error("no se avisó a quien se le atribuye")
	}

	if _, err := e.svc.ResponderIncidencia(i.ID, e.pasajero.ID, true); err != nil {
		t.Fatalf("ResponderIncidencia: %v", err)
	}
	if _, ok := grabadorDe(t, e.svc).Ultimo(notify.SucesoIncidenciaAceptada); !ok {
		t.Error("no se avisó a quien la declaró de que la aceptaron")
	}
}

// grabadorDe recupera el grabador de avisos del servicio.
func grabadorDe(t *testing.T, svc *Service) *notify.Grabador {
	t.Helper()
	g, ok := svc.cfg.Avisos.(*notify.Grabador)
	if !ok {
		t.Fatal("el servicio de pruebas no lleva grabador de avisos")
	}
	return g
}
