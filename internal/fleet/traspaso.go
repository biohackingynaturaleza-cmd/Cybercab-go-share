package fleet

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Traspaso es el proveedor que funciona hoy: la app no pide el coche, lo pide
// una persona en la app de Tesla y registra aquí la referencia del viaje.
//
// No es un apaño provisional disfrazado: es la única forma honesta de operar
// mientras no exista una API pública. Lo que sí evita es mentir sobre lo que
// la app controla — Request falla explícitamente en vez de fingir que ha
// pedido un coche.
type Traspaso struct {
	mu    sync.RWMutex
	rides map[string]*Ride
	now   func() time.Time
}

// NewTraspaso construye el proveedor de traspaso.
func NewTraspaso() *Traspaso {
	return &Traspaso{
		rides: map[string]*Ride{},
		now:   func() time.Time { return time.Now().UTC() },
	}
}

var _ Provider = (*Traspaso)(nil)

// Nombre identifica al proveedor.
func (t *Traspaso) Nombre() string { return "traspaso_manual" }

// Request no pide nada: devuelve ErrNoSoportado porque el vehículo lo pide una
// persona. Fallar aquí es deliberado; devolver un viaje inventado haría creer a
// la interfaz que hay un coche en camino.
func (t *Traspaso) Request(context.Context, RideRequest) (*Ride, error) {
	return nil, fmt.Errorf("%w: el vehículo se pide desde la app de Tesla y luego se registra su referencia aquí", ErrNoSoportado)
}

// Registrar guarda la referencia del viaje que la persona ha pedido en la app
// de Tesla. Es el sustituto de Request en el modo de traspaso.
func (t *Traspaso) Registrar(ref string) (*Ride, error) {
	if ref == "" {
		return nil, errors.New("hace falta la referencia del viaje")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.rides[ref]; ok {
		return nil, errors.New("esa referencia ya está registrada")
	}
	r := &Ride{Ref: ref, Status: RideSolicitado, UpdatedAt: t.now()}
	t.rides[ref] = r
	cp := *r
	return &cp, nil
}

// Status devuelve lo último que se sabe del viaje. En modo traspaso lo que se
// sabe es lo que la persona haya ido contando: no hay telemetría.
func (t *Traspaso) Status(_ context.Context, ref string) (*Ride, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	r, ok := t.rides[ref]
	if !ok {
		return nil, ErrViajeDesconocido
	}
	cp := *r
	return &cp, nil
}

// Actualizar refleja lo que la persona informa sobre el viaje.
func (t *Traspaso) Actualizar(ref string, status RideStatus, fareCents int64) (*Ride, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.rides[ref]
	if !ok {
		return nil, ErrViajeDesconocido
	}
	r.Status = status
	if fareCents > 0 {
		// El importe real de la flota sustituye a la estimación: el reparto
		// definitivo debe hacerse sobre lo que de verdad se ha cobrado.
		r.FareCents = fareCents
	}
	r.UpdatedAt = t.now()
	cp := *r
	return &cp, nil
}

// Cancel marca el viaje como anulado. No cancela nada en Tesla: eso lo hace la
// persona desde su app.
func (t *Traspaso) Cancel(_ context.Context, ref string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.rides[ref]
	if !ok {
		return ErrViajeDesconocido
	}
	r.Status = RideCancelado
	r.UpdatedAt = t.now()
	return nil
}

// ErrViajeDesconocido se devuelve al consultar una referencia no registrada.
var ErrViajeDesconocido = errors.New("no hay ningún viaje con esa referencia")

// Instrucciones explica lo que la persona tiene que hacer, para poder mostrarlo
// en la interfaz en lugar de dejarla adivinando.
func (t *Traspaso) Instrucciones(pasajeros int) string {
	vehiculo := "un Model Y"
	if pasajeros <= 2 {
		vehiculo = "un Cybercab"
	}
	return fmt.Sprintf(
		"Pide %s en la app de Tesla con el mismo origen y destino, y pega aquí la "+
			"referencia del viaje. Cuando termine, indica el importe real para "+
			"cuadrar el reparto.", vehiculo)
}
