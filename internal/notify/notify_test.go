package notify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// enviadorDePrueba guarda lo enviado y puede fallar a voluntad.
//
// El canal se crea siempre y se cierra una sola vez: dejarlo a nil dentro del
// cerrojo y leerlo fuera era una carrera de la propia prueba.
type enviadorDePrueba struct {
	mu       sync.Mutex
	enviados []string
	fallo    error
	unaVez   sync.Once
	listo    chan struct{}
}

func nuevoEnviador(fallo error) *enviadorDePrueba {
	return &enviadorDePrueba{fallo: fallo, listo: make(chan struct{})}
}

func (e *enviadorDePrueba) Enviar(_ context.Context, para, asunto, cuerpo string) error {
	defer e.unaVez.Do(func() { close(e.listo) })

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fallo != nil {
		return e.fallo
	}
	e.enviados = append(e.enviados, para+" | "+asunto+" | "+cuerpo)
	return nil
}

func (e *enviadorDePrueba) todo() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.enviados...)
}

func silencioso() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// --- Plantillas ---

func TestSeEscribeEnElIdiomaDeCadaCual(t *testing.T) {
	datos := map[string]string{"destino": "AUS", "salida": "8/9 15:00", "km": "10.8",
		"importe": "$5.41", "pasajero": "Bruno", "nivel": "verificado", "enlace": "https://x"}

	en, cuerpoEN := Componer(Aviso{Suceso: SucesoPlazaPedida, Nombre: "Ana", Idioma: "en", Datos: datos})
	es, cuerpoES := Componer(Aviso{Suceso: SucesoPlazaPedida, Nombre: "Ana", Idioma: "es", Datos: datos})

	if !strings.Contains(en, "share your ride") {
		t.Errorf("asunto en inglés = %q", en)
	}
	if !strings.Contains(es, "compartir tu viaje") {
		t.Errorf("asunto en español = %q", es)
	}
	for _, c := range []string{cuerpoEN, cuerpoES} {
		if !strings.Contains(c, "Ana") || !strings.Contains(c, "Bruno") || !strings.Contains(c, "AUS") {
			t.Errorf("el cuerpo no lleva los datos: %q", c)
		}
	}
}

func TestUnIdiomaDesconocidoCaeEnIngles(t *testing.T) {
	asunto, _ := Componer(Aviso{Suceso: SucesoPlazaAceptada, Idioma: "fr",
		Datos: map[string]string{"destino": "AUS"}})
	if !strings.Contains(asunto, "You have a seat") {
		t.Fatalf("asunto = %q, esperaba el inglés como respaldo", asunto)
	}
}

func TestNoQuedanHuecosSinRellenar(t *testing.T) {
	// Un correo con "{importe}" a la vista es peor que no mandarlo.
	for _, idioma := range []string{"en", "es"} {
		for suceso := range plantillas[idioma] {
			asunto, cuerpo := Componer(Aviso{Suceso: suceso, Nombre: "Ana", Idioma: idioma})
			for _, texto := range []string{asunto, cuerpo} {
				if strings.ContainsAny(texto, "{}") {
					t.Errorf("%s/%s deja huecos: %q", idioma, suceso, texto)
				}
			}
		}
	}
}

func TestTodosLosSucesosTienenPlantillaEnAmbosIdiomas(t *testing.T) {
	sucesos := []Suceso{
		SucesoPlazaPedida, SucesoPlazaAceptada, SucesoPlazaRechazada,
		SucesoReservaAnulada, SucesoTrayectoAnulado,
		SucesoIdentidadVerificada, SucesoIdentidadRechazada,
	}
	for _, idioma := range []string{"en", "es"} {
		for _, s := range sucesos {
			if _, ok := plantillas[idioma][s]; !ok {
				t.Errorf("falta la plantilla %s en %s", s, idioma)
			}
		}
	}
}

func TestUnSucesoSinPlantillaNoProduceCorreo(t *testing.T) {
	asunto, cuerpo := Componer(Aviso{Suceso: Suceso("inventado"), Idioma: "es"})
	if asunto != "" || cuerpo != "" {
		t.Fatalf("esperaba nada, salió %q / %q", asunto, cuerpo)
	}
}

// --- Cola ---

func TestLaColaEnviaLoQueSeLeEncola(t *testing.T) {
	e := nuevoEnviador(nil)
	c := NuevaCola(e, silencioso(), 8)

	c.Notificar(Aviso{Suceso: SucesoIdentidadVerificada, Para: "ana@x.com",
		Nombre: "Ana", Idioma: "es"})

	select {
	case <-e.listo:
	case <-time.After(2 * time.Second):
		t.Fatal("el aviso no llegó a enviarse")
	}
	c.Cerrar()

	enviados := e.todo()
	if len(enviados) != 1 || !strings.Contains(enviados[0], "ana@x.com") {
		t.Fatalf("enviados = %v", enviados)
	}
}

func TestNotificarNoBloqueaNiFalla(t *testing.T) {
	// Es la garantía que sostiene todo esto: quien acaba de reservar no puede
	// quedarse esperando a un servidor de correo, ni ver fallar su reserva
	// porque el correo no salga.
	e := nuevoEnviador(errors.New("servidor de correo caído"))
	c := NuevaCola(e, silencioso(), 4)
	defer c.Cerrar()

	hecho := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			c.Notificar(Aviso{Suceso: SucesoPlazaPedida, Para: "ana@x.com", Idioma: "es"})
		}
		close(hecho)
	}()

	select {
	case <-hecho:
	case <-time.After(3 * time.Second):
		t.Fatal("Notificar se ha quedado bloqueado")
	}
}

func TestUnAvisoSinDestinatarioSeDescarta(t *testing.T) {
	e := nuevoEnviador(nil)
	c := NuevaCola(e, silencioso(), 4)
	c.Notificar(Aviso{Suceso: SucesoPlazaPedida, Idioma: "es"})
	c.Cerrar()

	if len(e.todo()) != 0 {
		t.Fatalf("se intentó enviar sin destinatario: %v", e.todo())
	}
}

func TestCerrarEsperaAQueSalgaLoPendiente(t *testing.T) {
	e := nuevoEnviador(nil)
	c := NuevaCola(e, silencioso(), 64)

	for i := 0; i < 20; i++ {
		c.Notificar(Aviso{Suceso: SucesoIdentidadVerificada, Para: "ana@x.com", Idioma: "en"})
	}
	c.Cerrar()

	if n := len(e.todo()); n != 20 {
		t.Fatalf("enviados = %d, esperaba los 20 encolados", n)
	}
}

func TestCerrarDosVecesNoRompe(t *testing.T) {
	c := NuevaCola(nuevoEnviador(nil), silencioso(), 2)
	c.Cerrar()
	c.Cerrar()
}

func TestSilencioNoHaceNada(t *testing.T) {
	Silencio{}.Notificar(Aviso{Suceso: SucesoPlazaPedida, Para: "ana@x.com"})
}

// --- Composición del mensaje SMTP ---

func TestElAsuntoConTildesVaCodificado(t *testing.T) {
	// Sin codificar, las tildes y las eñes llegan rotas.
	s := &SMTP{De: "Cybercab <no-reply@x.com>"}
	msg := string(s.componer("ana@x.com", "Tienes plaza para ir al aeropuerto Ñ", "hola"))

	if strings.Contains(msg, "Subject: Tienes plaza para ir al aeropuerto Ñ") {
		t.Error("el asunto va sin codificar")
	}
	if !strings.Contains(msg, "Subject: =?UTF-8?") {
		t.Errorf("falta la codificación del asunto:\n%s", msg)
	}
	for _, cabecera := range []string{"To: ana@x.com", "charset=UTF-8", "Auto-Submitted: auto-generated"} {
		if !strings.Contains(msg, cabecera) {
			t.Errorf("falta la cabecera %q", cabecera)
		}
	}
}

func TestElRemitenteSeExtraeDelNombreCompleto(t *testing.T) {
	casos := map[string]string{
		"Cybercab Go Share <hola@x.com>": "hola@x.com",
		"hola@x.com":                     "hola@x.com",
	}
	for de, quiero := range casos {
		if got := (&SMTP{De: de}).remitente(); got != quiero {
			t.Errorf("%q → %q, esperaba %q", de, got, quiero)
		}
	}
}

func TestNuevoSMTPExigeServidorYRemitente(t *testing.T) {
	if _, err := NuevoSMTP("", "587", "u", "c", "a@x.com"); err == nil {
		t.Error("esperaba error sin servidor")
	}
	if _, err := NuevoSMTP("smtp.x.com", "587", "u", "c", ""); err == nil {
		t.Error("esperaba error sin remitente")
	}
	s, err := NuevoSMTP("smtp.x.com", "", "u", "c", "a@x.com")
	if err != nil {
		t.Fatalf("NuevoSMTP: %v", err)
	}
	if s.Port != "587" {
		t.Errorf("puerto = %q, esperaba el 587 por defecto", s.Port)
	}
}

func TestEnviarRespetaLaCancelacion(t *testing.T) {
	// Un servidor que no responde no puede dejar la gorrutina colgada.
	s := &SMTP{Host: "192.0.2.1", Port: "587", De: "a@x.com"} // dirección que no responde
	ctx, cancelar := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancelar()

	inicio := time.Now()
	err := s.Enviar(ctx, "b@x.com", "hola", "cuerpo")
	if err == nil {
		t.Fatal("esperaba un error")
	}
	if time.Since(inicio) > 3*time.Second {
		t.Fatalf("tardó %s: no está respetando el contexto", time.Since(inicio))
	}
}
