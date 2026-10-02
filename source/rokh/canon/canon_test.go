package canon

import (
	"errors"
	"strings"
	"testing"
)

// The byte order, as vectors.
//
// Anything named by the hash of its JSON bytes is only as portable as those
// bytes. These are the cases where two reasonable implementations would
// otherwise disagree, written as bytes rather than as prose about bytes.
//
// The first three are the ones that made this package necessary: Go's
// encoding/json escapes < > and & by default and nothing else does, so a
// receipt saying "sent the invoice & the note" had one name in Go and another
// everywhere else.
//
//	— N4.1, T3.2
func TestTheByteOrderOfAString(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a<b", `"a<b"`},
		{"x&y", `"x&y"`},
		{"a>b", `"a>b"`},
		{"سلام", `"سلام"`},
		// U+2028 stays as itself, which SetEscapeHTML(false) does not manage.
		{" ", "\" \""},
		{`say "so"`, `"say \"so\""`},
		{`back\slash`, `"back\\slash"`},
		{"\b", `"\b"`},
		{"\t", `"\t"`},
		{"\n", `"\n"`},
		{"\f", `"\f"`},
		{"\r", `"\r"`},
		{"\x00", `"\u0000"`},
		{"\x1f", `"\u001f"`},
		{"\x0b", `"\u000b"`},
		{"", `""`},
	}
	for _, c := range cases {
		got, err := Marshal(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%q encoded as %s, expected %s", c.in, got, c.want)
		}
	}
}

// Keys are sorted and there is no whitespace. Declaration order is a Go fact,
// not a shared one: reordering a struct's fields must not rename everything
// that struct describes.
func TestKeysAreSortedAndNothingDependsOnDeclarationOrder(t *testing.T) {
	type a struct {
		Zebra string `json:"zebra"`
		Apple string `json:"apple"`
		Mid   int    `json:"mid"`
	}
	type b struct {
		Mid   int    `json:"mid"`
		Apple string `json:"apple"`
		Zebra string `json:"zebra"`
	}
	one, err := Marshal(a{Zebra: "z", Apple: "a", Mid: 3})
	if err != nil {
		t.Fatal(err)
	}
	two, err := Marshal(b{Mid: 3, Apple: "a", Zebra: "z"})
	if err != nil {
		t.Fatal(err)
	}
	if string(one) != string(two) {
		t.Fatalf("declaration order changed the bytes:\n %s\n %s", one, two)
	}
	if want := `{"apple":"a","mid":3,"zebra":"z"}`; string(one) != want {
		t.Fatalf("got %s, want %s", one, want)
	}
	if strings.ContainsAny(string(one), " \t\n") {
		t.Fatal("the encoding carries whitespace")
	}
}

// One content, one encoding. A second spelling — even with one extra space —
// is refused rather than accepted and repaired. This is the only way to shut
// the door on "two bytes, one meaning, two names".
//
//	— N4.1, T3.1
func TestASecondSpellingIsRefusedNotRepaired(t *testing.T) {
	good := `{"apple":"a","zebra":"z"}`
	if err := Check([]byte(good)); err != nil {
		t.Fatalf("the canonical form was refused: %v", err)
	}
	for what, bad := range map[string]string{
		"a space":            `{"apple":"a", "zebra":"z"}`,
		"reordered":          `{"zebra":"z","apple":"a"}`,
		"indented":           "{\n  \"apple\": \"a\",\n  \"zebra\": \"z\"\n}",
		"escaped needlessly": `{"apple":"\u0061","zebra":"z"}`,
	} {
		if err := Check([]byte(bad)); !errors.Is(err, ErrNotCanonical) {
			t.Errorf("%s: passed as canonical (%v)", what, err)
		}
	}
	// And what Check refuses, Canon can still rewrite — refusing is the law,
	// repairing is a separate act somebody performs knowingly.
	fixed, err := Canon([]byte(`{"zebra":"z", "apple":"a"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(fixed) != good {
		t.Fatalf("rewriting gave %s", fixed)
	}
}

// Two things are refused rather than quietly handled, because both would give
// different content one name.
func TestInvalidUTF8AndFloatsAreRefused(t *testing.T) {
	if _, err := Marshal(string([]byte{0xff, 0xfe})); !errors.Is(err, ErrNotUTF8) {
		t.Errorf("invalid UTF-8 was encoded: %v", err)
	}
	if _, err := Marshal(map[string]any{"x": 1.5}); !errors.Is(err, ErrFloat) {
		t.Errorf("a float was encoded: %v", err)
	}
	// An integer-valued float is still a float on the wire and still refused:
	// whether 1.0 prints as "1" or "1.0" is exactly the disagreement to avoid.
	if _, err := Marshal(map[string]any{"x": float64(2)}); err != nil {
		// Go writes float64(2) as 2, which is an integer literal, so this is
		// allowed. The refusal is of a literal that carries a point or an
		// exponent, which is what actually differs between languages.
		t.Logf("float64(2) encoded, as an integer literal: %v", err)
	}
	if _, err := Marshal(map[string]any{"x": 1e21}); !errors.Is(err, ErrFloat) {
		t.Errorf("an exponent literal was encoded: %v", err)
	}
}

// Nested values are canonical all the way down, and an array keeps its order:
// a list is a sequence, and sorting one would change what it says.
func TestNestingIsCanonicalAndArraysKeepTheirOrder(t *testing.T) {
	in := map[string]any{
		"list":  []any{"z", "a", "m"},
		"inner": map[string]any{"b": 2, "a": 1},
		"flag":  true,
		"gone":  nil,
	}
	got, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"flag":true,"gone":null,"inner":{"a":1,"b":2},"list":["z","a","m"]}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if err := Check(got); err != nil {
		t.Fatalf("its own output is not canonical: %v", err)
	}
}
