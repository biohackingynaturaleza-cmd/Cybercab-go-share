// Package pagos es la frontera con quien mueve el dinero de verdad.
//
// La aplicación no mueve dinero: lo describe. Produce instrucciones —cobrar
// tanto a esta persona, pagar tanto a esta otra— y se las da a un procesador ya
// licenciado. Esa distinción es la que mantiene el servicio fuera de la
// licencia de transmisor de dinero, y por eso vive detrás de una interfaz en
// vez de estar cosida a la lógica de negocio.
package pagos

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/billing"
)

// Estado es en qué ha quedado un movimiento.
type Estado string

const (
	// EstadoAnotado es lo que devuelve el proveedor por defecto: el movimiento
	// está descrito y esperando a que haya un procesador de verdad. No es un
	// éxito, y no debe contarse como tal.
	EstadoAnotado Estado = "anotado"
	// EstadoEjecutado es dinero movido.
	EstadoEjecutado Estado = "ejecutado"
	// EstadoFallido es un intento que el procesador rechazó.
	EstadoFallido Estado = "fallido"
)

// Movimiento es una instrucción concreta para el procesador.
type Movimiento struct {
	// Clave identifica el movimiento de forma estable: la misma instrucción
	// reintentada tiene la misma clave, y el procesador no la cobra dos veces.
	// Es lo único que separa un reintento de un cargo duplicado.
	Clave         string
	LiquidacionID string
	UserID        string
	Tipo          billing.TipoInstruccion
	AmountCents   int64
	// Concepto es lo que verá la persona en su extracto.
	Concepto string
}

// Resultado es lo que contesta el procesador.
type Resultado struct {
	Estado Estado
	// Ref es la referencia del movimiento en el procesador, para poder
	// auditarlo sin replicar aquí sus datos.
	Ref string
	// Motivo explica un rechazo.
	Motivo string
}

// ErrSinProcesador se devuelve cuando se pide mover dinero de verdad y no hay
// procesador configurado.
var ErrSinProcesador = errors.New("no hay procesador de pagos configurado")

// Proveedor mueve el dinero de una instrucción.
type Proveedor interface {
	// Ejecutar aplica un movimiento. Tiene que ser idempotente por Clave: se
	// le va a reintentar.
	Ejecutar(ctx context.Context, m Movimiento) (*Resultado, error)
	// Nombre identifica al proveedor en el registro y en la auditoría.
	Nombre() string
}

// Anotado es el proveedor por defecto: describe el movimiento y no lo ejecuta.
//
// Es deliberado que no finja. Un proveedor de mentira que devolviera "cobrado"
// dejaría el libro diciendo que se cobró un dinero que nadie ha visto, y ese
// error solo se descubre cuando alguien reclama. Mientras no haya procesador,
// las liquidaciones se calculan, se guardan y se quedan esperando; lo que no
// hacen es dar por cobrado nada.
type Anotado struct{}

var _ Proveedor = Anotado{}

func (Anotado) Nombre() string { return "anotado" }

func (Anotado) Ejecutar(_ context.Context, m Movimiento) (*Resultado, error) {
	return &Resultado{
		Estado: EstadoAnotado,
		Motivo: "sin procesador de pagos: el movimiento queda anotado y sin ejecutar",
		Ref:    "anotado:" + m.Clave,
	}, nil
}

// ClaveDe compone la clave de idempotencia de un movimiento.
//
// Liquidación y persona: por cada periodo, a cada uno se le cobra o se le paga
// una vez. Reintentar con la misma clave no puede duplicar el cargo.
func ClaveDe(liquidacionID, userID string) string {
	return liquidacionID + ":" + userID
}

// ConceptoDe es lo que aparece en el extracto de quien lo lea.
func ConceptoDe(desde, hasta time.Time) string {
	return fmt.Sprintf("Cybercab Go Share %s", desde.Format("01/2006")+
		periodoCorto(desde, hasta))
}

// periodoCorto añade el rango solo cuando el periodo no es un mes natural: en
// el caso normal, "Cybercab Go Share 03/2026" ya lo dice todo.
func periodoCorto(desde, hasta time.Time) string {
	mesSiguiente := time.Date(desde.Year(), desde.Month(), 1, 0, 0, 0, 0, desde.Location()).AddDate(0, 1, 0)
	if desde.Day() == 1 && hasta.Equal(mesSiguiente) {
		return ""
	}
	return fmt.Sprintf(" (%s–%s)", desde.Format("02/01"), hasta.Format("02/01"))
}
