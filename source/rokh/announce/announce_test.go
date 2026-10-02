package announce

import (
	"bytes"
	"strings"
	"testing"

	"rokh/frame"
)

func ids(a, b string) (frame.ID, frame.ID) {
	return frame.Hash([]byte(a)), frame.Hash([]byte(b))
}

func TestRoundTrip(t *testing.T) {
	anchor, head := ids("ledger", "head")
	a := New(anchor, head, 42, "home/journal/today")
	b, err := a.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back != a {
		t.Fatalf("round trip changed the announcement:\n  %+v\n  %+v", a, back)
	}
	if !back.MatchesLedger(anchor) || !back.MatchesHead(head) {
		t.Fatal("short ids do not match the full ids they came from")
	}
	other, _ := ids("other", "x")
	if back.MatchesLedger(other) {
		t.Fatal("matched an unrelated ledger")
	}
}

// The whole point of this package: news always fits the smallest LoRa frame.
func TestAlwaysFitsSmallestLoRaFrame(t *testing.T) {
	anchor, head := ids("ledger", "head")

	// Bare: no metadata at all.
	bare, err := New(anchor, head, 0, "").Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(bare) != HeaderSize {
		t.Fatalf("bare announcement is %d bytes, want %d", len(bare), HeaderSize)
	}

	// Full: every optional field at its maximum.
	full, err := New(anchor, head, 65535, strings.Repeat("a", MaxAddressHint)).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(full) > MaxSize {
		t.Fatalf("full announcement is %d bytes, over the %d-byte SF12 frame", len(full), MaxSize)
	}
	t.Logf("bare %d bytes, full %d bytes, SF12 frame %d bytes", len(bare), len(full), MaxSize)
}

func TestAddressHintIsClippedOnARuneBoundary(t *testing.T) {
	anchor, head := ids("l", "h")
	// Multi-byte runes that would straddle the limit if cut blindly.
	long := strings.Repeat("é", 40) // two bytes each
	a := New(anchor, head, 0, long)
	if len(a.Address) > MaxAddressHint {
		t.Fatalf("hint not clipped: %d bytes", len(a.Address))
	}
	b, err := a.Encode()
	if err != nil {
		t.Fatalf("clipped hint did not encode: %v", err)
	}
	back, err := Decode(b)
	if err != nil {
		t.Fatalf("clipped hint did not decode: %v", err)
	}
	if !strings.HasPrefix(long, back.Address) {
		t.Fatal("clipped hint is not a prefix of the original")
	}
}

func TestCanonicalForm(t *testing.T) {
	anchor, head := ids("l", "h")
	a := New(anchor, head, 7, "x")
	b, err := a.Encode()
	if err != nil {
		t.Fatal(err)
	}
	// Metadata tags ascend, so one announcement has exactly one encoding.
	again, _ := New(anchor, head, 7, "x").Encode()
	if !bytes.Equal(b, again) {
		t.Fatal("encoding is not deterministic")
	}
}

func TestRejectsMalformed(t *testing.T) {
	anchor, head := ids("l", "h")
	good, err := New(anchor, head, 7, "x").Encode()
	if err != nil {
		t.Fatal(err)
	}

	bad := map[string][]byte{
		"empty":         {},
		"short":         good[:HeaderSize-1],
		"bad magic":     append([]byte{'X'}, good[1:]...),
		"bad version":   append([]byte{good[0], 0x09}, good[2:]...),
		"oversized":     make([]byte, MaxSize+1),
		"truncated tlv": good[:len(good)-1],
	}
	for name, b := range bad {
		if _, err := Decode(b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// Unknown flag bit.
	f := append([]byte(nil), good...)
	f[2] |= 0x80
	if _, err := Decode(f); err == nil {
		t.Error("unknown flag bit accepted")
	}

	// Flags claim metadata but none follows.
	noMeta, _ := New(anchor, head, 0, "").Encode()
	lying := append([]byte(nil), noMeta...)
	lying[2] |= flagMeta
	if _, err := Decode(lying); err == nil {
		t.Error("flags claiming absent metadata accepted")
	}

	// Metadata present but flags do not say so.
	quiet := append([]byte(nil), good...)
	quiet[2] &^= flagMeta
	if _, err := Decode(quiet); err == nil {
		t.Error("metadata present with flags unset accepted")
	}

	// Unknown metadata tag.
	unknown := append(append([]byte(nil), noMeta...), 0x7F, 1, 0x00)
	unknown[2] |= flagMeta
	if _, err := Decode(unknown); err == nil {
		t.Error("unknown metadata tag accepted")
	}

	// Descending metadata tags.
	desc := append([]byte(nil), noMeta...)
	desc[2] |= flagMeta
	desc = append(desc, tagAddress, 1, 'x', tagCount, 2, 0, 7)
	if _, err := Decode(desc); err == nil {
		t.Error("descending metadata tags accepted")
	}
}

// An announcement carries no authority: it is a hint, and the worst a forged
// one can do is cost a wasted fetch. This test states that the type has no
// signature and no key material to begin with.
// The courier is a carrier and is not trusted. Whatever it drops, delays,
// reorders or repeats changes nothing in what is accepted.
//
//	— N4.9
func TestAnnouncementCarriesNoAuthority(t *testing.T) {
	anchor, head := ids("l", "h")
	b, err := New(anchor, head, 1, "a").Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) >= frame.SigSize {
		// 51 bytes max, an Ed25519 signature alone is 64: there is no room
		// for one, and that is deliberate.
		t.Fatalf("announcement grew to %d bytes; it must stay under one frame", len(b))
	}
	// Anyone can forge one; nothing downstream may treat it as evidence.
	forged, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	wrong := frame.Hash([]byte("some other event"))
	if forged.MatchesHead(wrong) {
		t.Fatal("short id matched an unrelated event")
	}
}

// ---------- hello ----------

func TestHelloRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	h, err := NewHello(key)
	if err != nil {
		t.Fatal(err)
	}
	b := h.Encode()
	if len(b) != HelloSize {
		t.Fatalf("hello is %d bytes, want %d", len(b), HelloSize)
	}
	if len(b) > MaxSize {
		t.Fatalf("hello is %d bytes, over one SF12 frame (%d)", len(b), MaxSize)
	}
	back, err := DecodeHello(b)
	if err != nil {
		t.Fatal(err)
	}
	if back != h {
		t.Fatal("hello did not survive the round trip")
	}
	t.Logf("a knock is %d bytes; an SF12 frame holds %d", len(b), MaxSize)
}

func TestHelloRejectsMalformed(t *testing.T) {
	good := Hello{}.Encode()
	bad := map[string][]byte{
		"empty":     {},
		"short":     good[:HelloSize-1],
		"long":      append(append([]byte(nil), good...), 0),
		"bad magic": append([]byte{'X'}, good[1:]...),
	}
	for name, b := range bad {
		if _, err := DecodeHello(b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	ver := append([]byte(nil), good...)
	ver[1] = 0x09
	if _, err := DecodeHello(ver); err == nil {
		t.Error("unknown hello version accepted")
	}
	if _, err := NewHello([]byte{1, 2, 3}); err == nil {
		t.Error("a key of the wrong size was accepted")
	}
}

// Neither decoder may ever accept the other's frame. That is why a hello has
// its own magic byte rather than being a new version of an announcement.
func TestHelloAndAnnounceNeverCollide(t *testing.T) {
	anchor, head := ids("l", "h")
	ann, err := New(anchor, head, 3, "a").Encode()
	if err != nil {
		t.Fatal(err)
	}
	hello := Hello{}.Encode()

	if _, err := DecodeHello(ann); err == nil {
		t.Fatal("the hello decoder accepted an announcement")
	}
	if _, err := Decode(hello); err == nil {
		t.Fatal("the announcement decoder accepted a hello")
	}
}

// A knock carries no authority: 34 bytes leaves no room for a signature, and
// that is deliberate. It says a key exists, and nothing else.
func TestHelloCarriesNoAuthority(t *testing.T) {
	h := Hello{}.Encode()
	if len(h) >= 64 {
		t.Fatalf("a hello is %d bytes; it must stay too small to carry a signature", len(h))
	}
}
