package notify

import "sync"

// Grabador guarda los avisos en memoria en vez de enviarlos, para poder
// comprobar en las pruebas que se avisa a quien toca y de lo que toca.
type Grabador struct {
	mu     sync.Mutex
	avisos []Aviso
}

var _ Notificador = (*Grabador)(nil)

func (g *Grabador) Notificar(a Aviso) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.avisos = append(g.avisos, a)
}

// Avisos devuelve una copia de lo enviado hasta ahora.
func (g *Grabador) Avisos() []Aviso {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]Aviso(nil), g.avisos...)
}

// Para devuelve los avisos dirigidos a una dirección.
func (g *Grabador) Para(correo string) []Aviso {
	var out []Aviso
	for _, a := range g.Avisos() {
		if a.Para == correo {
			out = append(out, a)
		}
	}
	return out
}

// Ultimo devuelve el último aviso de un tipo, y si lo hubo.
func (g *Grabador) Ultimo(s Suceso) (Aviso, bool) {
	avisos := g.Avisos()
	for i := len(avisos) - 1; i >= 0; i-- {
		if avisos[i].Suceso == s {
			return avisos[i], true
		}
	}
	return Aviso{}, false
}

// Limpiar olvida lo grabado.
func (g *Grabador) Limpiar() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.avisos = nil
}
