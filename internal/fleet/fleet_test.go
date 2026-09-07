package fleet

import (
	"context"
	"errors"
	"testing"
)

func TestElTraspasoNoFingeQuePideElCoche(t *testing.T) {
	// Devolver un viaje inventado haría creer a la interfaz que hay un coche en
	// camino cuando no lo hay. Fallar aquí es la conducta correcta.
	tp := NewTraspaso()
	_, err := tp.Request(context.Background(), RideRequest{Passengers: 2})
	if !errors.Is(err, ErrNoSoportado) {
		t.Fatalf("error = %v, esperaba ErrNoSoportado", err)
	}
}

func TestRegistrarYConsultarUnViaje(t *testing.T) {
	tp := NewTraspaso()
	ctx := context.Background()

	r, err := tp.Registrar("tesla-ride-12345")
	if err != nil {
		t.Fatalf("Registrar: %v", err)
	}
	if r.Status != RideSolicitado {
		t.Fatalf("estado = %q, esperaba solicitado", r.Status)
	}

	leido, err := tp.Status(ctx, "tesla-ride-12345")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if leido.Ref != r.Ref {
		t.Fatalf("referencia = %q, esperaba %q", leido.Ref, r.Ref)
	}
}

func TestNoSePuedeRegistrarDosVecesLaMismaReferencia(t *testing.T) {
	tp := NewTraspaso()
	if _, err := tp.Registrar("tesla-ride-1"); err != nil {
		t.Fatalf("Registrar: %v", err)
	}
	if _, err := tp.Registrar("tesla-ride-1"); err == nil {
		t.Fatal("esperaba un error al repetir la referencia")
	}
}

func TestElImporteRealSustituyeALaEstimacion(t *testing.T) {
	// El reparto definitivo tiene que hacerse sobre lo que de verdad cobró la
	// flota, no sobre la estimación de la tarifa.
	tp := NewTraspaso()
	_, _ = tp.Registrar("tesla-ride-1")

	r, err := tp.Actualizar("tesla-ride-1", RideFinalizado, 1342)
	if err != nil {
		t.Fatalf("Actualizar: %v", err)
	}
	if r.FareCents != 1342 || r.Status != RideFinalizado {
		t.Fatalf("viaje = %+v", r)
	}
}

func TestActualizarSinImporteNoBorraElYaConocido(t *testing.T) {
	tp := NewTraspaso()
	_, _ = tp.Registrar("tesla-ride-1")
	_, _ = tp.Actualizar("tesla-ride-1", RideEnCurso, 1342)

	r, err := tp.Actualizar("tesla-ride-1", RideFinalizado, 0)
	if err != nil {
		t.Fatalf("Actualizar: %v", err)
	}
	if r.FareCents != 1342 {
		t.Fatalf("importe = %d, esperaba conservar 1342", r.FareCents)
	}
}

func TestCancelarUnViaje(t *testing.T) {
	tp := NewTraspaso()
	ctx := context.Background()
	_, _ = tp.Registrar("tesla-ride-1")

	if err := tp.Cancel(ctx, "tesla-ride-1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	r, _ := tp.Status(ctx, "tesla-ride-1")
	if r.Status != RideCancelado {
		t.Fatalf("estado = %q, esperaba cancelado", r.Status)
	}
}

func TestUnaReferenciaDesconocidaFalla(t *testing.T) {
	tp := NewTraspaso()
	ctx := context.Background()
	if _, err := tp.Status(ctx, "no-existe"); !errors.Is(err, ErrViajeDesconocido) {
		t.Errorf("Status: error = %v", err)
	}
	if _, err := tp.Actualizar("no-existe", RideEnCurso, 0); !errors.Is(err, ErrViajeDesconocido) {
		t.Errorf("Actualizar: error = %v", err)
	}
	if err := tp.Cancel(ctx, "no-existe"); !errors.Is(err, ErrViajeDesconocido) {
		t.Errorf("Cancel: error = %v", err)
	}
}

func TestLasInstruccionesDistinguenElVehiculo(t *testing.T) {
	tp := NewTraspaso()
	if got := tp.Instrucciones(2); !contiene(got, "Cybercab") {
		t.Errorf("instrucciones para 2 plazas = %q", got)
	}
	if got := tp.Instrucciones(4); !contiene(got, "Model Y") {
		t.Errorf("instrucciones para 4 plazas = %q", got)
	}
}

func contiene(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
