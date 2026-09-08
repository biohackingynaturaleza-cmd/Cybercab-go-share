package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
)

func nuevaRecuperacion(t *testing.T, pg *store.Postgres, id, userID, hash string) *domain.Recuperacion {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	r := &domain.Recuperacion{
		ID: id, UserID: userID, TokenHash: hash,
		CreatedAt: now, ExpiraAt: now.Add(domain.VigenciaRecuperacion),
	}
	if err := pg.CrearRecuperacion(r); err != nil {
		t.Fatalf("CrearRecuperacion(%s): %v", id, err)
	}
	return r
}

func TestPostgresRecuperacionIdaYVuelta(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	creada := nuevaRecuperacion(t, pg, "rec_1", "usr_1", "hash-de-prueba")

	leida, err := pg.RecuperacionPorHash("hash-de-prueba")
	if err != nil {
		t.Fatalf("RecuperacionPorHash: %v", err)
	}
	if leida.ID != creada.ID || leida.UserID != "usr_1" {
		t.Fatalf("leída = %+v", leida)
	}
	if !leida.ExpiraAt.Equal(creada.ExpiraAt) {
		t.Errorf("expira_at = %v, esperaba %v", leida.ExpiraAt, creada.ExpiraAt)
	}
	if !leida.Vigente(time.Now().UTC()) {
		t.Error("una recuperación recién creada tiene que estar vigente")
	}
}

func TestPostgresUnaRecuperacionSoloSeGastaUnaVez(t *testing.T) {
	// La condición va en el WHERE de la sentencia, no en un if previo: es lo
	// único que impide que dos peticiones simultáneas gasten el mismo enlace.
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevaRecuperacion(t, pg, "rec_1", "usr_1", "hash-de-prueba")

	now := time.Now().UTC()
	if err := pg.UsarRecuperacion("rec_1", now); err != nil {
		t.Fatalf("primer uso: %v", err)
	}
	if err := pg.UsarRecuperacion("rec_1", now); !errors.Is(err, store.ErrRecuperacionUsada) {
		t.Fatalf("segundo uso = %v, esperaba ErrRecuperacionUsada", err)
	}

	leida, err := pg.RecuperacionPorHash("hash-de-prueba")
	if err != nil {
		t.Fatalf("RecuperacionPorHash: %v", err)
	}
	if leida.Vigente(now) {
		t.Error("una recuperación gastada no puede seguir vigente")
	}
}

func TestPostgresAnularDejaFueraLosEnlacesVivos(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevoUsuario(t, pg, "usr_2", "bruno@example.com")
	nuevaRecuperacion(t, pg, "rec_1", "usr_1", "hash-1")
	nuevaRecuperacion(t, pg, "rec_2", "usr_1", "hash-2")
	nuevaRecuperacion(t, pg, "rec_ajena", "usr_2", "hash-3")

	now := time.Now().UTC()
	if err := pg.AnularRecuperaciones("usr_1", now); err != nil {
		t.Fatalf("AnularRecuperaciones: %v", err)
	}
	for _, hash := range []string{"hash-1", "hash-2"} {
		r, err := pg.RecuperacionPorHash(hash)
		if err != nil {
			t.Fatalf("RecuperacionPorHash(%s): %v", hash, err)
		}
		if r.Vigente(now) {
			t.Errorf("%s sigue vigente tras anular", hash)
		}
	}
	// La de otra persona no se toca.
	ajena, err := pg.RecuperacionPorHash("hash-3")
	if err != nil {
		t.Fatalf("RecuperacionPorHash(hash-3): %v", err)
	}
	if !ajena.Vigente(now) {
		t.Error("anular los enlaces de alguien no puede tocar los de otro")
	}
}

func TestPostgresCambiarContrasena(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")

	if err := pg.CambiarContrasena("usr_1", "$2a$10$otro-hash"); err != nil {
		t.Fatalf("CambiarContrasena: %v", err)
	}
	u, err := pg.GetUser("usr_1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.PasswordHash != "$2a$10$otro-hash" {
		t.Fatalf("hash = %q", u.PasswordHash)
	}
	if err := pg.CambiarContrasena("usr_no_existe", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("usuario inexistente = %v, esperaba ErrNotFound", err)
	}
}

func TestPostgresGuardaQueTerminosSeAceptaron(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")

	// Una cuenta creada sin aceptar nada queda sin versión, no al día.
	u, err := pg.GetUser("usr_1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.TerminosAlDia() {
		t.Fatal("una cuenta sin aceptación no puede estar al día")
	}

	cuando := time.Now().UTC().Truncate(time.Millisecond)
	if err := pg.AceptarTerminos("usr_1", domain.VersionTerminos, cuando); err != nil {
		t.Fatalf("AceptarTerminos: %v", err)
	}
	u, err = pg.GetUser("usr_1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !u.TerminosAlDia() {
		t.Fatalf("versión = %q, esperaba %q", u.TerminosVersion, domain.VersionTerminos)
	}
	if u.TerminosAt == nil || !u.TerminosAt.Equal(cuando) {
		t.Fatalf("terminos_at = %v, esperaba %v", u.TerminosAt, cuando)
	}
}
