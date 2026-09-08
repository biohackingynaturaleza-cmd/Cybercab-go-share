// Package ratelimit pone un techo a cuántas veces se puede repetir una acción.
//
// Sin él, el formulario de acceso es un sitio donde probar contraseñas a
// millones y el de registro una forma de llenar la base de datos de cuentas
// falsas. El limitador vive en el proceso: no hace falta nada más mientras la
// app corra en una sola máquina, y cuando deje de ser así habrá que llevarlo a
// un almacén compartido.
package ratelimit

import (
	"sync"
	"time"
)

// Limitador reparte fichas por clave con un cubo que se rellena solo.
//
// Un cubo, y no una ventana fija: la ventana fija deja pasar el doble del
// límite justo en su frontera —todo al final de una y todo al principio de la
// siguiente—, que es exactamente el momento que aprovecha quien va en serio.
type Limitador struct {
	mu    sync.Mutex
	cubos map[string]*cubo

	// capacidad es cuántos intentos seguidos se toleran de golpe.
	capacidad float64
	// recarga son las fichas que vuelven al cubo por segundo.
	recarga float64
	// vaciado es cada cuánto se barren los cubos que ya nadie usa.
	vaciado time.Duration
	// ultimoBarrido evita recorrer el mapa entero en cada petición.
	ultimoBarrido time.Time
}

type cubo struct {
	fichas float64
	visto  time.Time
}

// Nuevo crea un limitador que tolera intentos por ventana.
//
// Los intentos se pueden gastar de golpe; a partir de ahí se recuperan poco a
// poco, uno cada ventana/intentos.
func Nuevo(intentos int, ventana time.Duration) *Limitador {
	if intentos < 1 {
		intentos = 1
	}
	if ventana <= 0 {
		ventana = time.Minute
	}
	return &Limitador{
		cubos:     map[string]*cubo{},
		capacidad: float64(intentos),
		recarga:   float64(intentos) / ventana.Seconds(),
		vaciado:   ventana,
	}
}

// Permitir consume una ficha de esa clave.
//
// Devuelve false y cuánto hay que esperar cuando no quedan. La espera se
// devuelve para poder ponerla en la cabecera Retry-After: decir "no" sin decir
// "cuándo" obliga a quien lo recibe a reintentar a ciegas.
func (l *Limitador) Permitir(clave string, now time.Time) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	l.barrer(now)

	c, ok := l.cubos[clave]
	if !ok {
		c = &cubo{fichas: l.capacidad, visto: now}
		l.cubos[clave] = c
	}
	// Rellenar lo que corresponda al tiempo transcurrido desde la última vez.
	if transcurrido := now.Sub(c.visto).Seconds(); transcurrido > 0 {
		c.fichas = min(l.capacidad, c.fichas+transcurrido*l.recarga)
	}
	c.visto = now

	if c.fichas < 1 {
		faltan := (1 - c.fichas) / l.recarga
		return false, time.Duration(faltan * float64(time.Second)).Round(time.Second)
	}
	c.fichas--
	return true, 0
}

// barrer tira los cubos llenos que llevan una ventana sin tocarse: un cubo
// lleno no recuerda nada, así que guardarlo solo gasta memoria.
func (l *Limitador) barrer(now time.Time) {
	if now.Sub(l.ultimoBarrido) < l.vaciado {
		return
	}
	l.ultimoBarrido = now
	for clave, c := range l.cubos {
		if now.Sub(c.visto) >= l.vaciado {
			delete(l.cubos, clave)
		}
	}
}
