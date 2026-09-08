package ratelimit_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/ratelimit"
)

func TestSeAgotanLasFichasYSeRecuperanConElTiempo(t *testing.T) {
	l := ratelimit.Nuevo(3, time.Minute)
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

	for i := range 3 {
		if ok, _ := l.Permitir("ana", now); !ok {
			t.Fatalf("el intento %d debería pasar", i+1)
		}
	}
	ok, espera := l.Permitir("ana", now)
	if ok {
		t.Fatal("el cuarto intento seguido debería sobrar")
	}
	if espera <= 0 || espera > time.Minute {
		t.Fatalf("espera = %v, esperaba algo entre 0 y un minuto", espera)
	}

	// Pasada la espera anunciada, entra otro. Si no fuese así, la cabecera
	// Retry-After estaría mintiendo.
	if ok, _ := l.Permitir("ana", now.Add(espera)); !ok {
		t.Fatal("tras esperar lo que dijimos, debería pasar")
	}
}

func TestCadaClaveTieneSuPropioCubo(t *testing.T) {
	// Que alguien agote sus intentos no puede dejar fuera a los demás.
	l := ratelimit.Nuevo(2, time.Minute)
	now := time.Now()

	l.Permitir("ana", now)
	l.Permitir("ana", now)
	if ok, _ := l.Permitir("ana", now); ok {
		t.Fatal("ana debería estar agotada")
	}
	if ok, _ := l.Permitir("berta", now); !ok {
		t.Fatal("berta no ha gastado nada y quedó fuera")
	}
}

func TestElCuboNoAcumulaMasDeSuCapacidad(t *testing.T) {
	// Estar una semana sin entrar no da derecho a mil intentos de golpe.
	l := ratelimit.Nuevo(3, time.Minute)
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	l.Permitir("ana", now)

	tarde := now.Add(7 * 24 * time.Hour)
	for i := range 3 {
		if ok, _ := l.Permitir("ana", tarde); !ok {
			t.Fatalf("el intento %d debería pasar", i+1)
		}
	}
	if ok, _ := l.Permitir("ana", tarde); ok {
		t.Fatal("el cubo acumuló más fichas de las que caben")
	}
}

func TestLimitadorNiloDejaPasar(t *testing.T) {
	// Un limitador sin configurar no puede tirar la aplicación.
	var l *ratelimit.Limitador
	if ok, _ := l.Permitir("ana", time.Now()); !ok {
		t.Fatal("un limitador nilo tiene que dejar pasar")
	}
}

func TestUsoConcurrente(t *testing.T) {
	// Con -race: la comprobación de verdad es que no haya carrera.
	l := ratelimit.Nuevo(50, time.Second)
	now := time.Now()
	var wg sync.WaitGroup
	var mu sync.Mutex
	pasaron := 0
	for i := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := l.Permitir(fmt.Sprintf("clave-%d", i%4), now); ok {
				mu.Lock()
				pasaron++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if pasaron != 100 {
		t.Fatalf("pasaron %d de 100: cuatro claves con 50 fichas cada una caben de sobra", pasaron)
	}
}
