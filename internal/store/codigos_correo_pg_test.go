package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
)

func nuevoCodigo(t *testing.T, pg *store.Postgres, id, userID, hash string) *domain.CodigoCorreo {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	c := &domain.CodigoCorreo{
		ID: id, UserID: userID, CheckRef: "correo_" + id, CodigoHash: hash,
		CreatedAt: now, ExpiraAt: now.Add(domain.VigenciaCodigoCorreo),
	}
	if err := pg.CrearCodigoCorreo(c); err != nil {
		t.Fatalf("CrearCodigoCorreo(%s): %v", id, err)
	}
	return c
}

func TestPostgresCodigoDeCorreoIdaYVuelta(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	creado := nuevoCodigo(t, pg, "cod_1", "usr_1", "hash-de-prueba")

	leido, err := pg.CodigoCorreoVivo("usr_1")
	if err != nil {
		t.Fatalf("CodigoCorreoVivo: %v", err)
	}
	if leido.ID != creado.ID || leido.CodigoHash != "hash-de-prueba" || leido.CheckRef != creado.CheckRef {
		t.Fatalf("leído = %+v", leido)
	}
	if leido.Intentos != 0 || !leido.Vigente(time.Now().UTC()) {
		t.Fatalf("un código recién creado debería estar vigente y sin intentos: %+v", leido)
	}
}

func TestPostgresLosIntentosSeCuentanEnUnaSolaSentencia(t *testing.T) {
	// Contar leyendo y escribiendo por separado deja una ventana en la que
	// varias peticiones simultáneas gastan un solo intento entre todas, que es
	// justo lo que hace quien prueba códigos a lo bruto.
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevoCodigo(t, pg, "cod_1", "usr_1", "hash-de-prueba")

	for esperado := 1; esperado <= domain.MaxIntentosCodigo; esperado++ {
		n, err := pg.AnotarIntentoCodigo("cod_1")
		if err != nil {
			t.Fatalf("AnotarIntentoCodigo: %v", err)
		}
		if n != esperado {
			t.Fatalf("intentos = %d, esperaba %d", n, esperado)
		}
	}

	leido, err := pg.CodigoCorreoVivo("usr_1")
	if err != nil {
		t.Fatalf("CodigoCorreoVivo: %v", err)
	}
	if leido.Vigente(time.Now().UTC()) {
		t.Fatal("con los intentos agotados el código no puede seguir vigente")
	}
	if leido.IntentosRestantes() != 0 {
		t.Fatalf("intentos restantes = %d, esperaba 0", leido.IntentosRestantes())
	}
}

func TestPostgresUnCodigoSoloSeGastaUnaVez(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevoCodigo(t, pg, "cod_1", "usr_1", "hash-de-prueba")

	now := time.Now().UTC()
	if err := pg.UsarCodigoCorreo("cod_1", now); err != nil {
		t.Fatalf("primer uso: %v", err)
	}
	if err := pg.UsarCodigoCorreo("cod_1", now); !errors.Is(err, store.ErrCodigoGastado) {
		t.Fatalf("segundo uso = %v, esperaba ErrCodigoGastado", err)
	}
	if _, err := pg.CodigoCorreoVivo("usr_1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("un código gastado sigue apareciendo como vivo: %v", err)
	}
}

func TestPostgresElCodigoVivoEsElUltimo(t *testing.T) {
	pg := newPostgres(t)
	nuevoUsuario(t, pg, "usr_1", "ana@example.com")
	nuevoUsuario(t, pg, "usr_2", "bruno@example.com")

	viejo := nuevoCodigo(t, pg, "cod_viejo", "usr_1", "hash-viejo")
	viejo.CreatedAt = viejo.CreatedAt.Add(-time.Hour)
	nuevoCodigo(t, pg, "cod_ajeno", "usr_2", "hash-ajeno")

	// El segundo llega después y anula al primero.
	now := time.Now().UTC()
	if err := pg.AnularCodigosCorreo("usr_1", now); err != nil {
		t.Fatalf("AnularCodigosCorreo: %v", err)
	}
	nuevoCodigo(t, pg, "cod_nuevo", "usr_1", "hash-nuevo")

	vivo, err := pg.CodigoCorreoVivo("usr_1")
	if err != nil {
		t.Fatalf("CodigoCorreoVivo: %v", err)
	}
	if vivo.ID != "cod_nuevo" {
		t.Fatalf("vivo = %s, esperaba cod_nuevo", vivo.ID)
	}

	// Anular los de alguien no toca los de otro.
	ajeno, err := pg.CodigoCorreoVivo("usr_2")
	if err != nil {
		t.Fatalf("CodigoCorreoVivo(usr_2): %v", err)
	}
	if ajeno.ID != "cod_ajeno" {
		t.Fatalf("el código de otra persona desapareció: %+v", ajeno)
	}
}
