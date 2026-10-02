// Package proof is the adversarial witness: it attacks Rokh's executable
// axioms from every door it has — the ledger, the daemon's socket protocol,
// the carrier on disk, the bundle layer, the byte grammar — and records that
// each attack is refused, by name, with a code a program can read.
//
// These tests witness refusals. Every one of them tries to do something the
// treatise forbids and asserts that the core says no in a way a caller can
// act on: a typed verdict from the ledger, a stable code and a one-word
// record state from the daemon, a named error from the carrier. A test here
// that merely reads the source and finds the right words in it would prove
// nothing; every assertion below is made against behaviour.
//
// The line these tests never cross, and the reason the whole package exists:
// a signature proves only who signed which bytes (T12.5), never consent or
// truth.
// Nothing here checks that a payload is honest, because nothing anywhere
// can; what is checked is that the refusals are real, that a settled verdict
// never reopens, and that no door quietly widens a right.
//
// Each test's doc comment names the treatise clauses it witnesses.
package proof
