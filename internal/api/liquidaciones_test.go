package api_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

// viajeCerrado deja un viaje completado, que es lo que escribe apuntes en el
// libro: la parte del coste que Carla le debe a Ana, y la comisión.
func viajeCerrado(t *testing.T, srv *entorno) (ana, carla sesion, tripID string) {
	t.Helper()
	ana, carla, tripID = viajeEnMarcha(t, srv)
	if code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/completar", ana.Token,
		map[string]any{}, nil); code != http.StatusOK {
		t.Fatalf("cierre del viaje: código = %d", code)
	}
	return ana, carla, tripID
}

// periodoDeAhora son los parámetros de consulta del mes en curso.
func periodoDeAhora() string {
	now := time.Now().UTC()
	desde := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	q := url.Values{}
	q.Set("desde", desde.Format(time.RFC3339))
	q.Set("hasta", desde.AddDate(0, 1, 0).Format(time.RFC3339))
	return "?" + q.Encode()
}

func TestMiSaldoDesglosaLoPendiente(t *testing.T) {
	srv := newTestServer(t)
	ana, carla, _ := viajeCerrado(t, srv)

	var saldo struct {
		PendienteCents int64  `json:"pendiente_cents"`
		DebeCents      int64  `json:"debe_cents"`
		LeDebenCents   int64  `json:"le_deben_cents"`
		ComisionCents  int64  `json:"comision_cents"`
		Apuntes        int    `json:"apuntes"`
		ProximoCierre  string `json:"proximo_cierre"`
		Movimientos    []any  `json:"movimientos"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/me/saldo", carla.Token, nil, &saldo); code != http.StatusOK {
		t.Fatalf("saldo de Carla: código = %d", code)
	}
	if saldo.DebeCents <= 0 || saldo.ComisionCents <= 0 || saldo.Apuntes != 2 {
		t.Fatalf("saldo de la pasajera = %+v", saldo)
	}
	if saldo.PendienteCents != saldo.DebeCents-saldo.LeDebenCents {
		t.Fatalf("el neto no cuadra con el desglose: %+v", saldo)
	}
	if saldo.ProximoCierre == "" {
		t.Fatal("no dice cuándo se cierra el periodo en curso")
	}

	// A quien organiza le deben: su neto va en negativo.
	var delHost struct {
		PendienteCents int64 `json:"pendiente_cents"`
		LeDebenCents   int64 `json:"le_deben_cents"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/me/saldo", ana.Token, nil, &delHost); code != http.StatusOK {
		t.Fatalf("saldo de Ana: código = %d", code)
	}
	if delHost.LeDebenCents <= 0 || delHost.PendienteCents >= 0 {
		t.Fatalf("saldo de quien organiza = %+v", delHost)
	}
}

func TestElSaldoExigeSesion(t *testing.T) {
	srv := newTestServer(t)
	if code := do(t, srv, http.MethodGet, "/api/v1/me/saldo", "", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("código = %d, esperaba 401", code)
	}
}

func TestCerrarUnPeriodoDesdeOperaciones(t *testing.T) {
	srv := servidorConOperaciones(t)
	_, carla, _ := viajeCerrado(t, srv)

	var cerradas struct {
		Liquidaciones []struct {
			ID                string `json:"id"`
			Estado            string `json:"estado"`
			ApuntesLiquidados int    `json:"apuntes_liquidados"`
			Instrucciones     []struct {
				UserID string `json:"user_id"`
				Tipo   string `json:"tipo"`
				Estado string `json:"estado"`
			} `json:"instrucciones"`
		} `json:"liquidaciones"`
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/operaciones/liquidaciones/cerrar"+periodoDeAhora(),
		tokenDeOperaciones, map[string]any{}, &cerradas); code != http.StatusOK {
		t.Fatalf("cerrar: código = %d", code)
	}
	if len(cerradas.Liquidaciones) != 1 {
		t.Fatalf("liquidaciones = %d, esperaba 1", len(cerradas.Liquidaciones))
	}
	liq := cerradas.Liquidaciones[0]
	if liq.Estado != "calculada" {
		t.Fatalf("estado = %q: cerrar el periodo no puede mover dinero", liq.Estado)
	}
	if liq.ApuntesLiquidados != 2 || len(liq.Instrucciones) != 2 {
		t.Fatalf("liquidación = %+v", liq)
	}
	for _, in := range liq.Instrucciones {
		if in.Estado != "pendiente" {
			t.Fatalf("instrucción en estado %q nada más calcularla", in.Estado)
		}
	}

	// Y el saldo de Carla deja de tener pendiente, con su movimiento apuntado.
	var saldo struct {
		PendienteCents int64 `json:"pendiente_cents"`
		Movimientos    []struct {
			Tipo string `json:"tipo"`
		} `json:"movimientos"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/me/saldo", carla.Token, nil, &saldo); code != http.StatusOK {
		t.Fatalf("saldo: código = %d", code)
	}
	if saldo.PendienteCents != 0 {
		t.Fatalf("pendiente tras liquidar = %d", saldo.PendienteCents)
	}
	if len(saldo.Movimientos) != 1 || saldo.Movimientos[0].Tipo != "cobro" {
		t.Fatalf("movimientos = %+v", saldo.Movimientos)
	}
}

func TestCerrarDosVecesElMismoPeriodoDevuelve409(t *testing.T) {
	srv := servidorConOperaciones(t)
	viajeCerrado(t, srv)
	ruta := "/api/v1/operaciones/liquidaciones/cerrar" + periodoDeAhora()

	if code := do(t, srv, http.MethodPost, ruta, tokenDeOperaciones, map[string]any{}, nil); code != http.StatusOK {
		t.Fatalf("primera: código = %d", code)
	}
	// La segunda ya no tiene apuntes que liquidar en ese periodo.
	if code := do(t, srv, http.MethodPost, ruta, tokenDeOperaciones, map[string]any{}, nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("segunda: código = %d, esperaba 422", code)
	}
}

func TestEjecutarSinProcesadorAvisaDeQueNoHaCobradoNada(t *testing.T) {
	// Dar por hecho un cobro que no existe solo se descubre cuando alguien
	// reclama, así que la respuesta tiene que decirlo.
	srv := servidorConOperaciones(t)
	viajeCerrado(t, srv)

	var cerradas struct {
		Liquidaciones []struct {
			ID string `json:"id"`
		} `json:"liquidaciones"`
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/operaciones/liquidaciones/cerrar"+periodoDeAhora(),
		tokenDeOperaciones, map[string]any{}, &cerradas); code != http.StatusOK {
		t.Fatalf("cerrar: código = %d", code)
	}
	id := cerradas.Liquidaciones[0].ID

	var res struct {
		Liquidacion struct {
			Estado        string `json:"estado"`
			Instrucciones []struct {
				Estado string `json:"estado"`
			} `json:"instrucciones"`
		} `json:"liquidacion"`
		Procesador string `json:"procesador"`
		Aviso      string `json:"aviso"`
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/operaciones/liquidaciones/"+id+"/ejecutar",
		tokenDeOperaciones, map[string]any{}, &res); code != http.StatusOK {
		t.Fatalf("ejecutar: código = %d", code)
	}
	if res.Procesador != "anotado" {
		t.Fatalf("procesador = %q", res.Procesador)
	}
	if res.Aviso == "" {
		t.Fatal("no avisa de que no se ha cobrado nada")
	}
	if res.Liquidacion.Estado != "calculada" {
		t.Fatalf("estado = %q: sin procesador no se ha cobrado nada", res.Liquidacion.Estado)
	}
	for _, in := range res.Liquidacion.Instrucciones {
		if in.Estado != "anotado" {
			t.Fatalf("instrucción en estado %q, esperaba anotado", in.Estado)
		}
	}
}

func TestLasLiquidacionesSeListanConSuProcesador(t *testing.T) {
	srv := servidorConOperaciones(t)
	viajeCerrado(t, srv)
	if code := do(t, srv, http.MethodPost, "/api/v1/operaciones/liquidaciones/cerrar"+periodoDeAhora(),
		tokenDeOperaciones, map[string]any{}, nil); code != http.StatusOK {
		t.Fatalf("cerrar: código = %d", code)
	}

	var lista struct {
		Liquidaciones []map[string]any `json:"liquidaciones"`
		Procesador    string           `json:"procesador"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/operaciones/liquidaciones",
		tokenDeOperaciones, nil, &lista); code != http.StatusOK {
		t.Fatalf("listar: código = %d", code)
	}
	if len(lista.Liquidaciones) != 1 {
		t.Fatalf("liquidaciones = %d", len(lista.Liquidaciones))
	}
	// Qué procesador hay detrás es lo primero que quiere saber quien mire esto.
	if lista.Procesador != "anotado" {
		t.Fatalf("procesador = %q", lista.Procesador)
	}
}

func TestLasLiquidacionesNoSonPublicas(t *testing.T) {
	srv := servidorConOperaciones(t)
	ana, _, _ := viajeCerrado(t, srv)

	for _, caso := range []struct {
		nombre   string
		token    string
		esperado int
	}{
		{"sin token", "", http.StatusUnauthorized},
		{"con token de usuario", ana.Token, http.StatusUnauthorized},
	} {
		if code := do(t, srv, http.MethodGet, "/api/v1/operaciones/liquidaciones",
			caso.token, nil, nil); code != caso.esperado {
			t.Fatalf("%s: código = %d, esperaba %d", caso.nombre, code, caso.esperado)
		}
	}

	// Y sin OPS_TOKEN configurado las rutas no existen siquiera.
	sinOps := newTestServer(t)
	if code := do(t, sinOps, http.MethodGet, "/api/v1/operaciones/liquidaciones", "", nil, nil); code != http.StatusNotFound {
		t.Fatalf("sin panel de operaciones: código = %d, esperaba 404", code)
	}
}

func TestCerrarSinPeriodoNoTocaElMesEnCurso(t *testing.T) {
	// Sin parámetros hace lo que el programador: cerrar lo vencido. El mes en
	// curso todavía puede recibir apuntes.
	srv := servidorConOperaciones(t)
	viajeCerrado(t, srv)

	var cerradas struct {
		Liquidaciones []any `json:"liquidaciones"`
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/operaciones/liquidaciones/cerrar",
		tokenDeOperaciones, map[string]any{}, &cerradas); code != http.StatusOK {
		t.Fatalf("cerrar: código = %d", code)
	}
	if len(cerradas.Liquidaciones) != 0 {
		t.Fatalf("cerró %d periodos: el del viaje es el mes en curso", len(cerradas.Liquidaciones))
	}
}
