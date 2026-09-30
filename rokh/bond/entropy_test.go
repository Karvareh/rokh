package bond_test

import (
	"crypto/rand"

	"rokh/event"
)

// The core imports no randomness; whoever runs it gives it some, as each
// command does. Here the tests are that host.
func init() { event.Entropy = rand.Reader }
