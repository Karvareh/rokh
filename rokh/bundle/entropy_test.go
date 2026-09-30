package bundle_test

import (
	"crypto/rand"

	"rokh/event"
)

// The host gives the core its randomness (event.Entropy); a test is the host
// here.
func init() {
	if event.Entropy == nil {
		event.Entropy = rand.Reader
	}
}
