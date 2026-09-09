package service

import (
	"context"
	"time"
)

// IntervaloDeLiquidacion es cada cuánto mira el programador si hay un periodo
// que cerrar.
//
// Cada hora y no una vez al mes: un cron mensual que se pierde su única cita
// —porque el servidor estaba reiniciándose— no vuelve a intentarlo hasta el mes
// siguiente. Mirar seguido y no hacer nada es barato; cerrar tarde, no.
const IntervaloDeLiquidacion = time.Hour

// Programador cierra los periodos vencidos sin que nadie tenga que acordarse.
//
// Es un bucle y no un cron del sistema para que el binario siga siendo uno
// solo: desplegar la app no puede exigir además configurar una tarea aparte
// que, si falta, hace que nadie cobre y nadie se entere.
type Programador struct {
	svc *Service
	// Cada cuánto se mira. Se puede ajustar en las pruebas.
	Intervalo time.Duration
}

// NuevoProgramador construye el bucle de liquidación.
func NuevoProgramador(svc *Service) *Programador {
	return &Programador{svc: svc, Intervalo: IntervaloDeLiquidacion}
}

// Arrancar corre hasta que se cancele el contexto.
//
// Cierra lo vencido nada más arrancar: si el servicio estuvo caído durante el
// cambio de mes, esperar otra hora a la primera vuelta del reloj sería alargar
// gratis el tiempo que alguien lleva sin cobrar.
func (p *Programador) Arrancar(ctx context.Context) {
	p.unaVuelta()

	t := time.NewTicker(p.Intervalo)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.unaVuelta()
		}
	}
}

// unaVuelta cierra los periodos vencidos. Un fallo no detiene el bucle: se
// queda anotado y se reintenta a la siguiente.
func (p *Programador) unaVuelta() {
	hechas, err := p.svc.LiquidarPendientes(p.svc.cfg.Now())
	if err != nil {
		p.svc.log("no se pudo cerrar un periodo", err)
	}
	if len(hechas) == 0 || p.svc.cfg.Log == nil {
		return
	}
	for _, l := range hechas {
		p.svc.cfg.Log.Info("periodo liquidado",
			"liquidacion", l.ID,
			"periodo", l.Desde.Format("2006-01"),
			"apuntes", l.ApuntesLiquidados,
			"movimientos", len(l.Instrucciones),
			"comision_cents", l.ComisionTotalCents)
	}
}
