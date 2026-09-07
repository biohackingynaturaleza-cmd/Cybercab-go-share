package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTP entrega correo por un servidor SMTP con autenticación y STARTTLS.
type SMTP struct {
	Host string
	Port string
	// Usuario y Clave son las credenciales. Vacías, se conecta sin autenticar
	// (solo tiene sentido con un relé local).
	Usuario string
	Clave   string
	// De es el remitente: "Cybercab Go Share <hola@ejemplo.com>".
	De string
}

var _ Enviador = (*SMTP)(nil)

// NuevoSMTP comprueba la configuración antes de dar por bueno el envío.
func NuevoSMTP(host, port, usuario, clave, de string) (*SMTP, error) {
	if host == "" || de == "" {
		return nil, errors.New("el correo necesita al menos servidor y remitente")
	}
	if port == "" {
		port = "587"
	}
	return &SMTP{Host: host, Port: port, Usuario: usuario, Clave: clave, De: de}, nil
}

// Enviar entrega el mensaje. Respeta la cancelación del contexto: sin eso, un
// servidor que no responde dejaría la gorrutina colgada para siempre.
func (s *SMTP) Enviar(ctx context.Context, para, asunto, cuerpo string) error {
	if para == "" || asunto == "" {
		return errors.New("faltan destinatario o asunto")
	}

	var auth smtp.Auth
	if s.Usuario != "" {
		auth = smtp.PlainAuth("", s.Usuario, s.Clave, s.Host)
	}
	mensaje := s.componer(para, asunto, cuerpo)
	direccion := net.JoinHostPort(s.Host, s.Port)

	hecho := make(chan error, 1)
	go func() {
		hecho <- smtp.SendMail(direccion, auth, s.remitente(), []string{para}, mensaje)
	}()

	select {
	case err := <-hecho:
		if err != nil {
			return fmt.Errorf("enviando a través de %s: %w", direccion, err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// componer arma el mensaje completo con sus cabeceras.
func (s *SMTP) componer(para, asunto, cuerpo string) []byte {
	var b strings.Builder
	b.WriteString("From: " + s.De + "\r\n")
	b.WriteString("To: " + para + "\r\n")
	// El asunto va codificado: sin esto, las tildes y las eñes llegan rotas.
	b.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", asunto) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	// Estos correos los provoca una acción concreta, no son una lista de
	// difusión; marcarlos evita respuestas automáticas y bucles.
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	b.WriteString("\r\n")
	b.WriteString(cuerpo)
	b.WriteString("\r\n")
	return []byte(b.String())
}

// remitente extrae la dirección de un "Nombre <correo>".
func (s *SMTP) remitente() string {
	if i := strings.LastIndex(s.De, "<"); i >= 0 {
		if j := strings.Index(s.De[i:], ">"); j > 0 {
			return s.De[i+1 : i+j]
		}
	}
	return s.De
}

// Registro escribe los correos en el registro en vez de enviarlos.
//
// Es el modo por defecto en desarrollo: deja ver exactamente qué se habría
// mandado sin necesidad de un servidor de correo, y sin el riesgo de escribir a
// direcciones reales desde una máquina de pruebas.
type Registro struct{ Log *slog.Logger }

var _ Enviador = (*Registro)(nil)

func (r *Registro) Enviar(_ context.Context, para, asunto, cuerpo string) error {
	r.Log.Info("correo (no enviado: modo registro)",
		"para", para, "asunto", asunto, "cuerpo", cuerpo)
	return nil
}
