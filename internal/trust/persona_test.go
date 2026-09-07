package trust

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

const secretoDePrueba = "whsec_de_prueba_1234567890"

// servidorPersona levanta un Persona de mentira que responde lo que se le diga.
func servidorPersona(t *testing.T, respuestas map[string]string) (*Persona, *[]string) {
	t.Helper()
	var vistas []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vistas = append(vistas, r.Method+" "+r.URL.Path+" | "+
			r.Header.Get("Authorization")+" | "+r.Header.Get("Persona-Version")+" | "+
			r.Header.Get("Key-Inflection"))

		// Se prueban los patrones de más largo a más corto: recorrer el mapa
		// tal cual dejaba que "/inquiries" ganara a
		// "generate-one-time-link" según el orden aleatorio del mapa, y la
		// prueba pasaba o fallaba por suerte.
		patrones := make([]string, 0, len(respuestas))
		for patron := range respuestas {
			patrones = append(patrones, patron)
		}
		sort.Slice(patrones, func(i, j int) bool { return len(patrones[i]) > len(patrones[j]) })

		for _, patron := range patrones {
			if strings.Contains(r.URL.Path, patron) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(respuestas[patron]))
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	p, err := NewPersona(PersonaConfig{
		APIKey:        "persona_api_key",
		WebhookSecret: secretoDePrueba,
		BaseURL:       srv.URL,
		Plantillas: map[CheckKind]string{
			CheckGovernmentID: "itmpl_documento_y_cara",
			CheckSelfie:       "itmpl_documento_y_cara",
			CheckPhone:        "itmpl_telefono",
		},
	})
	if err != nil {
		t.Fatalf("NewPersona: %v", err)
	}
	return p, &vistas
}

// --- Configuración ---

func TestNewPersonaExigeLoImprescindible(t *testing.T) {
	casos := map[string]PersonaConfig{
		"sin clave":      {Plantillas: map[CheckKind]string{CheckPhone: "x"}},
		"sin plantillas": {APIKey: "k"},
		"plantilla de un tipo inventado": {
			APIKey:     "k",
			Plantillas: map[CheckKind]string{CheckKind("huella"): "x"},
		},
	}
	for nombre, cfg := range casos {
		if _, err := NewPersona(cfg); err == nil {
			t.Errorf("%s: esperaba un error", nombre)
		}
	}
}

// --- Abrir una verificación ---

func TestStartCreaLaVerificacionYDevuelveElEnlace(t *testing.T) {
	p, vistas := servidorPersona(t, map[string]string{
		"generate-one-time-link": `{"meta":{"one-time-link":"https://x.withpersona.com/verify?i=abc",
			"one-time-link-expires-at":"2026-09-08T10:00:00Z"}}`,
		"/inquiries": `{"data":{"id":"inq_123","attributes":{"status":"created"}}}`,
	})

	sess, err := p.Start(context.Background(), "usr_9", CheckGovernmentID)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if sess.Ref != "inq_123" {
		t.Errorf("Ref = %q, esperaba inq_123", sess.Ref)
	}
	if !strings.HasPrefix(sess.RedirectURL, "https://x.withpersona.com/verify") {
		t.Errorf("enlace = %q", sess.RedirectURL)
	}
	if sess.ExpiresAt.IsZero() {
		t.Error("el enlace no tiene caducidad")
	}

	// Las cabeceras fijan la versión y la forma de las claves: sin ellas, un
	// cambio del proveedor alteraría las respuestas sin aviso.
	primera := (*vistas)[0]
	for _, esperada := range []string{"Bearer persona_api_key", PersonaVersion, "kebab"} {
		if !strings.Contains(primera, esperada) {
			t.Errorf("falta %q en las cabeceras: %s", esperada, primera)
		}
	}
}

func TestStartRechazaUnTipoSinPlantilla(t *testing.T) {
	p, _ := servidorPersona(t, nil)
	if _, err := p.Start(context.Background(), "usr_1", CheckPayment); err == nil {
		t.Fatal("esperaba un error: no hay plantilla para ese tipo")
	}
}

func TestStartFallaSiPersonaNoDevuelveEnlace(t *testing.T) {
	p, _ := servidorPersona(t, map[string]string{
		"generate-one-time-link": `{"meta":{}}`,
		"/inquiries":             `{"data":{"id":"inq_123"}}`,
	})
	if _, err := p.Start(context.Background(), "usr_1", CheckPhone); err == nil {
		t.Fatal("esperaba un error sin enlace al que mandar a la persona")
	}
}

// --- Traducción de estados ---

func TestTraduccionDeEstados(t *testing.T) {
	p, _ := servidorPersona(t, nil)
	casos := map[string]CheckStatus{
		"approved":     StatusVerified,
		"declined":     StatusRejected,
		"failed":       StatusRejected,
		"expired":      StatusExpired,
		"created":      StatusPending,
		"pending":      StatusPending,
		"needs_review": StatusPending,
		// Un estado que hoy no existe: seguir esperando antes que acreditar.
		"algo_nuevo": StatusPending,
	}
	for estado, quiero := range casos {
		if got := p.traducirEstado(estado).Status; got != quiero {
			t.Errorf("%q → %q, esperaba %q", estado, got, quiero)
		}
	}
}

func TestUnaVerificacionSinDecisionNoAcreditaPorDefecto(t *testing.T) {
	// Una plantilla mal configurada termina todo en "completed" sin haber
	// comprobado nada. Darlo por bueno acreditaría a cualquiera.
	p, _ := servidorPersona(t, nil)
	if got := p.traducirEstado("completed").Status; got != StatusPending {
		t.Fatalf("completed → %q, esperaba que no acreditara sin más", got)
	}

	p.cfg.AceptarCompletado = true
	if got := p.traducirEstado("completed").Status; got != StatusVerified {
		t.Fatalf("con AceptarCompletado, completed → %q", got)
	}
}

func TestResultLeeLaCaducidadDelDocumento(t *testing.T) {
	// Sin esto, un carné vencido seguiría contando como identidad verificada.
	p, _ := servidorPersona(t, map[string]string{
		"/inquiries/": `{
			"data":{"attributes":{"status":"approved"}},
			"included":[{"type":"verification/government-id",
				"attributes":{"status":"passed","expiration-date":"2031-04-18"}}]}`,
	})

	out, err := p.Result(context.Background(), "inq_1")
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if out.Status != StatusVerified {
		t.Fatalf("estado = %q", out.Status)
	}
	if out.DocumentExpiresAt.Format("2006-01-02") != "2031-04-18" {
		t.Fatalf("caducidad = %v", out.DocumentExpiresAt)
	}
}

// --- Una plantilla puede acreditar varias comprobaciones ---

func TestUnMismoTramiteAcreditaDocumentoYCara(t *testing.T) {
	p, _ := servidorPersona(t, nil)
	cubre := p.Cubre("itmpl_documento_y_cara")

	if len(cubre) != 2 {
		t.Fatalf("cubre = %v, esperaba documento y selfie", cubre)
	}
	quiero := map[CheckKind]bool{CheckGovernmentID: true, CheckSelfie: true}
	for _, k := range cubre {
		if !quiero[k] {
			t.Errorf("no esperaba %s", k)
		}
	}
	if len(p.Cubre("itmpl_telefono")) != 1 {
		t.Error("la plantilla de teléfono solo debería acreditar el teléfono")
	}
}

func TestAcreditaSinDetalleValeParaLaQueSePidio(t *testing.T) {
	o := Outcome{Status: StatusVerified}
	if !o.Acredita(CheckPhone) {
		t.Fatal("sin detalle, el veredicto vale para la comprobación pedida")
	}
	o.Cubre = []CheckKind{CheckGovernmentID}
	if o.Acredita(CheckPhone) {
		t.Fatal("no puede acreditar algo que no cubre")
	}
}

// --- Firma de los avisos ---

func firmar(t *testing.T, cuerpo []byte, cuando time.Time, secreto string) string {
	t.Helper()
	marca := fmt.Sprintf("%d", cuando.Unix())
	mac := hmac.New(sha256.New, []byte(secreto))
	mac.Write([]byte(marca + "."))
	mac.Write(cuerpo)
	return fmt.Sprintf("t=%s,v1=%s", marca, hex.EncodeToString(mac.Sum(nil)))
}

func TestVerificarFirmaAceptaUnAvisoLegitimo(t *testing.T) {
	p, _ := servidorPersona(t, nil)
	cuerpo := []byte(`{"data":{"id":"evt_1"}}`)
	ahora := time.Now()

	if err := p.VerificarFirma(firmar(t, cuerpo, ahora, secretoDePrueba), cuerpo, ahora); err != nil {
		t.Fatalf("un aviso legítimo debería pasar: %v", err)
	}
}

func TestVerificarFirmaRechazaLoQueNoVieneDePersona(t *testing.T) {
	p, _ := servidorPersona(t, nil)
	cuerpo := []byte(`{"data":{"id":"evt_1"}}`)
	ahora := time.Now()

	casos := map[string]string{
		"sin cabecera":                "",
		"sin firma":                   "t=" + fmt.Sprint(ahora.Unix()),
		"sin marca de tiempo":         "v1=abcdef",
		"basura":                      "no-es-una-firma",
		"firmado con otro secreto":    firmar(t, cuerpo, ahora, "otro-secreto"),
		"firma que no es hexadecimal": fmt.Sprintf("t=%d,v1=zzzz", ahora.Unix()),
	}
	for nombre, cabecera := range casos {
		if err := p.VerificarFirma(cabecera, cuerpo, ahora); !errors.Is(err, ErrFirmaInvalida) {
			t.Errorf("%s: error = %v, esperaba ErrFirmaInvalida", nombre, err)
		}
	}
}

func TestVerificarFirmaRechazaUnCuerpoManipulado(t *testing.T) {
	// El ataque que importa: una firma legítima reutilizada sobre otro
	// contenido, para colar un "aprobado" que Persona nunca envió.
	p, _ := servidorPersona(t, nil)
	original := []byte(`{"status":"declined"}`)
	ahora := time.Now()
	cabecera := firmar(t, original, ahora, secretoDePrueba)

	manipulado := []byte(`{"status":"approved"}`)
	if err := p.VerificarFirma(cabecera, manipulado, ahora); !errors.Is(err, ErrFirmaInvalida) {
		t.Fatalf("error = %v: un cuerpo cambiado no puede pasar", err)
	}
}

func TestVerificarFirmaRechazaAvisosViejos(t *testing.T) {
	// Sin límite temporal, quien capturase un aviso legítimo podría reenviarlo
	// para siempre.
	p, _ := servidorPersona(t, nil)
	cuerpo := []byte(`{"data":{"id":"evt_1"}}`)
	ahora := time.Now()
	viejo := ahora.Add(-30 * time.Minute)

	if err := p.VerificarFirma(firmar(t, cuerpo, viejo, secretoDePrueba), cuerpo, ahora); !errors.Is(err, ErrFirmaInvalida) {
		t.Fatalf("error = %v, esperaba que rechazara un aviso caducado", err)
	}
}

func TestVerificarFirmaAceptaVariasFirmasDuranteLaRotacion(t *testing.T) {
	// Al rotar el secreto, Persona envía las dos firmas: basta con que encaje
	// una, o el cambio de secreto tumbaría las verificaciones.
	p, _ := servidorPersona(t, nil)
	cuerpo := []byte(`{"data":{"id":"evt_1"}}`)
	ahora := time.Now()

	buena := firmar(t, cuerpo, ahora, secretoDePrueba)
	_, firmaBuena, _ := strings.Cut(strings.Split(buena, ",")[1], "=")
	cabecera := fmt.Sprintf("t=%d,v1=%s,v1=%s", ahora.Unix(),
		hex.EncodeToString([]byte("firma-vieja-que-ya-no-vale")), firmaBuena)

	if err := p.VerificarFirma(cabecera, cuerpo, ahora); err != nil {
		t.Fatalf("con una firma válida entre varias debería pasar: %v", err)
	}
}

func TestSinSecretoNoSePuedeVerificarNada(t *testing.T) {
	p, err := NewPersona(PersonaConfig{
		APIKey:     "k",
		Plantillas: map[CheckKind]string{CheckPhone: "itmpl_1"},
	})
	if err != nil {
		t.Fatalf("NewPersona: %v", err)
	}
	if err := p.VerificarFirma("t=1,v1=aa", []byte("{}"), time.Now()); err == nil {
		t.Fatal("sin secreto configurado no se puede dar por bueno ningún aviso")
	}
}

// --- Lectura del aviso ---

func TestLeerAvisoDeAprobacion(t *testing.T) {
	p, _ := servidorPersona(t, nil)
	cuerpo := []byte(`{"data":{"attributes":{"name":"inquiry.approved","payload":{"data":{
		"id":"inq_77","attributes":{"status":"approved","reference-id":"usr_9"},
		"relationships":{"inquiry-template":{"data":{"id":"itmpl_documento_y_cara"}}}}}}}}`)

	aviso, err := p.LeerAviso(cuerpo)
	if err != nil {
		t.Fatalf("LeerAviso: %v", err)
	}
	if aviso.Ref != "inq_77" || aviso.UsuarioRef != "usr_9" {
		t.Fatalf("aviso = %+v", aviso)
	}
	if aviso.Outcome.Status != StatusVerified {
		t.Fatalf("estado = %q", aviso.Outcome.Status)
	}
	// La plantilla acredita documento y cara a la vez.
	if len(aviso.Outcome.Cubre) != 2 {
		t.Fatalf("cubre = %v, esperaba dos comprobaciones", aviso.Outcome.Cubre)
	}
}

func TestLeerAvisoRechazaLoQueNoIdentificaNada(t *testing.T) {
	p, _ := servidorPersona(t, nil)
	for nombre, cuerpo := range map[string]string{
		"no es json":        `no soy json`,
		"sin identificador": `{"data":{"attributes":{"name":"inquiry.approved"}}}`,
	} {
		if _, err := p.LeerAviso([]byte(cuerpo)); err == nil {
			t.Errorf("%s: esperaba un error", nombre)
		}
	}
}
