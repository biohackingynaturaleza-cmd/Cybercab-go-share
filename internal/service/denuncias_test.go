package service

import (
	"errors"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// viajeCompletadoConAvisos es viajeCompletado con un grabador de correos, para
// las pruebas que comprueban a quién se avisa y de qué.
func viajeCompletadoConAvisos(t *testing.T) (escenario, *notify.Grabador) {
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
	if _, _, err := svc.CompletarViaje(CierreInput{TripID: e.trip.ID, HostID: e.host.ID}); err != nil {
		t.Fatalf("CompletarViaje: %v", err)
	}
	return e, avisos
}

func denunciar(t *testing.T, e escenario, de, a string, motivo domain.MotivoDenuncia) *domain.Denuncia {
	t.Helper()
	d, err := e.svc.Denunciar(DenunciarInput{
		DenuncianteID: de, DenunciadoID: a, Motivo: motivo, TripID: e.trip.ID,
		Descripcion: "Se puso a gritar dentro del coche y no paró en todo el trayecto.",
	})
	if err != nil {
		t.Fatalf("Denunciar: %v", err)
	}
	return d
}

func TestSoloSePuedeDenunciarAQuienCompartioViaje(t *testing.T) {
	// Sin esta regla el sistema es un arma: basta con registrarse y repartir
	// denuncias contra quien molesta.
	e, _ := viajeCompletado(t, 0)
	extraño, err := e.svc.Register(RegisterInput{
		Name: "Eva", Email: "eva@example.com", Password: testPassword,
		Idioma: "es", AceptaTerminos: true,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err = e.svc.Denunciar(DenunciarInput{
		DenuncianteID: extraño.User.ID, DenunciadoID: e.host.ID,
		Motivo: domain.DenunciaConducta, Descripcion: "No me gusta esta persona en absoluto.",
	})
	if !errors.Is(err, ErrSinTratoPrevio) {
		t.Fatalf("err = %v, esperaba ErrSinTratoPrevio", err)
	}
}

func TestDenunciarBloqueaEnLasDosDirecciones(t *testing.T) {
	// Quien acaba de pasar un mal rato no debería tener que marcar además una
	// casilla para no volver a cruzarse con quien se lo hizo pasar.
	e, _ := viajeCompletado(t, 0)
	denunciar(t, e, e.pasajero.ID, e.host.ID, domain.DenunciaConducta)

	bloqueados, err := e.svc.bloqueados(e.pasajero.ID)
	if err != nil {
		t.Fatalf("bloqueados: %v", err)
	}
	if !bloqueados[e.host.ID] {
		t.Fatal("denunciar no bloqueó a quien fue denunciado")
	}
	// Y en el otro sentido: el bloqueo corta para los dos.
	delOtro, err := e.svc.bloqueados(e.host.ID)
	if err != nil {
		t.Fatalf("bloqueados(host): %v", err)
	}
	if !delOtro[e.pasajero.ID] {
		t.Fatal("el bloqueo no corta en la dirección contraria")
	}
}

func TestLaDenunciaNoSeLeEnsenaAlDenunciado(t *testing.T) {
	// Una denuncia que llega a oídos del denunciado es una denuncia que nadie
	// pone.
	e, _ := viajeCompletado(t, 0)
	denunciar(t, e, e.pasajero.ID, e.host.ID, domain.DenunciaSeguridad)

	suyas, err := e.svc.MisDenuncias(e.host.ID)
	if err != nil {
		t.Fatalf("MisDenuncias: %v", err)
	}
	if len(suyas) != 0 {
		t.Fatalf("el denunciado ve %d denuncias, esperaba ninguna", len(suyas))
	}
	// Quien la puso sí las ve.
	delDenunciante, err := e.svc.MisDenuncias(e.pasajero.ID)
	if err != nil {
		t.Fatalf("MisDenuncias(denunciante): %v", err)
	}
	if len(delDenunciante) != 1 {
		t.Fatalf("quien denunció ve %d, esperaba la suya", len(delDenunciante))
	}
}

func TestLoUrgenteVaPrimeroEnLaCola(t *testing.T) {
	// Una denuncia de seguridad esperando turno detrás de veinte de conducta es
	// una cola mal ordenada.
	e, _ := viajeCompletado(t, 0)
	denunciar(t, e, e.pasajero.ID, e.host.ID, domain.DenunciaConducta)
	denunciar(t, e, e.host.ID, e.pasajero.ID, domain.DenunciaSeguridad)

	cola, err := e.svc.DenunciasPendientes()
	if err != nil {
		t.Fatalf("DenunciasPendientes: %v", err)
	}
	if len(cola) != 2 {
		t.Fatalf("cola = %d, esperaba 2", len(cola))
	}
	if cola[0].Motivo != domain.DenunciaSeguridad {
		t.Fatalf("primera de la cola = %q, esperaba la de seguridad", cola[0].Motivo)
	}
}

func TestConfirmarUnaDenunciaSuspendeYLoDice(t *testing.T) {
	// Un sistema de denuncias que solo archiva es un buzón de quejas, y la
	// gente deja de usarlo en cuanto se da cuenta.
	e, avisos := viajeCompletadoConAvisos(t)
	svc := e.svc
	d := denunciar(t, e, e.host.ID, e.pasajero.ID, domain.DenunciaSeguridad)

	resuelta, err := svc.ResolverDenuncia(ResolucionInput{
		DenunciaID: d.ID, Confirmada: true,
		Resolucion: "Confirmado con el relato de la otra parte.",
	})
	if err != nil {
		t.Fatalf("ResolverDenuncia: %v", err)
	}
	if resuelta.Estado != domain.DenunciaConfirmada {
		t.Fatalf("estado = %q", resuelta.Estado)
	}

	u, err := svc.GetUser(e.pasajero.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !u.Suspendido(svc.cfg.Now()) {
		t.Fatal("confirmar la denuncia no suspendió a nadie")
	}

	// Y se le dice por qué: una suspensión sin explicación no se puede recurrir.
	aviso, ok := avisos.Ultimo(notify.SucesoCuentaSuspendida)
	if !ok {
		t.Fatal("no se avisó a quien queda suspendido")
	}
	if aviso.Para != u.Email {
		t.Fatalf("el aviso fue a %q", aviso.Para)
	}
	if aviso.Datos["resolucion"] == "" || aviso.Datos["hasta"] == "" {
		t.Fatalf("el aviso no dice por qué ni hasta cuándo: %+v", aviso.Datos)
	}
}

func TestQuienEstaSuspendidoNoSeSubeAUnCoche(t *testing.T) {
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelVerificado)

	hasta := e.svc.cfg.Now().Add(30 * 24 * time.Hour)
	if err := e.svc.store.SuspenderUsuario(e.pasajero.ID, &hasta); err != nil {
		t.Fatalf("SuspenderUsuario: %v", err)
	}

	if _, err := e.reservar(); !errors.Is(err, ErrSuspendido) {
		t.Fatalf("reservar = %v, esperaba ErrSuspendido", err)
	}
	// Tampoco puede publicar sus propios trayectos.
	_, err := e.svc.CreateTrip(t.Context(), NewTripInput{
		HostID: e.pasajero.ID, Origin: downtown, Destination: airport,
		DepartureTime: e.svc.cfg.Now().Add(2 * time.Hour), Vehicle: domain.VehicleModelY,
	})
	if !errors.Is(err, ErrSuspendido) {
		t.Fatalf("CreateTrip = %v, esperaba ErrSuspendido", err)
	}
}

func TestLaSuspensionVenceSola(t *testing.T) {
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelVerificado)

	base := e.svc.cfg.Now()
	hasta := base.Add(7 * 24 * time.Hour)
	if err := e.svc.store.SuspenderUsuario(e.pasajero.ID, &hasta); err != nil {
		t.Fatalf("SuspenderUsuario: %v", err)
	}
	e.svc.cfg.Now = func() time.Time { return hasta.Add(time.Minute) }

	if _, err := e.reservar(); err != nil {
		t.Fatalf("pasada la suspensión sigue sin poder reservar: %v", err)
	}
}

func TestSePuedeLevantarUnaSuspension(t *testing.T) {
	// Una suspensión que no se puede deshacer convierte cada error nuestro en
	// definitivo.
	e := nuevoEscenario(t, domain.VehicleModelY, trust.LevelNuevo)
	acreditar(t, e.svc, e.identidad, e.pasajero.ID, trust.LevelVerificado)
	hasta := domain.SuspensionPermanente
	if err := e.svc.store.SuspenderUsuario(e.pasajero.ID, &hasta); err != nil {
		t.Fatalf("SuspenderUsuario: %v", err)
	}

	if err := e.svc.LevantarSuspension(e.pasajero.ID); err != nil {
		t.Fatalf("LevantarSuspension: %v", err)
	}
	if _, err := e.reservar(); err != nil {
		t.Fatalf("tras levantar la suspensión sigue bloqueado: %v", err)
	}
}

func TestDesestimarUnaDenunciaNoSuspendeANadie(t *testing.T) {
	e, _ := viajeCompletado(t, 0)
	d := denunciar(t, e, e.host.ID, e.pasajero.ID, domain.DenunciaConducta)

	if _, err := e.svc.ResolverDenuncia(ResolucionInput{
		DenunciaID: d.ID, Confirmada: false, Resolucion: "No hay nada que sostenga el relato.",
	}); err != nil {
		t.Fatalf("ResolverDenuncia: %v", err)
	}
	u, err := e.svc.GetUser(e.pasajero.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.Suspendido(e.svc.cfg.Now()) {
		t.Fatal("desestimar una denuncia suspendió a alguien")
	}
}

func TestUnaDenunciaSoloSeResuelveUnaVez(t *testing.T) {
	e, _ := viajeCompletado(t, 0)
	d := denunciar(t, e, e.host.ID, e.pasajero.ID, domain.DenunciaConducta)
	entrada := ResolucionInput{DenunciaID: d.ID, Confirmada: false, Resolucion: "Sin recorrido."}

	if _, err := e.svc.ResolverDenuncia(entrada); err != nil {
		t.Fatalf("primera resolución: %v", err)
	}
	if _, err := e.svc.ResolverDenuncia(entrada); !errors.Is(err, ErrDenunciaResuelta) {
		t.Fatalf("segunda resolución = %v, esperaba ErrDenunciaResuelta", err)
	}
}

func TestUnaDenunciaSinRelatoNoSeAdmite(t *testing.T) {
	// Una denuncia que no se puede revisar es peor que no tenerla: promete una
	// revisión que nadie puede hacer.
	e, _ := viajeCompletado(t, 0)
	_, err := e.svc.Denunciar(DenunciarInput{
		DenuncianteID: e.host.ID, DenunciadoID: e.pasajero.ID,
		Motivo: domain.DenunciaConducta, Descripcion: "mal",
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, esperaba un fallo de validación", err)
	}
}
