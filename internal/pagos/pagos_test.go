package pagos_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/billing"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pagos"
)

func TestElProveedorPorDefectoNuncaDiceQueHaCobrado(t *testing.T) {
	// Es lo único que este proveedor tiene que garantizar. Uno que devolviera
	// "ejecutado" dejaría el libro diciendo que se cobró un dinero que nadie ha
	// visto, y eso solo se descubre cuando alguien reclama.
	res, err := pagos.Anotado{}.Ejecutar(context.Background(), pagos.Movimiento{
		Clave: "liq_1:usr_1", UserID: "usr_1",
		Tipo: billing.Cobro, AmountCents: 1440,
	})
	if err != nil {
		t.Fatalf("Ejecutar: %v", err)
	}
	if res.Estado != pagos.EstadoAnotado {
		t.Fatalf("estado = %q, esperaba anotado", res.Estado)
	}
	if res.Estado == pagos.EstadoEjecutado {
		t.Fatal("el proveedor por defecto dio un cobro por hecho")
	}
	if res.Motivo == "" {
		t.Fatal("no explica por qué se queda sin ejecutar")
	}
}

func TestLaClaveDistingueAPersonaYPeriodo(t *testing.T) {
	// La clave es lo único que separa un reintento de un cargo duplicado: dos
	// movimientos distintos no pueden compartirla, y el mismo repetido tiene
	// que dar siempre la misma.
	misma := pagos.ClaveDe("liq_1", "usr_1")
	if misma != pagos.ClaveDe("liq_1", "usr_1") {
		t.Fatal("la misma instrucción da claves distintas: un reintento cobraría dos veces")
	}
	for _, otra := range []string{
		pagos.ClaveDe("liq_1", "usr_2"),
		pagos.ClaveDe("liq_2", "usr_1"),
	} {
		if otra == misma {
			t.Fatalf("dos movimientos distintos comparten la clave %q", misma)
		}
	}
}

func TestElConceptoDiceDeQuePeriodoEs(t *testing.T) {
	// Es lo que la persona verá en su extracto: tiene que poder reconocerlo sin
	// entrar en la app.
	marzo := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	abril := marzo.AddDate(0, 1, 0)

	mesEntero := pagos.ConceptoDe(marzo, abril)
	if !strings.Contains(mesEntero, "03/2026") {
		t.Fatalf("concepto = %q, esperaba que dijera el mes", mesEntero)
	}
	// Un mes natural no necesita el rango: "Cybercab Go Share 03/2026" ya lo
	// dice todo, y el paréntesis solo sería ruido en el extracto.
	if strings.Contains(mesEntero, "(") {
		t.Fatalf("concepto = %q: un mes natural no lleva rango", mesEntero)
	}

	// Un periodo que no es un mes natural sí lo lleva, o no se sabría cuál es.
	parcial := pagos.ConceptoDe(marzo.AddDate(0, 0, 9), marzo.AddDate(0, 0, 20))
	if !strings.Contains(parcial, "(") {
		t.Fatalf("concepto = %q: un periodo suelto necesita decir sus fechas", parcial)
	}
}
