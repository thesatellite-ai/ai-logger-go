package store

import (
	"fmt"

	"github.com/google/uuid"
)

// NewID returns a fresh UUID v7 (time-sortable) as a string.
// Panics only if the underlying crypto RNG fails, which is treated as fatal.
func NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		panic(fmt.Errorf("uuid v7: %w", err))
	}
	return id.String()
}
