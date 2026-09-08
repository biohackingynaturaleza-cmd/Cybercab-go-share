// Package notify avisa a la gente de lo que pasa con sus viajes.
//
// Dos reglas gobiernan el diseño:
//
// Un fallo de correo no puede deshacer nada. Si alguien ya ha pedido plaza, la
// plaza está pedida aunque el servidor de correo esté caído; por eso los avisos
// se encolan y se envían aparte, y Notificar nunca devuelve error ni bloquea a
// quien lo llama.
//
// Y se escribe en el idioma de quien lee. Mandar un correo en español a alguien
// que usa la app en inglés es tratarle como a un usuario de segunda.
package notify

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Suceso es cada cosa de la que merece la pena avisar.
type Suceso string

const (
	// SucesoPlazaPedida avisa a quien organiza de que alguien quiere subirse.
	SucesoPlazaPedida Suceso = "plaza_pedida"
	// SucesoPlazaAceptada avisa al pasajero de que tiene sitio.
	SucesoPlazaAceptada Suceso = "plaza_aceptada"
	// SucesoPlazaRechazada avisa al pasajero de que no.
	SucesoPlazaRechazada Suceso = "plaza_rechazada"
	// SucesoReservaAnulada avisa a la otra parte de una anulación.
	SucesoReservaAnulada Suceso = "reserva_anulada"
	// SucesoTrayectoAnulado avisa a los pasajeros de que el viaje se cae.
	SucesoTrayectoAnulado Suceso = "trayecto_anulado"
	// SucesoIdentidadVerificada avisa de que ya se puede compartir coche.
	SucesoIdentidadVerificada Suceso = "identidad_verificada"
	// SucesoIdentidadRechazada avisa de que la verificación no pasó.
	SucesoIdentidadRechazada Suceso = "identidad_rechazada"
	// SucesoIncidenciaDeclarada avisa de que la flota cobró algo después del
	// viaje y se le atribuye.
	SucesoIncidenciaDeclarada Suceso = "incidencia_declarada"
	// SucesoIncidenciaAceptada avisa a quien la declaró de que la reconocen.
	SucesoIncidenciaAceptada Suceso = "incidencia_aceptada"
	// SucesoIncidenciaDiscutida avisa de que la niegan.
	SucesoIncidenciaDiscutida Suceso = "incidencia_discutida"
	// SucesoRecuperacionPedida lleva el enlace para poner una contraseña nueva.
	SucesoRecuperacionPedida Suceso = "recuperacion_pedida"
	// SucesoContrasenaCambiada avisa de que la contraseña acaba de cambiar. Es
	// el aviso que descubre un robo de cuenta.
	SucesoContrasenaCambiada Suceso = "contrasena_cambiada"
	// SucesoCodigoCorreo lleva el código que acredita el buzón.
	SucesoCodigoCorreo Suceso = "codigo_correo"
	// SucesoPideValoracion pide opinión sobre el viaje recién terminado.
	SucesoPideValoracion Suceso = "pide_valoracion"
	// SucesoValoracionRecibida avisa de que ya se puede ver lo que le pusieron,
	// porque han valorado los dos.
	SucesoValoracionRecibida Suceso = "valoracion_recibida"
	// SucesoDenunciaRecibida confirma a quien denuncia que su denuncia existe.
	SucesoDenunciaRecibida Suceso = "denuncia_recibida"
	// SucesoDenunciaResuelta le cuenta en qué quedó.
	SucesoDenunciaResuelta Suceso = "denuncia_resuelta"
	// SucesoCuentaSuspendida avisa a quien queda apartado de compartir viajes.
	SucesoCuentaSuspendida Suceso = "cuenta_suspendida"
)

// Aviso es un mensaje concreto para una persona concreta.
type Aviso struct {
	Suceso Suceso
	// Para es la dirección de correo del destinatario.
	Para string
	// Nombre es cómo dirigirse a esa persona.
	Nombre string
	// Idioma es "es" o "en". Cualquier otra cosa cae en inglés.
	Idioma string
	// Datos rellena los huecos de la plantilla.
	Datos map[string]string
}

// Enviador entrega un correo ya compuesto. Es la frontera con el proveedor:
// SMTP, Postmark, SendGrid o lo que sea.
type Enviador interface {
	Enviar(ctx context.Context, para, asunto, cuerpo string) error
}

// Notificador encola avisos y los va enviando.
type Notificador interface {
	// Notificar encola un aviso. No bloquea y no falla: si algo va mal, se
	// registra, pero nunca se le devuelve el problema a quien estaba haciendo
	// otra cosa.
	Notificar(a Aviso)
}

// Cola es el notificador de verdad: guarda los avisos en un canal y los envía
// desde una gorrutina aparte.
type Cola struct {
	enviador Enviador
	log      *slog.Logger
	avisos   chan Aviso
	cerrar   sync.Once
	fin      chan struct{}
	// Espera es cuánto se le da a cada envío antes de rendirse.
	espera time.Duration
}

var _ Notificador = (*Cola)(nil)

// NuevaCola arranca el notificador. Hay que llamar a Cerrar al terminar.
func NuevaCola(e Enviador, log *slog.Logger, capacidad int) *Cola {
	if capacidad <= 0 {
		capacidad = 256
	}
	c := &Cola{
		enviador: e,
		log:      log,
		avisos:   make(chan Aviso, capacidad),
		fin:      make(chan struct{}),
		espera:   20 * time.Second,
	}
	go c.trabajar()
	return c
}

// Notificar encola el aviso. Si la cola está llena se descarta con un registro:
// perder un aviso es malo, pero bloquear a quien acaba de reservar es peor.
func (c *Cola) Notificar(a Aviso) {
	if a.Para == "" {
		return
	}
	select {
	case c.avisos <- a:
	default:
		c.log.Warn("cola de avisos llena, aviso descartado", "suceso", string(a.Suceso))
	}
}

func (c *Cola) trabajar() {
	defer close(c.fin)
	for a := range c.avisos {
		asunto, cuerpo := Componer(a)
		ctx, cancelar := context.WithTimeout(context.Background(), c.espera)
		err := c.enviador.Enviar(ctx, a.Para, asunto, cuerpo)
		cancelar()
		if err != nil {
			// No se reintenta aquí: un servidor de correo caído haría girar
			// esto sin parar. El proveedor de correo ya reintenta por su
			// cuenta, y lo que queda se ve en el registro.
			c.log.Error("no se pudo enviar un aviso",
				"suceso", string(a.Suceso), "err", err)
			continue
		}
		c.log.Info("aviso enviado", "suceso", string(a.Suceso))
	}
}

// Cerrar deja de aceptar avisos y espera a que se envíen los que quedan.
func (c *Cola) Cerrar() {
	c.cerrar.Do(func() { close(c.avisos) })
	<-c.fin
}

// Silencio es un notificador que no hace nada. Sirve para pruebas y para
// arrancar sin correo configurado.
type Silencio struct{}

func (Silencio) Notificar(Aviso) {}

var _ Notificador = Silencio{}
