package main

import (
	"testing"

	"rokh/carrier"
	"rokh/ledger"
)

// openForTest opens a carrier the way the reading commands do, so a test can
// read back what a command left: the vessel in its folder, the key through
// the key layer's hooks. It takes no turn and writes nothing.
func openForTest(t *testing.T, dir, pass string) *carrier.Carrier {
	t.Helper()
	c, _, _, err := openCarrier(dir, pass, false)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// ledgerOf reads the ledger back from a carrier by following its references.
func ledgerOf(t *testing.T, c *carrier.Carrier) *ledger.Ledger {
	t.Helper()
	gen, err := c.Get(c.Anchor())
	if err != nil {
		t.Fatal(err)
	}
	heads, err := allHeads(c)
	if err != nil {
		t.Fatal(err)
	}
	l, err := ledger.Load(gen, c.Get, heads)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
