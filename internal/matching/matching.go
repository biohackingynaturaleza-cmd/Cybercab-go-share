// Package matching decide qué trayectos abiertos sirven a quien busca sitio.
package matching

import (
	"sort"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/pricing"
)

// DefaultMaxWalkKm es lo que asumimos que alguien acepta caminar hasta el punto
// de recogida si no dice otra cosa.
const DefaultMaxWalkKm = 1.5

// MinSharedKm evita ofrecer trayectos en los que apenas se comparte camino.
const MinSharedKm = 0.5

// Query es la búsqueda de un pasajero: de dónde a dónde y en qué horquilla.
type Query struct {
	Pickup            geo.Point
	Dropoff           geo.Point
	EarliestDeparture time.Time
	LatestDeparture   time.Time
	Seats             int
	MaxWalkKm         float64
}

func (q Query) normalized() Query {
	if q.Seats < 1 {
		q.Seats = 1
	}
	if q.MaxWalkKm <= 0 {
		q.MaxWalkKm = DefaultMaxWalkKm
	}
	return q
}

// Match es un trayecto compatible, ya valorado.
type Match struct {
	Trip *domain.Trip `json:"trip"`
	// PickupAlongKm y DropoffAlongKm marcan el tramo compartido sobre la ruta.
	PickupAlongKm  float64 `json:"pickup_along_km"`
	DropoffAlongKm float64 `json:"dropoff_along_km"`
	// PickupWalkKm y DropoffWalkKm son las distancias a pie hasta la ruta.
	PickupWalkKm  float64 `json:"pickup_walk_km"`
	DropoffWalkKm float64 `json:"dropoff_walk_km"`
	SharedKm      float64 `json:"shared_km"`
	// Coverage es la fracción del viaje pedido que cubre este trayecto.
	Coverage            float64 `json:"coverage"`
	EstimatedPriceCents int64   `json:"estimated_price_cents"`
	Score               float64 `json:"score"`
}

// Occupancy describe las plazas ya comprometidas de un trayecto, para poder
// estimar el precio del pasajero nuevo.
type Occupancy map[string][]pricing.Occupant

// Find devuelve los trayectos que encajan con la búsqueda, del mejor al peor.
func Find(trips []*domain.Trip, q Query, occ Occupancy, tariff pricing.Tariff, speedKmh float64) []Match {
	q = q.normalized()
	directKm := geo.DistanceKm(q.Pickup, q.Dropoff)

	matches := make([]Match, 0, len(trips))
	for _, t := range trips {
		m, ok := evaluate(t, q, directKm, occ[t.ID], tariff, speedKmh)
		if ok {
			matches = append(matches, m)
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Trip.DepartureTime.Before(matches[j].Trip.DepartureTime)
	})
	return matches
}

func evaluate(t *domain.Trip, q Query, directKm float64, current []pricing.Occupant, tariff pricing.Tariff, speedKmh float64) (Match, bool) {
	if !t.Bookable() || t.SeatsAvailable() < q.Seats {
		return Match{}, false
	}
	if !q.EarliestDeparture.IsZero() && t.DepartureTime.Before(q.EarliestDeparture) {
		return Match{}, false
	}
	if !q.LatestDeparture.IsZero() && t.DepartureTime.After(q.LatestDeparture) {
		return Match{}, false
	}

	pickup := t.Route.Project(q.Pickup)
	dropoff := t.Route.Project(q.Dropoff)

	// La recogida y la bajada tienen que quedar de camino...
	maxOff := maxFloat(q.MaxWalkKm, t.MaxDetourKm)
	if pickup.OffRouteKm > maxOff || dropoff.OffRouteKm > maxOff {
		return Match{}, false
	}
	// ...y en el sentido de la marcha: no se puede bajar antes de subir.
	shared := dropoff.AlongKm - pickup.AlongKm
	if shared < MinSharedKm {
		return Match{}, false
	}

	routeKm := t.DistanceKm()
	price := pricing.EstimateSeatPrice(
		tariff.TripCostCents(routeKm, durationMin(routeKm, speedKmh)),
		routeKm,
		current,
		pricing.Occupant{ID: "__candidate__", StartKm: pickup.AlongKm, EndKm: dropoff.AlongKm, Seats: q.Seats},
		t.HostID,
	)

	coverage := 1.0
	if directKm > 0 {
		coverage = minFloat(shared/directKm, 1)
	}

	return Match{
		Trip:                t,
		PickupAlongKm:       pickup.AlongKm,
		DropoffAlongKm:      dropoff.AlongKm,
		PickupWalkKm:        pickup.OffRouteKm,
		DropoffWalkKm:       dropoff.OffRouteKm,
		SharedKm:            shared,
		Coverage:            coverage,
		EstimatedPriceCents: price,
		Score:               score(coverage, pickup.OffRouteKm, dropoff.OffRouteKm, maxOff),
		// Score pondera cubrir el viaje pedido frente a lo que toca caminar.
	}, true
}

// score va de 0 a 1: prima cubrir el trayecto pedido y penaliza el paseo hasta
// los puntos de recogida y bajada.
func score(coverage, pickupWalk, dropoffWalk, maxOff float64) float64 {
	walkPenalty := 0.0
	if maxOff > 0 {
		walkPenalty = (pickupWalk + dropoffWalk) / (2 * maxOff)
	}
	return 0.75*coverage + 0.25*(1-minFloat(walkPenalty, 1))
}

// durationMin estima el tiempo de trayecto a partir de una velocidad media.
func durationMin(distanceKm, speedKmh float64) float64 {
	if speedKmh <= 0 {
		return 0
	}
	return distanceKm / speedKmh * 60
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
