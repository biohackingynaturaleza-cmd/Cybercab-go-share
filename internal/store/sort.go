package store

import (
	"sort"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
)

func sortBookings(bs []*domain.Booking) {
	sort.SliceStable(bs, func(i, j int) bool {
		if !bs[i].CreatedAt.Equal(bs[j].CreatedAt) {
			return bs[i].CreatedAt.Before(bs[j].CreatedAt)
		}
		return bs[i].ID < bs[j].ID
	})
}

func sortIncidencias(is []*domain.Incidencia) {
	sort.SliceStable(is, func(i, j int) bool {
		if !is[i].CreatedAt.Equal(is[j].CreatedAt) {
			return is[i].CreatedAt.After(is[j].CreatedAt)
		}
		return is[i].ID < is[j].ID
	})
}
