package content

import (
	"bytes"
	"strings"
	"testing"

	"rokh/frame"
)

func TestRoundTrip(t *testing.T) {
	body := []byte("# Plan\n\nBuild the smallest thing that works.\n")
	d, err := New(body, "text/markdown")
	if err != nil {
		t.Fatal(err)
	}
	if d.Hash != frame.Hash(body) || d.Size != uint64(len(body)) {
		t.Fatal("descriptor does not describe the bytes it was built from")
	}
	b, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back != d {
		t.Fatalf("round trip changed the descriptor: %+v vs %+v", d, back)
	}
	// One descriptor, one encoding.
	again, _ := d.Encode()
	if !bytes.Equal(b, again) {
		t.Fatal("encoding is not deterministic")
	}
}

// A descriptor must stay far inside the inline payload limit, or it could not
// be what an event carries instead of the content.
// Heavy content travels as a descriptor — hash, size, type. Rokh records the
// handing over and the receipt; it does not hold the item itself.
//
//	— N4.5, N2.11, N6.5
func TestDescriptorIsTiny(t *testing.T) {
	d, err := New(make([]byte, 50<<20), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 128 {
		t.Fatalf("descriptor is %d bytes; it describes 50 MB and must stay tiny", len(b))
	}
	t.Logf("a 50 MB blob is described by %d bytes", len(b))
}

func TestVerify(t *testing.T) {
	body := []byte("exact bytes")
	d, _ := New(body, "text/plain")
	if err := d.Verify(body); err != nil {
		t.Fatalf("the original bytes did not verify: %v", err)
	}
	if err := d.Verify([]byte("exact byteS")); err == nil {
		t.Fatal("altered bytes verified")
	}
	if err := d.Verify(append(body, ' ')); err == nil {
		t.Fatal("bytes of a different length verified")
	}
}

// Persian prose, ZWNJ included, must survive byte for byte. A payload is prose,
// not an identifier: nothing here normalizes or repairs it.
func TestPersianContentIsPreservedByteForByte(t *testing.T) {
	persian := "طرحِ کسب‌وکار: می‌خواهیم نیم‌فاصله‌ها دست‌نخورده بمانند.\n"
	body := []byte(persian)
	if !strings.Contains(persian, "‌") {
		t.Fatal("test text was expected to contain ZWNJ")
	}
	d, err := New(body, "text/markdown")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := d.Encode()
	back, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := back.Verify(body); err != nil {
		t.Fatalf("Persian content did not verify against its descriptor: %v", err)
	}
	if back.Size != uint64(len(body)) {
		t.Fatalf("size %d, want %d - byte length, not rune count", back.Size, len(body))
	}
}

func TestRejectsMalformed(t *testing.T) {
	if _, err := New([]byte("x"), ""); err == nil {
		t.Error("empty media type accepted")
	}
	if _, err := New([]byte("x"), strings.Repeat("a", MaxType+1)); err == nil {
		t.Error("over-long media type accepted")
	}
	if _, err := New([]byte("x"), "text/پارسی"); err == nil {
		t.Error("non-ASCII media type accepted")
	}
	d, _ := New([]byte("x"), "text/plain")
	good, _ := d.Encode()

	if _, err := Decode(nil); err == nil {
		t.Error("empty descriptor accepted")
	}
	if _, err := Decode(good[:len(good)-1]); err == nil {
		t.Error("truncated descriptor accepted")
	}
	// A descriptor missing a required field.
	partial, _ := frame.EncodeFields(frame.Fields{{Tag: 0x0001, Value: make([]byte, 32)}})
	if _, err := Decode(partial); err == nil {
		t.Error("descriptor without size or type accepted")
	}
	// An unknown field.
	extra, _ := frame.EncodeFields(frame.Fields{
		{Tag: 0x0001, Value: make([]byte, 32)},
		{Tag: 0x0002, Value: make([]byte, 8)},
		{Tag: 0x0003, Value: []byte("text/plain")},
		{Tag: 0x0009, Value: []byte("surprise")},
	})
	if _, err := Decode(extra); err == nil {
		t.Error("descriptor with an unknown field accepted")
	}
}

func TestExtension(t *testing.T) {
	for typ, want := range map[string]string{
		"text/plain":               ".txt",
		"text/markdown":            ".md",
		"application/json":         ".json",
		"image/png":                ".png",
		"application/vnd.foo+json": ".json",
		"nonsense":                 ".bin",
	} {
		d, err := New([]byte("x"), typ)
		if err != nil {
			t.Fatal(err)
		}
		if got := d.Extension(); got != want {
			t.Errorf("%s -> %s, want %s", typ, got, want)
		}
	}
}
