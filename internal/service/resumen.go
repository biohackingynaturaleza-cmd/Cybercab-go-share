package service

import (
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

// Resumen es lo que una persona ha conseguido usando el servicio.
//
// No es una métrica de vanidad: el ahorro acumulado es la razón por la que
// alguien vuelve. Enseñarlo es enseñar el valor que ya ha recibido.
type Resumen struct {
	UserID string `json:"user_id"`
	// ViajesCompartidos son los trayectos que ya se han hecho compartiendo.
	ViajesCompartidos int `json:"viajes_compartidos"`
	// ViajesPendientes son los que están confirmados pero aún no ocurridos.
	ViajesPendientes int `json:"viajes_pendientes"`
	// PeticionesPorResponder son las plazas que otros le han pedido y aún no
	// ha decidido. Es lo único de este resumen que exige actuar.
	PeticionesPorResponder int `json:"peticiones_por_responder"`

	KmCompartidos float64 `json:"km_compartidos"`
	AhorroCents   int64   `json:"ahorro_cents"`
	// AhorroPrevistoCents es lo que se ahorrará en los viajes ya confirmados
	// que aún no han ocurrido. Enseñar 0 $ a quien acaba de reservar su primer
	// viaje es desanimarle justo cuando más ilusión tiene.
	AhorroPrevistoCents int64 `json:"ahorro_previsto_cents"`
	// SinCompartirCents es lo que habría costado hacer esos mismos viajes en
	// solitario. El ahorro se entiende comparándolo con esto.
	SinCompartirCents int64 `json:"sin_compartir_cents"`

	Nivel trust.Level `json:"nivel"`
	// SiguienteNivel y QueFalta indican cómo subir, para que el nivel no
	// parezca una etiqueta fija sino un camino.
	SiguienteNivel string            `json:"siguiente_nivel,omitempty"`
	QueFalta       []trust.CheckKind `json:"que_falta,omitempty"`
}

// ResumenDe reúne lo que esa persona lleva ahorrado y lo que tiene pendiente.
func (s *Service) ResumenDe(userID string) (*Resumen, error) {
	u, err := s.store.GetUser(userID)
	if err != nil {
		return nil, err
	}
	checks, err := s.store.ChecksByUser(userID)
	if err != nil {
		return nil, err
	}

	now := s.cfg.Now()
	nivel := trust.LevelOf(checks, trust.Stats{
		CompletedTrips: u.RideCount, Rating: u.Rating, RatingCount: u.RatingCount,
	}, now)

	r := &Resumen{UserID: userID, Nivel: nivel}
	if falta := trust.Missing(checks, trust.LevelVerificado, now); len(falta) > 0 {
		r.SiguienteNivel = trust.LevelVerificado.Label()
		r.QueFalta = falta
	}

	// Como pasajero: lo que se ahorra es la diferencia entre lo que paga y lo
	// que le habría costado ese mismo camino en un coche para él solo.
	misReservas, err := s.store.BookingsByPassenger(userID)
	if err != nil {
		return nil, err
	}
	for _, b := range misReservas {
		if b.Status != domain.BookingConfirmed {
			continue
		}
		t, err := s.store.GetTrip(b.TripID)
		if err != nil {
			continue
		}
		if t.Status == domain.TripCancelled {
			continue
		}

		km := b.SharedKm()
		solo := s.tripCost(km)
		ahorro := solo - b.PriceCents
		if ahorro < 0 {
			ahorro = 0
		}

		if t.Status == domain.TripCompleted {
			r.ViajesCompartidos++
			r.KmCompartidos += km
			r.SinCompartirCents += solo
			r.AhorroCents += ahorro
			continue
		}
		r.ViajesPendientes++
		r.AhorroPrevistoCents += ahorro
	}

	// Como quien organiza: lo que aportan los demás es lo que deja de pagar.
	abiertos, err := s.store.ListOpenTrips()
	if err != nil {
		return nil, err
	}
	for _, t := range abiertos {
		if t.HostID != userID {
			continue
		}
		reservas, err := s.store.BookingsByTrip(t.ID)
		if err != nil {
			continue
		}
		for _, b := range reservas {
			if b.Status == domain.BookingPending {
				r.PeticionesPorResponder++
			}
		}
	}

	return r, nil
}
