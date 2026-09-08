package api_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/api"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/auth"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// tokenDeOperaciones es el secreto del panel en las pruebas.
const tokenDeOperaciones = "secreto-de-operaciones-para-pruebas"

// servidorConOperaciones monta el servidor con la cola de revisión abierta.
func servidorConOperaciones(t *testing.T) *entorno {
	t.Helper()
	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	tokens, err := auth.NewTokenIssuer(secret, time.Hour)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	identidad := trust.NewManual("http://test")
	svc := service.New(store.NewMemory(), service.Config{Tokens: tokens, Identidad: identidad})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(api.NewServer(svc, tokens, log,
		api.WithDevIdentityResolver(identidad), api.WithPanelDeOperaciones(tokenDeOperaciones)))
	t.Cleanup(srv.Close)
	return &entorno{Server: srv, identidad: identidad}
}

// viajeHecho deja un trayecto completado con dos personas dentro, que es el
// único punto desde el que se puede valorar o denunciar.
func viajeHecho(t *testing.T, srv *entorno) (ana, carla sesion, bookingID string) {
	t.Helper()
	ana = registrar(t, srv, "Ana", "ana@example.com")
	carla = registrar(t, srv, "Carla", "carla@example.com")
	tripID := publicarTrayecto(t, srv, ana.Token)

	var booking map[string]any
	if code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/bookings", carla.Token, map[string]any{
		"pickup": lugar("Riverside", riverside), "dropoff": lugar("AUS", aus),
	}, &booking); code != http.StatusCreated {
		t.Fatalf("reserva: código = %d (%v)", code, booking)
	}
	bookingID = booking["id"].(string)

	if code := do(t, srv, http.MethodPost, "/api/v1/bookings/"+bookingID+"/decision", ana.Token,
		map[string]any{"accept": true, "acepta_responsabilidad": true}, nil); code != http.StatusOK {
		t.Fatalf("confirmación: código = %d", code)
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/completar", ana.Token,
		map[string]any{}, nil); code != http.StatusOK {
		t.Fatalf("cierre: código = %d", code)
	}
	return ana, carla, bookingID
}

func valoracionesDe(t *testing.T, srv *entorno, userID string) []map[string]any {
	t.Helper()
	var body struct {
		Valoraciones []map[string]any `json:"valoraciones"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/users/"+userID+"/valoraciones", "", nil, &body); code != http.StatusOK {
		t.Fatalf("valoraciones de %s: código = %d", userID, code)
	}
	return body.Valoraciones
}

func TestValorarNoPublicaHastaQueValoranLosDos(t *testing.T) {
	srv := servidorConOperaciones(t)
	ana, carla, bookingID := viajeHecho(t, srv)

	if code := do(t, srv, http.MethodPost, "/api/v1/bookings/"+bookingID+"/valoracion", ana.Token,
		map[string]any{"estrellas": 2, "comentario": "Llegó veinte minutos tarde"}, nil); code != http.StatusCreated {
		t.Fatalf("valorar: código = %d", code)
	}
	if vs := valoracionesDe(t, srv, carla.User.ID); len(vs) != 0 {
		t.Fatalf("visibles = %d, esperaba ninguna con una sola valoración", len(vs))
	}

	if code := do(t, srv, http.MethodPost, "/api/v1/bookings/"+bookingID+"/valoracion", carla.Token,
		map[string]any{"estrellas": 5}, nil); code != http.StatusCreated {
		t.Fatalf("valorar (Carla): código = %d", code)
	}

	deCarla := valoracionesDe(t, srv, carla.User.ID)
	if len(deCarla) != 1 || deCarla[0]["estrellas"].(float64) != 2 {
		t.Fatalf("valoraciones de Carla = %v", deCarla)
	}
	deAna := valoracionesDe(t, srv, ana.User.ID)
	if len(deAna) != 1 || deAna[0]["estrellas"].(float64) != 5 {
		t.Fatalf("valoraciones de Ana = %v", deAna)
	}
}

func TestValorarDosVecesDevuelve409(t *testing.T) {
	srv := servidorConOperaciones(t)
	ana, _, bookingID := viajeHecho(t, srv)
	cuerpo := map[string]any{"estrellas": 5}

	if code := do(t, srv, http.MethodPost, "/api/v1/bookings/"+bookingID+"/valoracion", ana.Token, cuerpo, nil); code != http.StatusCreated {
		t.Fatalf("primera: código = %d", code)
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/bookings/"+bookingID+"/valoracion", ana.Token, cuerpo, nil); code != http.StatusConflict {
		t.Fatalf("segunda: código = %d, esperaba 409", code)
	}
}

func TestElViajeHechoSaleComoPendienteDeValorar(t *testing.T) {
	srv := servidorConOperaciones(t)
	ana, carla, _ := viajeHecho(t, srv)

	var body struct {
		Pendientes []struct {
			SobreID string `json:"sobre_id"`
			Nombre  string `json:"nombre"`
		} `json:"pendientes"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/me/valoraciones/pendientes", ana.Token, nil, &body); code != http.StatusOK {
		t.Fatalf("código = %d", code)
	}
	if len(body.Pendientes) != 1 || body.Pendientes[0].SobreID != carla.User.ID {
		t.Fatalf("pendientes = %+v", body.Pendientes)
	}
}

func TestDenunciarYSuspenderDePuntaAPunta(t *testing.T) {
	srv := servidorConOperaciones(t)
	ana, carla, _ := viajeHecho(t, srv)

	var denuncia map[string]any
	if code := do(t, srv, http.MethodPost, "/api/v1/users/"+carla.User.ID+"/denunciar", ana.Token,
		map[string]any{
			"motivo":      "seguridad",
			"descripcion": "Se puso agresiva al bajar y me siguió hasta la terminal.",
		}, &denuncia); code != http.StatusCreated {
		t.Fatalf("denunciar: código = %d (%v)", code, denuncia)
	}

	// La cola de operaciones la ve; nadie más.
	if code := do(t, srv, http.MethodGet, "/api/v1/operaciones/denuncias", ana.Token, nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("con un token de usuario: código = %d, esperaba 401", code)
	}
	var cola struct {
		Denuncias []map[string]any `json:"denuncias"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/operaciones/denuncias", tokenDeOperaciones, nil, &cola); code != http.StatusOK {
		t.Fatalf("cola: código = %d", code)
	}
	if len(cola.Denuncias) != 1 {
		t.Fatalf("cola = %d, esperaba 1", len(cola.Denuncias))
	}

	// Confirmarla suspende a Carla, y suspendida no se sube a ningún coche.
	id := denuncia["id"].(string)
	if code := do(t, srv, http.MethodPost, "/api/v1/operaciones/denuncias/"+id+"/resolver", tokenDeOperaciones,
		map[string]any{"confirmada": true, "resolucion": "Confirmada tras revisar el relato."}, nil); code != http.StatusOK {
		t.Fatalf("resolver: código = %d", code)
	}

	otro := registrar(t, srv, "Diego", "diego@example.com")
	tripID := publicarTrayecto(t, srv, otro.Token)
	var problema map[string]any
	code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/bookings", carla.Token, map[string]any{
		"pickup": lugar("Riverside", riverside), "dropoff": lugar("AUS", aus),
	}, &problema)
	if code != http.StatusForbidden {
		t.Fatalf("reserva de una cuenta suspendida: código = %d, esperaba 403", code)
	}
	if problema["codigo"] != "cuenta_suspendida" {
		t.Fatalf("codigo = %v, esperaba cuenta_suspendida", problema["codigo"])
	}

	// Y se puede deshacer: una suspensión irreversible convierte cada error en
	// definitivo.
	if code := do(t, srv, http.MethodPost, "/api/v1/operaciones/usuarios/"+carla.User.ID+"/levantar-suspension",
		tokenDeOperaciones, map[string]any{}, nil); code != http.StatusOK {
		t.Fatalf("levantar: código = %d", code)
	}
	if code := do(t, srv, http.MethodPost, "/api/v1/trips/"+tripID+"/bookings", carla.Token, map[string]any{
		"pickup": lugar("Riverside", riverside), "dropoff": lugar("AUS", aus),
	}, nil); code != http.StatusCreated {
		t.Fatalf("tras levantar la suspensión: código = %d", code)
	}
}

func TestNadiePuedeConsultarLasDenunciasQueTieneEnContra(t *testing.T) {
	// No hay ruta que las enseñe, y la de "mis denuncias" solo trae las
	// propias: enterarse de que te han denunciado es lo que hace que la
	// siguiente persona no denuncie.
	srv := servidorConOperaciones(t)
	ana, carla, _ := viajeHecho(t, srv)
	if code := do(t, srv, http.MethodPost, "/api/v1/users/"+carla.User.ID+"/denunciar", ana.Token,
		map[string]any{"motivo": "conducta", "descripcion": "Fue muy desagradable durante todo el trayecto."},
		nil); code != http.StatusCreated {
		t.Fatalf("denunciar: código = %d", code)
	}

	var body struct {
		Denuncias []map[string]any `json:"denuncias"`
	}
	if code := do(t, srv, http.MethodGet, "/api/v1/me/denuncias", carla.Token, nil, &body); code != http.StatusOK {
		t.Fatalf("código = %d", code)
	}
	if len(body.Denuncias) != 0 {
		t.Fatalf("la denunciada ve %d denuncias, esperaba ninguna", len(body.Denuncias))
	}
}

func TestSinTokenDeOperacionesLaColaNoExiste(t *testing.T) {
	// El servidor normal no la monta: devolver 404 y no 403 evita confirmar
	// siquiera que estas rutas están ahí.
	srv := newTestServer(t)
	if code := do(t, srv, http.MethodGet, "/api/v1/operaciones/denuncias", "", nil, nil); code != http.StatusNotFound {
		t.Fatalf("código = %d, esperaba 404", code)
	}
}

func TestUnTokenDeOperacionesEquivocadoNoEntra(t *testing.T) {
	srv := servidorConOperaciones(t)
	for _, malo := range []string{"", "otro-secreto", tokenDeOperaciones + "x"} {
		esperado := http.StatusUnauthorized
		if code := do(t, srv, http.MethodGet, "/api/v1/operaciones/denuncias", malo, nil, nil); code != esperado {
			t.Fatalf("token %q: código = %d, esperaba %d", malo, code, esperado)
		}
	}
}
