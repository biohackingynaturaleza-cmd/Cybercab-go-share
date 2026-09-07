package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// newID genera identificadores ordenables por fecha de creación: el prefijo
// temporal hace que ordenar por ID equivalga a ordenar por antigüedad.
func newID(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s_%d%s", prefix, time.Now().UTC().UnixNano(), hex.EncodeToString(b[:]))
}
