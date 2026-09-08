package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/notify"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/store"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// ErrSeguimientoInvalido se devuelve cuando el enlace no existe, caducó o se
// revocó. Los tres dan lo mismo a quien lo abre: ya no enseña nada.
var ErrSeguimientoInvalido = errors.New("este enlace ya no sigue ningún viaje")

// ErrNoVasEnEsteViaje se devuelve a quien intenta compartir o alertar sobre un
// viaje en el que no va.
var ErrNoVasEnEsteViaje = errors.New("no vas en este viaje")

// ErrAlertaResuelta se devuelve al tocar una alerta ya cerrada.
var ErrAlertaResuelta = errors.New("esa alerta ya está resuelta")

// --- Compartir el viaje ---

// EnlaceDeSeguimiento es lo que se le da a quien comparte: la dirección que
// tiene que mandar y hasta cuándo sirve.
type EnlaceDeSeguimiento struct {
	ID     string    `json:"id"`
	URL    string    `json:"url"`
	Expira time.Time `json:"expira"`
}

// CompartirViaje abre un enlace público que enseña este viaje.
//
// Es la versión digital de decirle a alguien "voy en este coche, con esta
// persona, y llego sobre esta hora". No evita nada por sí solo: convierte un
// viaje anónimo en uno del que hay testigo, que es de lo que carece un coche
// sin conductor.
func (s *Service) CompartirViaje(userID, tripID string) (*EnlaceDeSeguimiento, error) {
	t, err := s.viajeDondeVa(userID, tripID)
	if err != nil {
		return nil, err
	}

	testigo, hash, err := nuevoTestigo()
	if err != nil {
		return nil, err
	}
	now := s.cfg.Now()
	seg := &domain.Seguimiento{
		ID:        newID("seg"),
		TripID:    t.ID,
		UserID:    userID,
		TokenHash: hash,
		CreatedAt: now,
		ExpiraAt:  t.DepartureTime.Add(domain.GraciaSeguimiento),
	}
	// Un viaje que ya salió hace mucho todavía puede compartirse mientras dure
	// la gracia; uno que sale dentro de dos días, hasta doce horas después de
	// su salida. Nunca menos que ahora mismo, o el enlace nacería caducado.
	if !seg.ExpiraAt.After(now) {
		seg.ExpiraAt = now.Add(domain.GraciaSeguimiento)
	}
	if err := s.store.CrearSeguimiento(seg); err != nil {
		return nil, err
	}
	return &EnlaceDeSeguimiento{ID: seg.ID, URL: s.enlaceDeSeguimiento(testigo), Expira: seg.ExpiraAt}, nil
}

func (s *Service) enlaceDeSeguimiento(testigo string) string {
	return strings.TrimSuffix(s.cfg.PublicURL, "/") + "/seguir.html?t=" + testigo
}

// viajeDondeVa devuelve el trayecto si esa persona va dentro.
func (s *Service) viajeDondeVa(userID, tripID string) (*domain.Trip, error) {
	t, err := s.store.GetTrip(tripID)
	if err != nil {
		return nil, err
	}
	if t.HostID == userID {
		return t, nil
	}
	bookings, err := s.store.BookingsByTrip(tripID)
	if err != nil {
		return nil, err
	}
	for _, b := range bookings {
		if b.PassengerID == userID && b.Status == domain.BookingConfirmed {
			return t, nil
		}
	}
	return nil, ErrNoVasEnEsteViaje
}

// DejarDeCompartir cierra todos los enlaces de esa persona en ese viaje.
//
// Todos y no uno: pueden convivir varios —el del aviso al reservar, el del
// botón de emergencia— y "dejar de compartir" tiene que significar eso, no
// cerrar uno y dejar otro abierto sin que nadie se entere.
func (s *Service) DejarDeCompartir(userID, tripID string) (int, error) {
	if _, err := s.viajeDondeVa(userID, tripID); err != nil {
		return 0, err
	}
	return s.store.RevocarSeguimientos(tripID, userID, s.cfg.Now())
}

// ApuntarPosicion guarda dónde está quien comparte.
//
// La manda el navegador con la app abierta y con permiso, no un servicio de
// fondo: la app no sigue a nadie cuando no se está usando, y la política de
// privacidad dice exactamente eso.
func (s *Service) ApuntarPosicion(userID, tripID string, punto geo.Point) error {
	if _, err := s.viajeDondeVa(userID, tripID); err != nil {
		return err
	}
	vivos, err := s.store.SeguimientosVivos(tripID, userID)
	if err != nil {
		return err
	}
	if len(vivos) == 0 {
		// Sin enlace abierto no hay a quién enseñársela, así que no se guarda.
		// Guardar posiciones que nadie va a mirar es rastrear.
		return nil
	}
	return s.store.ApuntarPosicion(tripID, userID, punto, s.cfg.Now())
}

// OcupanteVisto es cada persona a bordo, tal y como la ve quien abre el enlace.
type OcupanteVisto struct {
	// Nombre es solo el primero: quien comparte comparte su viaje, no la
	// identidad completa de quien va a su lado.
	Nombre string      `json:"nombre"`
	Nivel  trust.Level `json:"nivel"`
	Papel  string      `json:"papel"`
}

// VistaSeguimiento es lo que enseña el enlace público.
type VistaSeguimiento struct {
	Comparte         string             `json:"comparte"`
	Origen           domain.Place       `json:"origen"`
	Destino          domain.Place       `json:"destino"`
	Ruta             geo.Route          `json:"ruta"`
	Salida           time.Time          `json:"salida"`
	DuracionMin      float64            `json:"duracion_min"`
	Vehiculo         domain.VehicleType `json:"vehiculo"`
	Estado           domain.TripStatus  `json:"estado"`
	Ocupantes        []OcupanteVisto    `json:"ocupantes"`
	UltimaPosicion   *geo.Point         `json:"ultima_posicion,omitempty"`
	UltimaPosicionAt *time.Time         `json:"ultima_posicion_at,omitempty"`
	// Alerta va aquí porque es lo primero que tiene que ver quien abra esto
	// después de recibir el aviso.
	Alerta   *domain.Alerta `json:"alerta,omitempty"`
	Expira   time.Time      `json:"expira"`
	Telefono string         `json:"telefono_emergencias"`
}

// VerSeguimiento devuelve lo que enseña un enlace público.
func (s *Service) VerSeguimiento(testigo string) (*VistaSeguimiento, error) {
	testigo = strings.TrimSpace(testigo)
	if testigo == "" {
		return nil, ErrSeguimientoInvalido
	}
	seg, err := s.store.SeguimientoPorHash(hashDeTestigo(testigo))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrSeguimientoInvalido
		}
		return nil, err
	}
	if !seg.Vigente(s.cfg.Now()) {
		return nil, ErrSeguimientoInvalido
	}

	t, err := s.store.GetTrip(seg.TripID)
	if err != nil {
		return nil, err
	}
	quien, err := s.store.GetUser(seg.UserID)
	if err != nil {
		return nil, err
	}

	vista := &VistaSeguimiento{
		Comparte:         domain.PrimerNombre(quien.Name),
		Origen:           t.Origin,
		Destino:          t.Destination,
		Ruta:             t.Route,
		Salida:           t.DepartureTime,
		DuracionMin:      t.DurationMin,
		Vehiculo:         t.Vehicle,
		Estado:           t.Status,
		UltimaPosicion:   seg.UltimaPosicion,
		UltimaPosicionAt: seg.UltimaPosicionAt,
		Expira:           seg.ExpiraAt,
		Telefono:         domain.TelefonoEmergencias,
	}
	if vista.Ocupantes, err = s.ocupantesDe(t); err != nil {
		return nil, err
	}
	// Solo la alerta de quien comparte: el enlace enseña su viaje, no el
	// historial de sustos de los demás.
	if a, err := s.store.AlertaVivaDe(t.ID, seg.UserID); err == nil {
		vista.Alerta = a
	}
	return vista, nil
}

// ocupantesDe enumera a quién va dentro, con lo justo para reconocerlos.
func (s *Service) ocupantesDe(t *domain.Trip) ([]OcupanteVisto, error) {
	out := []OcupanteVisto{}
	añadir := func(userID, papel string) {
		p, err := s.PerfilDe(userID)
		if err != nil {
			return
		}
		out = append(out, OcupanteVisto{
			Nombre: domain.PrimerNombre(p.Nombre), Nivel: p.Nivel, Papel: papel,
		})
	}
	añadir(t.HostID, "organiza")

	bookings, err := s.store.BookingsByTrip(t.ID)
	if err != nil {
		return nil, err
	}
	for _, b := range bookings {
		if b.Status == domain.BookingConfirmed {
			añadir(b.PassengerID, "pasajero")
		}
	}
	return out, nil
}

// --- Botón de emergencia ---

// AlertaInput son los datos con los que se dispara una alerta.
type AlertaInput struct {
	UserID string
	TripID string
	// Posicion puede faltar: sin permiso de ubicación el botón sigue
	// funcionando, porque lo que no puede pasar es que falle justo cuando hace
	// falta.
	Posicion *geo.Point
	Nota     string
}

// Emergencia dispara la alerta de un viaje.
//
// Lo que hace está acotado y la interfaz lo dice con todas las letras: avisa a
// los contactos de confianza con el enlace del viaje y la última posición, y
// pone la alerta la primera en la cola de operaciones. **No llama a los
// servicios de emergencia**: una app no puede hacer esa llamada, y dar a
// entender que sí es la clase de mentira por la que alguien se queda esperando
// una ayuda que no viene.
func (s *Service) Emergencia(in AlertaInput) (*domain.Alerta, error) {
	t, err := s.viajeDondeVa(in.UserID, in.TripID)
	if err != nil {
		return nil, err
	}

	now := s.cfg.Now()
	a := &domain.Alerta{
		ID:        newID("alr"),
		TripID:    t.ID,
		UserID:    in.UserID,
		Posicion:  in.Posicion,
		Nota:      domain.NormalizarComentario(in.Nota),
		Estado:    domain.AlertaAbierta,
		CreatedAt: now,
	}
	if err := a.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrValidation, err)
	}
	if err := s.store.CrearAlerta(a); err != nil {
		return nil, err
	}

	// El enlace se crea aquí si no lo había: quien pulsa el botón no está en
	// condiciones de acordarse de compartir el viaje antes.
	enlace := s.enlaceParaLaAlerta(in.UserID, t, in.Posicion, now)
	s.avisarContactos(in.UserID, notify.SucesoAlertaDisparada, t, enlace)

	// El registro es lo que hace que operaciones se entere sin esperar a que
	// alguien abra la cola.
	if s.cfg.Log != nil {
		s.cfg.Log.Error("ALERTA de emergencia",
			"alerta", a.ID, "viaje", t.ID, "usuario", in.UserID,
			"con_posicion", in.Posicion != nil, "ref_flota", t.FleetRideRef)
	}
	return a, nil
}

// enlaceParaLaAlerta abre un enlace nuevo para el aviso y le apunta la posición.
//
// Siempre uno nuevo, aunque ya hubiera otro: el testigo se guarda hasheado, así
// que de un enlace ya mandado no se puede recuperar la dirección para volver a
// escribirla en este correo. Los anteriores se quedan vivos, porque quien los
// recibió al reservar tiene que seguir pudiendo abrirlos justo ahora.
func (s *Service) enlaceParaLaAlerta(userID string, t *domain.Trip, p *geo.Point, now time.Time) string {
	nuevo, err := s.CompartirViaje(userID, t.ID)
	if err != nil {
		s.log("no se pudo abrir el enlace de seguimiento para la alerta", err)
		return s.cfg.PublicURL
	}
	if p != nil {
		if err := s.store.ApuntarPosicion(t.ID, userID, *p, now); err != nil {
			s.log("no se pudo apuntar la posición de la alerta", err)
		}
	}
	return nuevo.URL
}

// RetirarAlerta la marca como falsa alarma y manda la corrección.
//
// El aviso de corrección no es un detalle: los correos ya salieron y no se
// pueden recoger, así que lo único honesto es mandar otro diciendo que no pasa
// nada. Sin eso, quien lo recibió se queda con el susto puesto.
func (s *Service) RetirarAlerta(alertaID, userID string) (*domain.Alerta, error) {
	a, err := s.store.GetAlerta(alertaID)
	if err != nil {
		return nil, err
	}
	if a.UserID != userID {
		return nil, fmt.Errorf("%w: solo quien la disparó puede retirarla", ErrNoAutorizado)
	}
	if !a.Viva() {
		return nil, ErrAlertaResuelta
	}

	a.Estado = domain.AlertaRetirada
	a.ResueltaAt = s.cfg.Now()
	if err := s.store.UpdateAlerta(a); err != nil {
		return nil, err
	}
	if t, err := s.store.GetTrip(a.TripID); err == nil {
		s.avisarContactos(userID, notify.SucesoAlertaRetirada, t, s.cfg.PublicURL)
	}
	return a, nil
}

// AlertasPendientes es la cola de operaciones. Va por delante de las denuncias.
func (s *Service) AlertasPendientes() ([]*domain.Alerta, error) {
	return s.store.AlertasAbiertas()
}

// AtenderAlerta la cierra desde operaciones.
func (s *Service) AtenderAlerta(alertaID, resolucion string) (*domain.Alerta, error) {
	a, err := s.store.GetAlerta(alertaID)
	if err != nil {
		return nil, err
	}
	if !a.Viva() {
		return nil, ErrAlertaResuelta
	}
	a.Estado = domain.AlertaAtendida
	a.ResueltaAt = s.cfg.Now()
	a.Resolucion = domain.NormalizarComentario(resolucion)
	if err := s.store.UpdateAlerta(a); err != nil {
		return nil, err
	}
	return a, nil
}

// --- Avisos a los contactos ---

// avisarContactos escribe a los contactos de confianza de esa persona.
func (s *Service) avisarContactos(userID string, suceso notify.Suceso, t *domain.Trip, enlace string) {
	contactos, err := s.store.ContactosDe(userID)
	if err != nil {
		s.log("no se pudo leer la lista de contactos de confianza", err)
		return
	}
	if len(contactos) == 0 {
		return
	}
	u, err := s.store.GetUser(userID)
	if err != nil {
		s.log("no se pudo avisar a los contactos", err)
		return
	}

	datos := s.datosDelViaje(t)
	datos["enlace"] = enlace
	datos["quien"] = u.Name
	datos["telefono"] = domain.TelefonoEmergencias
	datos["vehiculo"] = string(t.Vehicle)
	if t.FleetRideRef != "" {
		datos["ref"] = t.FleetRideRef
	}

	for _, c := range contactos {
		// El contacto no es usuario de la app: no tiene idioma propio, así que
		// se le escribe en el de quien le puso en la lista.
		s.cfg.Avisos.Notificar(notify.Aviso{
			Suceso: suceso,
			Para:   c.Email,
			Nombre: c.Nombre,
			Idioma: u.Idioma,
			Datos:  datos,
		})
	}
}

// avisarSalida manda el enlace de seguimiento a quien lo pidió por adelantado.
//
// Se dispara al confirmarse una plaza, que es el momento en que dos
// desconocidos se comprometen a ir en el mismo coche: justo cuando alguien de
// fuera debería saberlo. Esperar a que la gente se acuerde de compartir el
// viaje es esperar a que no lo haga.
func (s *Service) avisarSalida(t *domain.Trip, userID string) {
	contactos, err := s.store.ContactosDe(userID)
	if err != nil || len(contactos) == 0 {
		return
	}
	var alguno bool
	for _, c := range contactos {
		if c.AvisarAlSalir {
			alguno = true
			break
		}
	}
	if !alguno {
		return
	}

	enlace, err := s.CompartirViaje(userID, t.ID)
	if err != nil {
		s.log("no se pudo compartir el viaje con los contactos", err)
		return
	}
	u, err := s.store.GetUser(userID)
	if err != nil {
		return
	}
	datos := s.datosDelViaje(t)
	datos["enlace"] = enlace.URL
	datos["quien"] = u.Name
	for _, c := range contactos {
		if !c.AvisarAlSalir {
			continue
		}
		s.cfg.Avisos.Notificar(notify.Aviso{
			Suceso: notify.SucesoViajeCompartido,
			Para:   c.Email,
			Nombre: c.Nombre,
			Idioma: u.Idioma,
			Datos:  datos,
		})
	}
}

// --- Viajes en curso ---

// ViajeActivo es un trayecto en el que esa persona va a subirse o ya va dentro.
//
// Es lo que sostiene toda la parte de seguridad de la interfaz: sin saber en
// qué viaje estás, no se puede enseñar el botón de emergencia ni ofrecerte
// compartirlo.
type ViajeActivo struct {
	TripID   string             `json:"trip_id"`
	Origen   string             `json:"origen"`
	Destino  string             `json:"destino"`
	Salida   time.Time          `json:"salida"`
	Vehiculo domain.VehicleType `json:"vehiculo"`
	Papel    string             `json:"papel"`
	// Compartido dice si hay al menos un enlace abierto.
	Compartido bool `json:"compartido"`
	// Alerta es la que esa persona tenga viva en este viaje.
	Alerta *domain.Alerta `json:"alerta,omitempty"`
}

// ViajesActivos son los trayectos que todavía no han terminado y en los que
// esa persona va dentro.
func (s *Service) ViajesActivos(userID string) ([]ViajeActivo, error) {
	now := s.cfg.Now()
	vivo := func(t *domain.Trip) bool {
		if t.Status == domain.TripCancelled || t.Status == domain.TripCompleted {
			return false
		}
		// La gracia da margen a un viaje que se retrasa: quien va dentro de un
		// coche que sale tarde es justo quien más necesita el botón.
		return now.Before(t.DepartureTime.Add(domain.GraciaSeguimiento))
	}

	out := []ViajeActivo{}
	añadir := func(t *domain.Trip, papel string) {
		v := ViajeActivo{
			TripID: t.ID, Origen: t.Origin.Name, Destino: t.Destination.Name,
			Salida: t.DepartureTime, Vehiculo: t.Vehicle, Papel: papel,
		}
		if vivos, err := s.store.SeguimientosVivos(t.ID, userID); err == nil {
			for _, seg := range vivos {
				if seg.Vigente(now) {
					v.Compartido = true
					break
				}
			}
		}
		if a, err := s.store.AlertaVivaDe(t.ID, userID); err == nil {
			v.Alerta = a
		}
		out = append(out, v)
	}

	mios, err := s.store.TripsByHost(userID)
	if err != nil {
		return nil, err
	}
	for _, t := range mios {
		if vivo(t) {
			añadir(t, "organiza")
		}
	}

	reservas, err := s.store.BookingsByPassenger(userID)
	if err != nil {
		return nil, err
	}
	for _, b := range reservas {
		if b.Status != domain.BookingConfirmed {
			continue
		}
		t, err := s.store.GetTrip(b.TripID)
		if err != nil || !vivo(t) {
			continue
		}
		añadir(t, "pasajero")
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Salida.Before(out[j].Salida) })
	return out, nil
}
