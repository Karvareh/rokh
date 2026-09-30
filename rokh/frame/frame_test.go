package frame

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func mustBuild(t *testing.T, fs Fields) []byte {
	t.Helper()
	signed, err := BuildHead(KindEvent, fs)
	if err != nil {
		t.Fatalf("BuildHead: %v", err)
	}
	raw, err := SealHead(signed, make([]byte, SigSize))
	if err != nil {
		t.Fatalf("SealHead: %v", err)
	}
	return raw
}

// Parse reads a head that must be the whole input: what follows a head is a
// body, and these tests have none.
func Parse(raw []byte) (*Head, error) {
	h, rest, err := ParseHead(raw)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, errors.New("trailing bytes after the head")
	}
	return h, nil
}

// rawHead hand-builds a head around fields, because BuildHead sorts for you.
func rawHead(fields []byte) []byte {
	raw := append([]byte(Magic), byte(KindEvent))
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(fields)))
	raw = append(raw, n[:]...)
	raw = append(raw, fields...)
	return append(raw, make([]byte, SigSize)...)
}

func TestRoundTripAndCanonicalOrder(t *testing.T) {
	// The same fields in two different orders must produce identical bytes.
	a := mustBuild(t, Fields{{3, []byte("three")}, {1, []byte("one")}, {2, []byte("two")}})
	b := mustBuild(t, Fields{{1, []byte("one")}, {2, []byte("two")}, {3, []byte("three")}})
	if !bytes.Equal(a, b) {
		t.Fatal("canonical form broken: input order changed output bytes")
	}
	f, err := Parse(a)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(f.Fields) != 3 || f.Fields[0].Tag != 1 || f.Fields[2].Tag != 3 {
		t.Fatalf("fields decoded wrong: %+v", f.Fields)
	}
	re, err := f.Reencode()
	if err != nil {
		t.Fatalf("Reencode: %v", err)
	}
	if !bytes.Equal(re, f.Raw) {
		t.Fatal("re-encoding did not reproduce the original bytes")
	}
	if f.ID != Hash(a) {
		t.Fatal("id is not the hash of the whole frame")
	}
}

// The signed region is the stored region is the hashed region.
//
//	— T3.1, N-Axiom3
func TestSignedRegionIsEverythingBeforeSig(t *testing.T) {
	raw := mustBuild(t, Fields{{1, []byte("x")}})
	f, _ := Parse(raw)
	if len(f.Signed) != len(raw)-SigSize {
		t.Fatalf("signed region %d, want %d", len(f.Signed), len(raw)-SigSize)
	}
	if !bytes.Equal(f.Raw, append(append([]byte{}, f.Signed...), f.Sig...)) {
		t.Fatal("Signed||Sig must reproduce Raw exactly")
	}
}

func TestParseDoesNotAliasInput(t *testing.T) {
	raw := mustBuild(t, Fields{{1, []byte("value")}})
	f, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), f.Raw...)
	for i := range raw {
		raw[i] ^= 0xFF
	}
	if !bytes.Equal(f.Raw, before) {
		t.Fatal("Parse result aliases the caller's buffer")
	}
}

// One content has exactly one correct encoding. A second encoding — even
// one extra byte of padding — is refused, not repaired.
//
//	— N4.1
func TestRejectsNonCanonical(t *testing.T) {
	good := mustBuild(t, Fields{{1, []byte("a")}, {2, []byte("b")}})

	corrupt := func(name string, f func([]byte) []byte) {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(f(append([]byte{}, good...))); err == nil {
				t.Fatal("accepted, should have been rejected")
			}
		})
	}
	corrupt("bad magic", func(b []byte) []byte { b[0] = 'X'; return b })
	corrupt("unknown kind", func(b []byte) []byte { b[4] = 0x7F; return b })
	corrupt("truncated", func(b []byte) []byte { return b[:len(b)-1] })
	corrupt("trailing byte", func(b []byte) []byte { return append(b, 0x00) })

	// Descending fields, hand-built, because Build sorts for you.
	var body []byte
	for _, tag := range []byte{2, 1} {
		body = append(body, 0, tag, 0, 0, 0, 1, 'x')
	}
	raw := rawHead(body)
	if _, err := Parse(raw); err == nil {
		t.Fatal("descending fields accepted")
	}

	// Duplicate tag.
	body = nil
	for i := 0; i < 2; i++ {
		body = append(body, 0, 1, 0, 0, 0, 1, 'x')
	}
	raw = rawHead(body)
	if _, err := Parse(raw); err == nil {
		t.Fatal("duplicate field accepted")
	}

	// Empty value.
	raw = rawHead([]byte{0, 1, 0, 0, 0, 0})
	if _, err := Parse(raw); err == nil {
		t.Fatal("empty field accepted")
	}
}

func TestEncodeRejectsEmptyAndDuplicate(t *testing.T) {
	if _, err := EncodeFields(Fields{{1, nil}}); err == nil {
		t.Fatal("empty value accepted")
	}
	if _, err := EncodeFields(Fields{{1, []byte("a")}, {1, []byte("b")}}); err == nil {
		t.Fatal("duplicate tag accepted")
	}
}

// A truncated id must never pass through the parser: short ids are for
// pointing, never for trusting.
// A short name is a guide for the eye and never a reference.
//
//	— T3.5
func TestParseIDRefusesShortForms(t *testing.T) {
	var a ID
	a[0] = 1
	full := a.String()
	for _, bad := range []string{"", "abcd", full[:16], full[:63], full + "00"} {
		if _, err := ParseID(bad); err == nil {
			t.Errorf("accepted short or long id %q", bad)
		}
	}
	got, err := ParseID(full)
	if err != nil || got != a {
		t.Fatalf("round trip failed: %v", err)
	}
}

func TestIDCompareIsByteOrder(t *testing.T) {
	var a, b ID
	a[0], b[0] = 1, 2
	if a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(a) != 0 {
		t.Fatal("Compare is not byte order")
	}
}

// An earlier generation is named, not called malformed.
func TestAnEarlierGenerationIsNamed(t *testing.T) {
	raw := mustBuild(t, Fields{{1, []byte("a")}})
	copy(raw, "RKH2")
	_, _, err := ParseHead(raw)
	if !errors.Is(err, ErrMagic) || !bytes.Contains([]byte(err.Error()), []byte("earlier generation")) {
		t.Fatalf("RKH2 was not named as an earlier generation: %v", err)
	}
}

// A head carries its own length, so a stored event splits into head and body
// without reading the body.
func TestTheHeadCarriesItsLength(t *testing.T) {
	head := mustBuild(t, Fields{{1, []byte("a")}, {2, []byte("b")}})
	body := []byte("the body follows")
	h, rest, err := ParseHead(append(append([]byte(nil), head...), body...))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(h.Raw, head) || !bytes.Equal(rest, body) || h.ID != Hash(head) {
		t.Fatal("head and body were not split at the head's own length, or the id is not the head's hash")
	}
}
