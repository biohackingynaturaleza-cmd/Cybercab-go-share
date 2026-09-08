package notify

import "strings"

// plantilla es un correo en un idioma.
type plantilla struct {
	Asunto string
	Cuerpo string
}

// Los correos son de texto plano y cortos a propósito: se leen de pie, en el
// móvil, y lo único que hay que sacar de ellos es qué ha pasado y qué hacer.
// Nada de imágenes ni maquetación que se rompa en la mitad de los clientes.
var plantillas = map[string]map[Suceso]plantilla{
	"en": {
		SucesoPlazaPedida: {
			"Someone wants to share your ride to {destino}",
			`Hi {nombre},

{pasajero} asked for a seat on your trip to {destino} on {salida}.
They would ride {km} km with you and put in {importe}.

Their trust level: {nivel}

Accepting makes you responsible for their conduct in the vehicle, so check
their profile first.

Answer here: {enlace}`,
		},
		SucesoPlazaAceptada: {
			"You have a seat to {destino}",
			`Hi {nombre},

{host} accepted you on the trip to {destino} on {salida}.
You are sharing {km} km and paying {importe} — instead of {solo} on your own.

See the details: {enlace}`,
		},
		SucesoPlazaRechazada: {
			"Your seat request was declined",
			`Hi {nombre},

Your request for the trip to {destino} on {salida} was not accepted.

There may be other rides going your way: {enlace}`,
		},
		SucesoReservaAnulada: {
			"A booking on your {destino} trip was cancelled",
			`Hi {nombre},

The booking for the trip to {destino} on {salida} has been cancelled.
The seat is free again.

{enlace}`,
		},
		SucesoTrayectoAnulado: {
			"The trip to {destino} was cancelled",
			`Hi {nombre},

{host} cancelled the trip to {destino} on {salida}, so your seat is gone.
You have not been charged anything.

Find another ride: {enlace}`,
		},
		SucesoIdentidadVerificada: {
			"Your identity is verified",
			`Hi {nombre},

Your identity is verified. You can now share any vehicle, including the
two-seat Cybercab.

{enlace}`,
		},
		SucesoIncidenciaDeclarada: {
			"A {importe} charge from your {destino} ride",
			`Hi {nombre},

After the trip to {destino} on {salida}, the fleet charged {importe} to the
person who booked the ride. They say it was down to you:

{motivo}

If that is right, accept it and it goes on your next settlement. If it is not,
say so — nothing is charged to you until you agree.

{enlace}`,
		},
		SucesoIncidenciaAceptada: {
			"Your {importe} claim was accepted",
			`Hi {nombre},

The {importe} charge you passed on has been accepted. It will reach you in the
next settlement.

{enlace}`,
		},
		SucesoIncidenciaDiscutida: {
			"Your {importe} claim is disputed",
			`Hi {nombre},

The {importe} charge you passed on has been disputed, so nothing has been
charged. We cannot decide who is right, so a person will look at it.

{enlace}`,
		},
		SucesoIdentidadRechazada: {
			"We could not verify your identity",
			`Hi {nombre},

The identity check did not go through. You can try again — usually it is a
blurry photo or a document that is hard to read.

Try again: {enlace}`,
		},
	},

	"es": {
		SucesoPlazaPedida: {
			"Alguien quiere compartir tu viaje a {destino}",
			`Hola {nombre}:

{pasajero} ha pedido plaza en tu trayecto a {destino} del {salida}.
Recorrería {km} km contigo y aportaría {importe}.

Su nivel de confianza: {nivel}

Aceptar te hace responsable de su conducta dentro del vehículo, así que mira
antes su perfil.

Responde aquí: {enlace}`,
		},
		SucesoPlazaAceptada: {
			"Tienes plaza para ir a {destino}",
			`Hola {nombre}:

{host} te ha aceptado en el trayecto a {destino} del {salida}.
Compartís {km} km y pagas {importe}, en vez de {solo} yendo por tu cuenta.

Mira los detalles: {enlace}`,
		},
		SucesoPlazaRechazada: {
			"No has conseguido la plaza",
			`Hola {nombre}:

Tu petición para el trayecto a {destino} del {salida} no ha sido aceptada.

Puede que haya otros viajes por tu camino: {enlace}`,
		},
		SucesoReservaAnulada: {
			"Se ha anulado una reserva de tu viaje a {destino}",
			`Hola {nombre}:

La reserva del trayecto a {destino} del {salida} se ha anulado.
La plaza vuelve a estar libre.

{enlace}`,
		},
		SucesoTrayectoAnulado: {
			"Se ha anulado el viaje a {destino}",
			`Hola {nombre}:

{host} ha anulado el trayecto a {destino} del {salida}, así que te quedas sin
plaza. No se te ha cobrado nada.

Busca otro viaje: {enlace}`,
		},
		SucesoIdentidadVerificada: {
			"Tu identidad está verificada",
			`Hola {nombre}:

Ya tienes la identidad acreditada. Puedes compartir cualquier vehículo,
incluido el Cybercab biplaza.

{enlace}`,
		},
		SucesoIncidenciaDeclarada: {
			"Un cargo de {importe} de tu viaje a {destino}",
			`Hola {nombre}:

Después del viaje a {destino} del {salida}, la flota cobró {importe} a quien
pidió el coche. Dice que fue cosa tuya:

{motivo}

Si es así, acéptalo y entrará en tu próxima liquidación. Si no lo es, dilo: no
se te cobra nada mientras no estés de acuerdo.

{enlace}`,
		},
		SucesoIncidenciaAceptada: {
			"Han aceptado tu cargo de {importe}",
			`Hola {nombre}:

El cargo de {importe} que repercutiste ha sido aceptado. Te llegará en la
próxima liquidación.

{enlace}`,
		},
		SucesoIncidenciaDiscutida: {
			"Han discutido tu cargo de {importe}",
			`Hola {nombre}:

El cargo de {importe} que repercutiste ha sido discutido, así que no se ha
cobrado nada. No podemos decidir quién tiene razón, así que lo revisará una
persona.

{enlace}`,
		},
		SucesoIdentidadRechazada: {
			"No hemos podido verificar tu identidad",
			`Hola {nombre}:

La comprobación de identidad no ha salido adelante. Puedes volver a
intentarlo: casi siempre es una foto movida o un documento poco legible.

Inténtalo otra vez: {enlace}`,
		},
	},
}

// firma cierra todos los correos.
var firma = map[string]string{
	"en": "\n\n—\nCybercab Go Share · Austin, TX",
	"es": "\n\n—\nCybercab Go Share · Austin, TX",
}

// Componer produce el asunto y el cuerpo de un aviso.
//
// Un idioma desconocido cae en inglés, y un suceso sin plantilla devuelve un
// texto vacío que la cola descarta: mejor no mandar nada que mandar un correo
// con huecos sin rellenar.
func Componer(a Aviso) (asunto, cuerpo string) {
	idioma := a.Idioma
	if _, ok := plantillas[idioma]; !ok {
		idioma = "en"
	}
	p, ok := plantillas[idioma][a.Suceso]
	if !ok {
		return "", ""
	}

	datos := map[string]string{"nombre": a.Nombre}
	for k, v := range a.Datos {
		datos[k] = v
	}
	return rellenar(p.Asunto, datos), rellenar(p.Cuerpo, datos) + firma[idioma]
}

// rellenar sustituye los huecos {así}. Los que no tengan valor se quedan
// vacíos en vez de aparecer como llaves sueltas en el correo.
func rellenar(texto string, datos map[string]string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(texto, '{')
		if i < 0 {
			b.WriteString(texto)
			return b.String()
		}
		j := strings.IndexByte(texto[i:], '}')
		if j < 0 {
			b.WriteString(texto)
			return b.String()
		}
		b.WriteString(texto[:i])
		b.WriteString(datos[texto[i+1:i+j]])
		texto = texto[i+j+1:]
	}
}
