package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/ratelimit"
)

// Los techos de cada tipo de acción. Son generosos para quien se equivoca de
// contraseña un par de veces y estrechos para quien prueba diccionarios.
const (
	// IntentosPorIP son las peticiones de acceso que admite una dirección.
	// Cubre a familias enteras detrás del mismo router sin dejar hueco a un
	// ataque por fuerza bruta, que necesita órdenes de magnitud más.
	IntentosPorIP     = 40
	VentanaPorIP      = 10 * time.Minute
	IntentosPorCuenta = 6
	VentanaPorCuenta  = 15 * time.Minute
	// Las verificaciones de identidad cuestan dinero por cada una que se abre:
	// el techo protege la factura, no solo el servidor.
	IntentosVerificacion = 10
	VentanaVerificacion  = time.Hour
)

// limites agrupa los techos de las rutas que se pueden abusar.
//
// Van por separado porque protegen cosas distintas: el de IP frena a quien
// dispara desde una máquina, y el de cuenta frena a quien reparte los intentos
// contra un mismo correo desde muchas máquinas, que al primero se le escapa.
type limites struct {
	porIP           *ratelimit.Limitador
	porCuenta       *ratelimit.Limitador
	porVerificacion *ratelimit.Limitador
	// confiarEnProxy dice si se puede creer la cabecera X-Forwarded-For. Solo
	// vale cuando delante hay un proxy nuestro que la reescribe: si no, quien
	// quiera saltarse el límite solo tiene que inventarse una IP distinta en
	// cada petición.
	confiarEnProxy bool
}

func limitesPorDefecto() *limites {
	return &limites{
		porIP:           ratelimit.Nuevo(IntentosPorIP, VentanaPorIP),
		porCuenta:       ratelimit.Nuevo(IntentosPorCuenta, VentanaPorCuenta),
		porVerificacion: ratelimit.Nuevo(IntentosVerificacion, VentanaVerificacion),
	}
}

// WithProxyDeConfianza hace que las IP se lean de X-Forwarded-For.
//
// Actívalo solo si delante hay un proxy propio que reescriba esa cabecera. Sin
// proxy delante, creerla es peor que no mirarla: cualquiera se inventa una IP
// por petición y el límite deja de existir.
func WithProxyDeConfianza() Option {
	return func(s *Server) { s.limites.confiarEnProxy = true }
}

// limitarPorIP envuelve un manejador con el techo por dirección de origen.
func (s *Server) limitarPorIP(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.deja(w, s.limites.porIP, "ip:"+s.origen(r)) {
			return
		}
		h(w, r)
	}
}

// dejaCuenta aplica el techo por correo. Se llama ya dentro del manejador,
// cuando el cuerpo está leído y se sabe contra qué cuenta va la petición.
func (s *Server) dejaCuenta(w http.ResponseWriter, email string) bool {
	if email == "" {
		return true
	}
	return s.deja(w, s.limites.porCuenta, "cuenta:"+normalizarClave(email))
}

func (s *Server) dejaVerificacion(w http.ResponseWriter, userID string) bool {
	return s.deja(w, s.limites.porVerificacion, "verif:"+userID)
}

// deja consulta el limitador y, si dice que no, contesta 429 con el tiempo que
// hay que esperar.
func (s *Server) deja(w http.ResponseWriter, l *ratelimit.Limitador, clave string) bool {
	ok, espera := l.Permitir(clave, time.Now())
	if ok {
		return true
	}
	segundos := int(espera.Seconds())
	if segundos < 1 {
		segundos = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(segundos))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"error":           "demasiados intentos: espera un poco y vuelve a probar",
		"reintentar_en_s": segundos,
	})
	return false
}

// origen es la dirección desde la que llega la petición.
func (s *Server) origen(r *http.Request) string {
	if s.limites.confiarEnProxy {
		// El primero de la lista es el cliente; los siguientes son los proxies
		// por los que ha ido pasando.
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			primero, _, _ := strings.Cut(xff, ",")
			if ip := strings.TrimSpace(primero); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// normalizarClave iguala las variantes del mismo correo para que cambiar una
// mayúscula no regale otra tanda de intentos.
func normalizarClave(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
