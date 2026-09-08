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
		SucesoRecuperacionPedida: {
			"Reset your password",
			`Hi {nombre},

Someone asked to reset the password of this account. Open this link to choose
a new one — it works once and expires in {minutos} minutes:

{enlace}

If it was not you, ignore this email. Nothing changes until that link is used,
and your current password still works.`,
		},
		SucesoContrasenaCambiada: {
			"Your password has changed",
			`Hi {nombre},

The password of your account has just been changed, and any other reset links
have stopped working.

If this was not you, reset your password now to lock the account again:
{enlace}`,
		},
		SucesoPideValoracion: {
			"How did your ride to {destino} go?",
			`Hi {nombre},

You shared the trip to {destino} with {quien}. Rating each other is what makes
this work: without it, the next person has nothing to go on.

Neither of you sees the other's rating until you have both rated, so say what
you really thought.

Rate the ride: {enlace}`,
		},
		SucesoValoracionRecibida: {
			"{quien} rated your ride: {estrellas}/5",
			`Hi {nombre},

You have both rated the trip, so the ratings are now visible. {quien} gave you
{estrellas} out of 5.

See it: {enlace}`,
		},
		SucesoDenunciaRecibida: {
			"We have your report",
			`Hi {nombre},

Your report has reached us and a person will look at it. We will not tell the
other party that it came from you.

You will not see them again on the app: reporting somebody blocks them both
ways.

If you are in danger right now, call 911 first — we cannot help with that.`,
		},
		SucesoDenunciaResuelta: {
			"Your report has been reviewed",
			`Hi {nombre},

We have finished reviewing the report you filed. Outcome: {resultado}

{resolucion}

Thank you for telling us. It is what keeps this usable for everyone.`,
		},
		SucesoCuentaSuspendida: {
			"Your account cannot share rides for now",
			`Hi {nombre},

After reviewing a report, your account has been suspended from publishing or
booking trips until {hasta}.

{resolucion}

You can still sign in, see your history and settle what you owe. If you believe
this is a mistake, reply to this email.`,
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
		SucesoRecuperacionPedida: {
			"Cambia tu contraseña",
			`Hola {nombre}:

Alguien ha pedido cambiar la contraseña de esta cuenta. Abre este enlace para
elegir una nueva; sirve una sola vez y caduca en {minutos} minutos:

{enlace}

Si no has sido tú, ignora este correo. No cambia nada hasta que se use ese
enlace, y tu contraseña de siempre sigue valiendo.`,
		},
		SucesoContrasenaCambiada: {
			"Tu contraseña ha cambiado",
			`Hola {nombre}:

La contraseña de tu cuenta acaba de cambiar, y cualquier otro enlace de
recuperación ha dejado de servir.

Si no has sido tú, cambia la contraseña ahora para recuperar el control de la
cuenta: {enlace}`,
		},
		SucesoPideValoracion: {
			"¿Qué tal fue tu viaje a {destino}?",
			`Hola {nombre}:

Compartiste el trayecto a {destino} con {quien}. Valoraros es lo que hace que
esto funcione: sin eso, quien venga detrás no tiene nada en lo que apoyarse.

Ninguno de los dos ve la valoración del otro hasta que habéis valorado los dos,
así que di lo que de verdad piensas.

Valora el viaje: {enlace}`,
		},
		SucesoValoracionRecibida: {
			"{quien} ha valorado tu viaje: {estrellas}/5",
			`Hola {nombre}:

Ya habéis valorado los dos, así que las valoraciones son visibles. {quien} te
ha puesto {estrellas} sobre 5.

Míralo: {enlace}`,
		},
		SucesoDenunciaRecibida: {
			"Hemos recibido tu denuncia",
			`Hola {nombre}:

Tu denuncia nos ha llegado y la va a revisar una persona. No le diremos a la
otra parte que viene de ti.

No volverás a verla en la app: denunciar a alguien le bloquea en las dos
direcciones.

Si ahora mismo estás en peligro, llama antes al 911: con eso no podemos
ayudarte nosotros.`,
		},
		SucesoDenunciaResuelta: {
			"Hemos revisado tu denuncia",
			`Hola {nombre}:

Hemos terminado de revisar la denuncia que pusiste. Resultado: {resultado}

{resolucion}

Gracias por contárnoslo. Es lo que mantiene esto usable para todos.`,
		},
		SucesoCuentaSuspendida: {
			"Tu cuenta no puede compartir viajes por ahora",
			`Hola {nombre}:

Tras revisar una denuncia, tu cuenta queda suspendida para publicar y reservar
trayectos hasta el {hasta}.

{resolucion}

Puedes seguir entrando, consultar tu historial y saldar lo que debas. Si crees
que es un error, responde a este correo.`,
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
