// Package billing lleva la cuenta de quién debe qué y la liquida por periodos.
//
// Dos decisiones estructurales, ambas con motivo:
//
// No hay monedero. Lo que registra este paquete son cuentas pendientes, no
// dinero custodiado. La app nunca retiene fondos de nadie: produce
// instrucciones para un procesador ya licenciado, que es quien mueve el dinero.
// Esa distinción es lo que mantiene el servicio fuera de la licencia de
// transmisor de dinero.
//
// No se cobra viaje a viaje. La comisión fija del procesador —unos 0,30 $ por
// cargo— se lleva más de la mitad del ingreso cuando el ticket es de tres
// euros. Acumular y liquidar una vez por periodo multiplica el ingreso neto sin
// tocar el precio.
package billing

import (
	"errors"
	"time"
)

// EntryKind distingue qué representa cada apunte.
type EntryKind string

const (
	// EntryCostShare es la parte del coste del viaje que un pasajero debe a
	// quien lo organizó.
	EntryCostShare EntryKind = "cost_share"
	// EntryServiceFee es la comisión de la plataforma. No es de nadie más:
	// se separa del reparto para que quien organiza siga sin ganar dinero.
	EntryServiceFee EntryKind = "service_fee"
)

// Entry es un apunte del libro. Una vez escrito no se modifica: corregir es
// añadir el apunte contrario, nunca reescribir el original.
type Entry struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TripID    string    `json:"trip_id"`
	BookingID string    `json:"booking_id,omitempty"`
	Kind      EntryKind `json:"kind"`
	// AmountCents es siempre positivo: lo que UserID debe. La dirección la da
	// CounterpartyID, no el signo, para que no se pueda "deber negativo".
	AmountCents int64 `json:"amount_cents"`
	// CounterpartyID es a quién se le debe. Vacío en la comisión: esa se nos
	// debe a nosotros.
	CounterpartyID string    `json:"counterparty_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	// SettlementID queda vacío mientras el apunte está pendiente. Es lo que
	// impide cobrar dos veces lo mismo.
	SettlementID string `json:"settlement_id,omitempty"`
}

// Pendiente indica si el apunte aún no se ha liquidado.
func (e Entry) Pendiente() bool { return e.SettlementID == "" }

// Validate comprueba que el apunte tiene sentido antes de escribirlo.
func (e Entry) Validate() error {
	switch {
	case e.UserID == "":
		return errors.New("el apunte no tiene usuario")
	case e.AmountCents <= 0:
		// Un apunte de importe cero o negativo no significa nada: si hay que
		// devolver dinero, se escribe el apunte contrario.
		return errors.New("el importe del apunte debe ser positivo")
	case e.Kind == EntryCostShare && e.CounterpartyID == "":
		return errors.New("una parte del coste necesita saber a quién se le debe")
	case e.Kind == EntryServiceFee && e.CounterpartyID != "":
		return errors.New("la comisión no tiene contraparte: es de la plataforma")
	case e.UserID == e.CounterpartyID:
		return errors.New("nadie puede deberse dinero a sí mismo")
	}
	return nil
}
