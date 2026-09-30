package daemon

import (
	"crypto/rand"

	"rokh/event"
)

// The daemon is a host: it gives the core its system's randomness (C5).
func init() {
	if event.Entropy == nil {
		event.Entropy = rand.Reader
	}
}
