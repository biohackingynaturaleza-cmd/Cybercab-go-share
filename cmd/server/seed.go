package main

import (
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
)

// Puntos reales de Austin para probar el caso central: centro → aeropuerto.
var (
	downtownAustin = domain.Place{Name: "Congress Ave & 6th St, Austin", Point: geo.Point{Lat: 30.2685, Lng: -97.7425}}
	riverside      = domain.Place{Name: "E Riverside Dr, Austin", Point: geo.Point{Lat: 30.2380, Lng: -97.7180}}
	ausAirport     = domain.Place{Name: "Austin-Bergstrom Intl (AUS)", Point: geo.Point{Lat: 30.1975, Lng: -97.6664}}
)

// seedDemo carga un par de usuarios y un trayecto de ejemplo para poder probar
// la API nada más arrancar.
func seedDemo(svc *service.Service) error {
	host, err := svc.CreateUser("Ana", "ana@example.com")
	if err != nil {
		return err
	}
	if _, err := svc.CreateUser("Bruno", "bruno@example.com"); err != nil {
		return err
	}

	_, err = svc.CreateTrip(service.NewTripInput{
		HostID:        host.ID,
		Origin:        downtownAustin,
		Destination:   ausAirport,
		Waypoints:     []geo.Point{riverside.Point},
		DepartureTime: time.Now().UTC().Add(3 * time.Hour),
		Vehicle:       domain.VehicleModelY,
		MaxDetourKm:   2,
		Notes:         "Vuelo a las 18:40, salgo con margen. Cabe maleta grande.",
	})
	return err
}
