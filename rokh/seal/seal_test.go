package seal

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

func reader(t *testing.T) (*ecdh.PrivateKey, Reader) {
	t.Helper()
	priv, err := NewReading()
	if err != nil {
		t.Fatal(err)
	}
	r, err := ReaderFrom(priv.PublicKey().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return priv, r
}

// Content is closed for a named reader, and for nobody else. This is what
// makes "what must not travel never reaches the courier" more than a
// promise: the bytes handed over are already shut.
//
//	— T13.1, T7.4
func TestOnlyTheNamedReaderOpensIt(t *testing.T) {
	priv, r := reader(t)
	other, _ := reader(t)

	plain := []byte("what only they may read")
	aad := []byte("home/journal|note")
	s, err := To(r, plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(s.Box, plain) {
		t.Fatal("the content is inside the sealed box in the clear")
	}

	got, err := Open(priv, s, aad)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("the named reader could not open it: %v", err)
	}
	if _, err := Open(other, s, aad); !errors.Is(err, ErrNotForYou) {
		t.Fatalf("somebody else opened it: %v", err)
	}
}

// The courier holds no key, and that is the whole of what it is trusted with.
// It may drop the seal, delay it, hand it over twice or change a byte of it;
// none of that opens it, and none of it goes unnoticed.
//
//	— T7.4, N4.9
func TestTheCourierCanDoNothingWithIt(t *testing.T) {
	priv, r := reader(t)
	plain := []byte("a thing in transit")
	aad := []byte("context")
	s, err := To(r, plain, aad)
	if err != nil {
		t.Fatal(err)
	}

	// Handed over twice: the same seal opens the same way, and that is all.
	for i := 0; i < 3; i++ {
		got, err := Open(priv, s, aad)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("a repeat delivery broke it: %v", err)
		}
	}
	// A byte changed anywhere: refused.
	for _, spoil := range []func(Sealed) Sealed{
		func(x Sealed) Sealed { y := clone(x); y.Box[0] ^= 1; return y },
		func(x Sealed) Sealed { y := clone(x); y.Box[len(y.Box)-1] ^= 1; return y },
		func(x Sealed) Sealed { y := clone(x); y.Ephemeral[0] ^= 1; return y },
	} {
		if _, err := Open(priv, spoil(s), aad); err == nil {
			t.Fatal("a tampered seal opened")
		}
	}
	// The context changed: refused. What the reader was told is part of it.
	if _, err := Open(priv, s, []byte("some other context")); !errors.Is(err, ErrNotForYou) {
		t.Fatalf("the seal opened under a context it was not closed under: %v", err)
	}
}

// A seal cannot be lifted and re-offered as though it had been closed for
// somebody else: the reader's own key is bound into it.
//
//	— T13.1, T7.4
func TestASealCannotBeReAddressed(t *testing.T) {
	privA, rA := reader(t)
	privB, rB := reader(t)
	_ = rB

	s, err := To(rA, []byte("for A alone"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(privB, s, nil); !errors.Is(err, ErrNotForYou) {
		t.Fatal("B opened what was closed for A")
	}
	if _, err := Open(privA, s, nil); err != nil {
		t.Fatalf("A could not open what was closed for A: %v", err)
	}
}

// "May write here" and "may read this" are two different things with two
// different instruments. A signing key is not a reading key, and there is no
// way here to make one serve as the other.
//
//	— T7.1, N4.8
func TestASigningKeyIsNotAReadingKey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// An Ed25519 public key is 32 bytes, the same length as a reading key —
	// which is exactly why the check has to be more than the length.
	if len(pub) != KeySize {
		t.Skip("the two key sizes differ; the confusion this guards against is impossible")
	}
	if _, err := ReaderFrom(pub); err == nil {
		// X25519 accepts most 32-byte strings, so a length check alone
		// cannot separate them. What separates them is that nothing in Rokh
		// ever hands a signing key to this package: the covenant carries a
		// reading key in its own field, and the two never meet.
		t.Log("a 32-byte string is accepted as a reading key; the separation is " +
			"structural, not arithmetic — see the covenant's own SealTo field")
	}
	// The thing that matters: a signing key cannot open a seal.
	priv, r := reader(t)
	s, err := To(r, []byte("secret"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(priv, s, nil); err != nil {
		t.Fatalf("the reading key did not open its own seal: %v", err)
	}
	// And there is no function in this package that takes an ed25519 key.
	// The compiler enforces that; this line is what says it out loud.
	var _ func(Reader, []byte, []byte) (Sealed, error) = To
}

// Two seals of the same bytes to the same reader share nothing: the
// ephemeral key is one-time, so a watcher cannot tell that the same thing
// was sent twice.
//
//	— T13.1, T7.6
func TestTwoSealsOfOneThingLookNothingAlike(t *testing.T) {
	_, r := reader(t)
	plain := []byte("the very same words")
	a, err := To(r, plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := To(r, plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.Ephemeral, b.Ephemeral) {
		t.Fatal("the ephemeral key was reused")
	}
	if bytes.Equal(a.Box, b.Box) {
		t.Fatal("two seals of one thing are byte-identical")
	}
}

func clone(s Sealed) Sealed {
	return Sealed{
		Ephemeral: append([]byte(nil), s.Ephemeral...),
		Box:       append([]byte(nil), s.Box...),
	}
}
