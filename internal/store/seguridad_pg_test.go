package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
)

func TestPostgresContactosDeConfianza(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevoUsuario(t, pg, "usr_2", "bruno@example.com")

	c := &domain.ContactoDeConfianza{
		ID: "ctc_1", UserID: "usr_1", Nombre: "Madre", Email: "madre@example.com",
		AvisarAlSalir: true, CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := pg.CrearContacto(c); err != nil {
		t.Fatalf("CrearContacto: %v", err)
	}

	// La misma dirección no se añade dos veces: dos avisos idénticos no avisan
	// más.
	repetido := *c
	repetido.ID, repetido.Email = "ctc_2", "MADRE@Example.com"
	if err := pg.CrearContacto(&repetido); !errors.Is(err, store.ErrContactoRepetido) {
		t.Fatalf("repetido = %v, esperaba ErrContactoRepetido", err)
	}
	// Pero otra persona sí puede tener a la misma.
	deOtro := *c
	deOtro.ID, deOtro.UserID = "ctc_3", "usr_2"
	if err := pg.CrearContacto(&deOtro); err != nil {
		t.Fatalf("el contacto de otra persona: %v", err)
	}

	cs, err := pg.ContactosDe("usr_1")
	if err != nil {
		t.Fatalf("ContactosDe: %v", err)
	}
	if len(cs) != 1 || cs[0].Nombre != "Madre" || !cs[0].AvisarAlSalir {
		t.Fatalf("contactos = %+v", cs)
	}

	// Nadie borra el contacto de otro.
	if err := pg.BorrarContacto("ctc_1", "usr_2"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("borrado ajeno = %v, esperaba ErrNotFound", err)
	}
	if err := pg.BorrarContacto("ctc_1", "usr_1"); err != nil {
		t.Fatalf("BorrarContacto: %v", err)
	}
}

func TestPostgresVariosSeguimientosVivosConviven(t *testing.T) {
	// Un enlace ya mandado no se puede reconstruir —solo se guarda su hash—,
	// así que crear otro no puede matarlo.
	pg := newPostgres(t)
	host, _, tripID, _ := parejaConReserva(t, pg)
	now := time.Now().UTC().Truncate(time.Millisecond)

	primero := &domain.Seguimiento{
		ID: "seg_1", TripID: tripID, UserID: host.ID, TokenHash: "hash-1",
		CreatedAt: now, ExpiraAt: now.Add(domain.GraciaSeguimiento),
	}
	if err := pg.CrearSeguimiento(primero); err != nil {
		t.Fatalf("primero: %v", err)
	}
	segundo := &domain.Seguimiento{
		ID: "seg_2", TripID: tripID, UserID: host.ID, TokenHash: "hash-2",
		CreatedAt: now.Add(time.Minute), ExpiraAt: now.Add(domain.GraciaSeguimiento),
	}
	if err := pg.CrearSeguimiento(segundo); err != nil {
		t.Fatalf("segundo: %v", err)
	}

	viejo, err := pg.SeguimientoPorHash("hash-1")
	if err != nil {
		t.Fatalf("SeguimientoPorHash: %v", err)
	}
	if !viejo.Vigente(time.Now().UTC()) {
		t.Fatal("el enlace anterior dejó de valer al crear otro")
	}
	vivos, err := pg.SeguimientosVivos(tripID, host.ID)
	if err != nil {
		t.Fatalf("SeguimientosVivos: %v", err)
	}
	if len(vivos) != 2 || vivos[0].ID != "seg_2" {
		t.Fatalf("vivos = %d, el primero %v: esperaba los dos con el nuevo delante",
			len(vivos), vivos[0].ID)
	}

	// Dejar de compartir los cierra todos.
	n, err := pg.RevocarSeguimientos(tripID, host.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("RevocarSeguimientos: %v", err)
	}
	if n != 2 {
		t.Fatalf("cerrados = %d, esperaba 2", n)
	}
	vivos, err = pg.SeguimientosVivos(tripID, host.ID)
	if err != nil {
		t.Fatalf("SeguimientosVivos: %v", err)
	}
	if len(vivos) != 0 {
		t.Fatalf("vivos tras cerrar = %d", len(vivos))
	}
}

func TestPostgresLaPosicionVaYVuelve(t *testing.T) {
	pg := newPostgres(t)
	host, _, tripID, _ := parejaConReserva(t, pg)
	now := time.Now().UTC().Truncate(time.Millisecond)
	seg := &domain.Seguimiento{
		ID: "seg_1", TripID: tripID, UserID: host.ID, TokenHash: "hash-1",
		CreatedAt: now, ExpiraAt: now.Add(domain.GraciaSeguimiento),
	}
	if err := pg.CrearSeguimiento(seg); err != nil {
		t.Fatalf("CrearSeguimiento: %v", err)
	}

	// Sin posición, nula: no se rastrea a nadie de fondo.
	leido, err := pg.SeguimientoPorHash("hash-1")
	if err != nil {
		t.Fatalf("SeguimientoPorHash: %v", err)
	}
	if leido.UltimaPosicion != nil {
		t.Fatalf("posición = %+v, esperaba ninguna", leido.UltimaPosicion)
	}

	punto := geo.Point{Lat: 30.2672, Lng: -97.7431}
	if err := pg.ApuntarPosicion(tripID, host.ID, punto, now); err != nil {
		t.Fatalf("ApuntarPosicion: %v", err)
	}
	leido, err = pg.SeguimientoPorHash("hash-1")
	if err != nil {
		t.Fatalf("SeguimientoPorHash: %v", err)
	}
	if leido.UltimaPosicion == nil || leido.UltimaPosicion.Lat != punto.Lat ||
		leido.UltimaPosicion.Lng != punto.Lng {
		t.Fatalf("posición = %+v", leido.UltimaPosicion)
	}
	if leido.UltimaPosicionAt == nil {
		t.Fatal("no quedó constancia de cuándo")
	}

	// Cerrar los de otra persona no toca los tuyos.
	if n, err := pg.RevocarSeguimientos(tripID, "usr_pas", now); err != nil || n != 0 {
		t.Fatalf("cerrados de otro = %d, err = %v", n, err)
	}
	if n, err := pg.RevocarSeguimientos(tripID, host.ID, now); err != nil || n != 1 {
		t.Fatalf("cerrados = %d, err = %v", n, err)
	}
	if vivos, _ := pg.SeguimientosVivos(tripID, host.ID); len(vivos) != 0 {
		t.Fatalf("siguen vivos: %d", len(vivos))
	}
}

func TestPostgresAlertaIdaYVuelta(t *testing.T) {
	pg := newPostgres(t)
	host, _, tripID, _ := parejaConReserva(t, pg)
	now := time.Now().UTC().Truncate(time.Millisecond)
	punto := geo.Point{Lat: 30.2672, Lng: -97.7431}

	a := &domain.Alerta{
		ID: "alr_1", TripID: tripID, UserID: host.ID, Posicion: &punto,
		Nota:   "Me ha pedido que me baje en otro sitio",
		Estado: domain.AlertaAbierta, CreatedAt: now,
	}
	if err := pg.CrearAlerta(a); err != nil {
		t.Fatalf("CrearAlerta: %v", err)
	}

	leida, err := pg.GetAlerta("alr_1")
	if err != nil {
		t.Fatalf("GetAlerta: %v", err)
	}
	if leida.Posicion == nil || leida.Posicion.Lat != punto.Lat || leida.Nota != a.Nota {
		t.Fatalf("leída = %+v", leida)
	}
	if !leida.ResueltaAt.IsZero() {
		t.Fatalf("una alerta abierta trae fecha de resolución: %v", leida.ResueltaAt)
	}

	viva, err := pg.AlertaVivaDe(tripID, host.ID)
	if err != nil {
		t.Fatalf("AlertaVivaDe: %v", err)
	}
	if viva.ID != "alr_1" {
		t.Fatalf("viva = %s", viva.ID)
	}
	abiertas, err := pg.AlertasAbiertas()
	if err != nil {
		t.Fatalf("AlertasAbiertas: %v", err)
	}
	if len(abiertas) != 1 {
		t.Fatalf("abiertas = %d", len(abiertas))
	}

	leida.Estado = domain.AlertaAtendida
	leida.ResueltaAt = now.Add(time.Minute)
	leida.Resolucion = "Hablado con ella, está bien."
	if err := pg.UpdateAlerta(leida); err != nil {
		t.Fatalf("UpdateAlerta: %v", err)
	}
	abiertas, err = pg.AlertasAbiertas()
	if err != nil {
		t.Fatalf("AlertasAbiertas: %v", err)
	}
	if len(abiertas) != 0 {
		t.Fatalf("abiertas tras atender = %d", len(abiertas))
	}
	if _, err := pg.AlertaVivaDe(tripID, host.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("sigue viva: %v", err)
	}
}

func TestPostgresUnaAlertaSinPosicionSeGuardaIgual(t *testing.T) {
	// Lo que no puede pasar es que el botón falle justo cuando hace falta.
	pg := newPostgres(t)
	host, _, tripID, _ := parejaConReserva(t, pg)
	if err := pg.CrearAlerta(&domain.Alerta{
		ID: "alr_1", TripID: tripID, UserID: host.ID,
		Estado: domain.AlertaAbierta, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CrearAlerta: %v", err)
	}
	leida, err := pg.GetAlerta("alr_1")
	if err != nil {
		t.Fatalf("GetAlerta: %v", err)
	}
	if leida.Posicion != nil {
		t.Fatalf("posición = %+v, esperaba ninguna", leida.Posicion)
	}
}
