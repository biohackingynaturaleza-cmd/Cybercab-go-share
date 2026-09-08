package domain

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// MotivoDenuncia es por qué se denuncia a alguien.
//
// La lista es corta y concreta a propósito: un campo libre produce denuncias
// que nadie sabe clasificar ni priorizar, y aquí lo primero que hay que saber
// de una denuncia es si alguien corre peligro.
type MotivoDenuncia string

const (
	// DenunciaSeguridad es agresión, amenaza o acoso. Va primero porque es la
	// única que no puede esperar a la cola normal.
	DenunciaSeguridad MotivoDenuncia = "seguridad"
	// DenunciaIdentidad es que quien se presentó no era quien decía ser. Ataca
	// el cimiento del servicio: sin conductor, la identidad es todo lo que hay.
	DenunciaIdentidad MotivoDenuncia = "identidad"
	// DenunciaConducta cubre el trato irrespetuoso y las normas del vehículo.
	DenunciaConducta MotivoDenuncia = "conducta"
	// DenunciaNoPresentado es no aparecer y dejar a la otra parte tirada.
	DenunciaNoPresentado MotivoDenuncia = "no_presentado"
	// DenunciaCobro es intentar sacar dinero del viaje o negarse a pagar.
	DenunciaCobro MotivoDenuncia = "cobro"
	// DenunciaOtra es lo que no encaja en las anteriores.
	DenunciaOtra MotivoDenuncia = "otra"
)

// Valid indica si el motivo es conocido.
func (m MotivoDenuncia) Valid() bool {
	switch m {
	case DenunciaSeguridad, DenunciaIdentidad, DenunciaConducta,
		DenunciaNoPresentado, DenunciaCobro, DenunciaOtra:
		return true
	}
	return false
}

// Urgente distingue lo que hay que mirar hoy de lo que puede esperar turno.
func (m MotivoDenuncia) Urgente() bool {
	return m == DenunciaSeguridad || m == DenunciaIdentidad
}

// EstadoDenuncia es en qué punto está su revisión.
type EstadoDenuncia string

const (
	// DenunciaAbierta espera revisión.
	DenunciaAbierta EstadoDenuncia = "abierta"
	// DenunciaConfirmada se ha revisado y se le ha dado la razón.
	DenunciaConfirmada EstadoDenuncia = "confirmada"
	// DenunciaDesestimada se ha revisado y no procede.
	DenunciaDesestimada EstadoDenuncia = "desestimada"
)

// Denuncia es lo que alguien cuenta sobre otra persona con quien compartió
// viaje.
//
// Nunca es pública ni se le enseña a quien la recibe. Una denuncia que llega a
// oídos del denunciado es una denuncia que nadie pone: quien tuvo miedo dentro
// del coche no va a arriesgarse a que además se entere.
type Denuncia struct {
	ID            string         `json:"id"`
	TripID        string         `json:"trip_id,omitempty"`
	DenuncianteID string         `json:"denunciante_id"`
	DenunciadoID  string         `json:"denunciado_id"`
	Motivo        MotivoDenuncia `json:"motivo"`
	Descripcion   string         `json:"descripcion"`
	Estado        EstadoDenuncia `json:"estado"`
	CreatedAt     time.Time      `json:"created_at"`
	ResueltaAt    time.Time      `json:"resuelta_at,omitzero"`
	// Resolucion es lo que decidió quien la revisó. Solo la ve operaciones y
	// quien denunció, nunca el denunciado.
	Resolucion string `json:"resolucion,omitempty"`
}

// MaxDescripcionDenuncia acota el relato.
const MaxDescripcionDenuncia = 2000

// MinDescripcionDenuncia obliga a contar algo. Una denuncia sin relato no se
// puede revisar, y no revisarla es peor que no tenerla.
const MinDescripcionDenuncia = 10

// Validate comprueba la denuncia antes de guardarla.
func (d *Denuncia) Validate() error {
	n := utf8.RuneCountInString(d.Descripcion)
	switch {
	case d.DenuncianteID == "" || d.DenunciadoID == "":
		return errors.New("faltan datos de la denuncia")
	case d.DenuncianteID == d.DenunciadoID:
		return errors.New("no puedes denunciarte a ti mismo")
	case !d.Motivo.Valid():
		return errors.New("motivo de denuncia desconocido")
	case n < MinDescripcionDenuncia:
		return fmt.Errorf("cuenta qué pasó: hacen falta al menos %d caracteres", MinDescripcionDenuncia)
	case n > MaxDescripcionDenuncia:
		return fmt.Errorf("el relato no puede pasar de %d caracteres", MaxDescripcionDenuncia)
	}
	return nil
}

// SuspensionPorDenuncia es cuánto dura la suspensión por defecto cuando se
// confirma una denuncia. Operaciones puede fijar otra.
const SuspensionPorDenuncia = 30 * 24 * time.Hour

// SuspensionPermanente es la que no vence. Se expresa como una fecha lejana en
// vez de un booleano aparte para que toda la lógica de suspensión sea una sola
// comparación de fechas.
var SuspensionPermanente = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
