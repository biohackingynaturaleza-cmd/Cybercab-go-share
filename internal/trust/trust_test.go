package trust

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

var ahora = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

// verificada construye una comprobación superada, opcionalmente con caducidad.
func verificada(kind CheckKind, expira time.Time) Check {
	return Check{
		ID: "chk_" + string(kind), UserID: "usr_1", Kind: kind,
		Status: StatusVerified, VerifiedAt: ahora.Add(-24 * time.Hour), ExpiresAt: expira,
	}
}

// --- Comprobaciones ---

func TestCheckActive(t *testing.T) {
	cases := map[string]struct {
		check Check
		want  bool
	}{
		"verificada sin caducidad": {verificada(CheckPhone, time.Time{}), true},
		"verificada y vigente":     {verificada(CheckGovernmentID, ahora.Add(time.Hour)), true},
		"verificada pero caducada": {verificada(CheckGovernmentID, ahora.Add(-time.Hour)), false},
		"pendiente":                {Check{Kind: CheckPhone, Status: StatusPending}, false},
		"rechazada":                {Check{Kind: CheckPhone, Status: StatusRejected}, false},
	}
	for name, tc := range cases {
		if got := tc.check.Active(ahora); got != tc.want {
			t.Errorf("%s: Active = %v, esperaba %v", name, got, tc.want)
		}
	}
}

func TestUnDocumentoCaducadoDejaDeContarSolo(t *testing.T) {
	// Nadie tiene que pasar por la base de datos marcando documentos vencidos:
	// el nivel se cae solo en cuanto el documento caduca.
	checks := []Check{
		verificada(CheckEmail, time.Time{}),
		verificada(CheckPhone, time.Time{}),
		verificada(CheckSelfie, time.Time{}),
		verificada(CheckGovernmentID, ahora.Add(time.Hour)),
	}
	if got := LevelOf(checks, Stats{}, ahora); got != LevelVerificado {
		t.Fatalf("antes de caducar = %v, esperaba verificado", got)
	}
	// Una hora después, el mismo conjunto de comprobaciones ya no basta.
	if got := LevelOf(checks, Stats{}, ahora.Add(2*time.Hour)); got != LevelBasico {
		t.Fatalf("tras caducar = %v, esperaba básico", got)
	}
}

// --- Niveles ---

func TestLevelOf(t *testing.T) {
	email := verificada(CheckEmail, time.Time{})
	phone := verificada(CheckPhone, time.Time{})
	id := verificada(CheckGovernmentID, time.Time{})
	selfie := verificada(CheckSelfie, time.Time{})

	buenHistorial := Stats{CompletedTrips: 8, Rating: 4.8, RatingCount: 6}

	cases := map[string]struct {
		checks []Check
		stats  Stats
		want   Level
	}{
		"sin nada":                 {nil, Stats{}, LevelNuevo},
		"solo email":               {[]Check{email}, Stats{}, LevelNuevo},
		"solo teléfono":            {[]Check{phone}, Stats{}, LevelNuevo},
		"email y teléfono":         {[]Check{email, phone}, Stats{}, LevelBasico},
		"documento sin selfie":     {[]Check{email, phone, id}, Stats{}, LevelBasico},
		"selfie sin documento":     {[]Check{email, phone, selfie}, Stats{}, LevelBasico},
		"identidad completa":       {[]Check{email, phone, id, selfie}, Stats{}, LevelVerificado},
		"verificado con historial": {[]Check{email, phone, id, selfie}, buenHistorial, LevelVeterano},
		"historial sin identidad":  {[]Check{email, phone}, buenHistorial, LevelBasico},
	}
	for name, tc := range cases {
		if got := LevelOf(tc.checks, tc.stats, ahora); got != tc.want {
			t.Errorf("%s: nivel = %v, esperaba %v", name, got, tc.want)
		}
	}
}

func TestElDocumentoSoloNoAcreditaIdentidad(t *testing.T) {
	// Un documento demuestra que el documento existe, no que quien lo enseña
	// sea su titular. Sin selfie con prueba de vida, no hay identidad
	// acreditada: es lo que impide usar el carné de otra persona.
	checks := []Check{
		verificada(CheckEmail, time.Time{}),
		verificada(CheckPhone, time.Time{}),
		verificada(CheckGovernmentID, time.Time{}),
	}
	if got := LevelOf(checks, Stats{}, ahora); got == LevelVerificado {
		t.Fatal("un documento sin selfie no puede dar por acreditada la identidad")
	}
}

func TestVeteranoExigeHistorialSuficiente(t *testing.T) {
	completa := []Check{
		verificada(CheckEmail, time.Time{}),
		verificada(CheckPhone, time.Time{}),
		verificada(CheckGovernmentID, time.Time{}),
		verificada(CheckSelfie, time.Time{}),
	}
	cases := map[string]Stats{
		"pocos viajes":       {CompletedTrips: 2, Rating: 5, RatingCount: 5},
		"pocas valoraciones": {CompletedTrips: 9, Rating: 5, RatingCount: 1},
		"valoración baja":    {CompletedTrips: 9, Rating: 3.2, RatingCount: 7},
	}
	for name, stats := range cases {
		if got := LevelOf(completa, stats, ahora); got != LevelVerificado {
			t.Errorf("%s: nivel = %v, esperaba quedarse en verificado", name, got)
		}
	}
}

func TestMissingDiceQueFalta(t *testing.T) {
	checks := []Check{verificada(CheckEmail, time.Time{})}

	faltan := Missing(checks, LevelVerificado, ahora)
	quiero := map[CheckKind]bool{CheckPhone: true, CheckGovernmentID: true, CheckSelfie: true}
	if len(faltan) != len(quiero) {
		t.Fatalf("faltan = %v, esperaba %d comprobaciones", faltan, len(quiero))
	}
	for _, k := range faltan {
		if !quiero[k] {
			t.Errorf("no esperaba que faltara %s", k)
		}
	}

	if faltan := Missing(checks, LevelNuevo, ahora); len(faltan) != 0 {
		t.Errorf("para el nivel nuevo no debería faltar nada, faltan %v", faltan)
	}
}

func TestNivelIdaYVueltaEnJSON(t *testing.T) {
	for _, nivel := range []Level{LevelNuevo, LevelBasico, LevelVerificado, LevelVeterano} {
		raw, err := json.Marshal(nivel)
		if err != nil {
			t.Fatalf("Marshal(%v): %v", nivel, err)
		}
		var vuelta Level
		if err := json.Unmarshal(raw, &vuelta); err != nil {
			t.Fatalf("Unmarshal(%s): %v", raw, err)
		}
		if vuelta != nivel {
			t.Errorf("%v -> %s -> %v", nivel, raw, vuelta)
		}
	}
}

func TestElNivelSeSerializaComoTexto(t *testing.T) {
	// Un número desnudo en la API sería ilegible y se rompería al insertar un
	// nivel intermedio.
	raw, _ := json.Marshal(LevelVerificado)
	if string(raw) != `"verificado"` {
		t.Fatalf("JSON = %s, esperaba \"verificado\"", raw)
	}
}

// --- Suelo por vehículo ---

func TestElBiplazaExigeIdentidadVerificada(t *testing.T) {
	// La regla central de seguridad: dos plazas es un cara a cara sin testigos.
	if got := SueloPorVehiculo(2); got != LevelVerificado {
		t.Fatalf("suelo del biplaza = %v, esperaba verificado", got)
	}
	if got := SueloPorVehiculo(4); got != LevelBasico {
		t.Fatalf("suelo de cuatro plazas = %v, esperaba básico", got)
	}
}

func TestQuienOrganizaPuedeSubirElListonPeroNoBajarlo(t *testing.T) {
	// En un biplaza, pedir "nuevo" no rebaja el suelo.
	if got := Requisito(LevelNuevo, 2); got != LevelVerificado {
		t.Errorf("requisito = %v, el suelo del biplaza no se puede rebajar", got)
	}
	// Pero sí se puede exigir más de lo que marca el suelo.
	if got := Requisito(LevelVeterano, 4); got != LevelVeterano {
		t.Errorf("requisito = %v, esperaba veterano", got)
	}
	if got := Requisito(LevelVerificado, 4); got != LevelVerificado {
		t.Errorf("requisito = %v, esperaba verificado", got)
	}
}

// --- Proveedor ---

func TestManualAbreVerificacionesPendientes(t *testing.T) {
	m := NewManual("http://localhost:8080")
	ctx := context.Background()

	sess, err := m.Start(ctx, "usr_1", CheckGovernmentID)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if sess.Ref == "" || sess.RedirectURL == "" {
		t.Fatalf("sesión incompleta: %+v", sess)
	}

	out, err := m.Result(ctx, sess.Ref)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if out.Status != StatusPending {
		t.Fatalf("estado = %q, esperaba pendiente", out.Status)
	}
}

func TestManualPermiteResolverAMano(t *testing.T) {
	m := NewManual("http://localhost:8080")
	ctx := context.Background()
	sess, _ := m.Start(ctx, "usr_1", CheckGovernmentID)

	caduca := ahora.Add(5 * 365 * 24 * time.Hour)
	if err := m.Resolve(sess.Ref, Outcome{Status: StatusVerified, DocumentExpiresAt: caduca}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	out, _ := m.Result(ctx, sess.Ref)
	if out.Status != StatusVerified || !out.DocumentExpiresAt.Equal(caduca) {
		t.Fatalf("veredicto = %+v", out)
	}
}

func TestManualRechazaTiposDesconocidos(t *testing.T) {
	m := NewManual("http://localhost:8080")
	if _, err := m.Start(context.Background(), "usr_1", CheckKind("huella_dactilar")); err == nil {
		t.Fatal("esperaba un error con un tipo de comprobación desconocido")
	}
}

func TestManualNoConoceVerificacionesAjenas(t *testing.T) {
	m := NewManual("http://localhost:8080")
	if _, err := m.Result(context.Background(), "manual_inventado"); err == nil {
		t.Fatal("esperaba un error con una referencia inexistente")
	}
}
