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
