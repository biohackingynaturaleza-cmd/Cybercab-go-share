package main

import (
	"context"
	"time"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/service"
)

// Puntos reales de Austin para probar el caso central: centro → aeropuerto.
var (
	downtownAustin = domain.Place{Name: "Downtown (Congress & 6th)", Point: geo.Point{Lat: 30.2685, Lng: -97.7425}}
	riverside      = domain.Place{Name: "East Riverside", Point: geo.Point{Lat: 30.2380, Lng: -97.7180}}
	ausAirport     = domain.Place{Name: "Austin-Bergstrom (AUS)", Point: geo.Point{Lat: 30.1975, Lng: -97.6664}}
)

// demoPassword es la contraseña de las cuentas de ejemplo. Solo se usa con
// SEED_DEMO=1, que nunca debe activarse en producción.
const demoPassword = "cybercab-demo-2026"

// seedDemo carga un par de usuarios y un trayecto de ejemplo para poder probar
// la API nada más arrancar.
func seedDemo(ctx context.Context, svc *service.Service) error {
	host, err := svc.Register(service.RegisterInput{
		Name: "Ana", Email: "ana@example.com", Password: demoPassword,
		Idioma: "es", AceptaTerminos: true,
	})
	if err != nil {
		return err
	}
	if _, err := svc.Register(service.RegisterInput{
		Name: "Bruno", Email: "bruno@example.com", Password: demoPassword,
		Idioma: "en", AceptaTerminos: true,
	}); err != nil {
		return err
	}

	_, err = svc.CreateTrip(ctx, service.NewTripInput{
		HostID:        host.User.ID,
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
