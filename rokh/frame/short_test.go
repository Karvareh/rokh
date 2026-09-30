package frame

import (
	"encoding/hex"
	"testing"
)

// A short name is a guide for the eye, and a guide that points at two things
// is not a guide. Wherever two abbreviations collide the abbreviation
// lengthens — never the acceptance, which goes by the full name and the bytes
// and never by this.
//
//	— T3.5
func TestAShortNameLengthensWhenTwoWouldCollide(t *testing.T) {
	// Two names sharing their first five bytes, differing at the sixth.
	var a, b ID
	for i := range a {
		a[i], b[i] = byte(i), byte(i)
	}
	b[5] = 0xff

	// Alone, each is the plain four-byte abbreviation.
	if got := a.ShortIn(nil); got != a.Short() {
		t.Fatalf("alone, the abbreviation was %q", got)
	}
	// Together, it lengthens until it separates them — and no further.
	among := []ID{a, b}
	sa, sb := a.ShortIn(among), b.ShortIn(among)
	if sa == sb {
		t.Fatalf("two names abbreviated to the same thing: %q", sa)
	}
	if len(sa) != 12 || len(sb) != 12 {
		t.Fatalf("the abbreviation grew to %d and %d hex digits, expected 12", len(sa), len(sb))
	}
	if sa != hex.EncodeToString(a[:6]) {
		t.Fatalf("it lengthened to something other than the first six bytes: %q", sa)
	}
	// It is still a prefix of the full name, so nothing was invented.
	if a.String()[:len(sa)] != sa {
		t.Fatal("the abbreviation is not a prefix of the name")
	}
}

// Two names that separate only at the last byte lengthen all the way to it,
// which is the honest end of the rule.
//
// And past that end the rule stops helping, which the ruling says plainly. Two
// *different byte strings* under one full name are one name: the ledger sees
// the collision — it compares the byte strings and knows they are not the same
// thing — but no abbreviation can point at one of them rather than the other,
// because they have the same name to abbreviate. What happens then is an open
// ruling, not something this function quietly settles.
//
//	— T3.5, T13.8
func TestItLengthensToTheLastByteAndNoFurther(t *testing.T) {
	var a, b ID
	for i := range a {
		a[i], b[i] = 3, 3
	}
	b[IDSize-1] = 4

	among := []ID{a, b}
	sa := a.ShortIn(among)
	if sa != a.String() {
		t.Fatalf("names differing only at the last byte abbreviated to %q", sa)
	}
	if sa == b.ShortIn(among) {
		t.Fatal("two different names abbreviated alike")
	}

	// The same id twice is one name, and asking for its abbreviation among
	// itself changes nothing. There is no ambiguity to resolve: a name is the
	// hash of bytes, and equal hashes are one name whatever produced them.
	if got := a.ShortIn([]ID{a, a}); got != a.Short() {
		t.Fatalf("one name among copies of itself abbreviated to %q", got)
	}
}

// A column of names is abbreviated to one length, so the column lines up and
// no two entries in it are the same.
//
//	— T3.5
func TestAColumnOfNamesAbbreviatesToOneLengthAndNoTwoAlike(t *testing.T) {
	mk := func(at int, v byte) ID {
		var i ID
		for k := range i {
			i[k] = 7
		}
		i[at] = v
		return i
	}
	// Three names identical for their first nine bytes.
	ids := []ID{mk(9, 1), mk(9, 2), mk(9, 3)}
	got := ShortAll(ids)

	seen := map[string]bool{}
	want := 0
	for _, i := range ids {
		s := got[i]
		if seen[s] {
			t.Fatalf("two names in the column abbreviate to %q", s)
		}
		seen[s] = true
		if want == 0 {
			want = len(s)
		} else if len(s) != want {
			t.Fatalf("the column has entries of %d and %d hex digits", want, len(s))
		}
		if i.String()[:len(s)] != s {
			t.Fatalf("%q is not a prefix of its name", s)
		}
	}
	if want != 20 {
		t.Fatalf("the column settled at %d hex digits, expected 20", want)
	}
	// Names that separate early do not drag the column out.
	plain := []ID{mk(0, 1), mk(0, 2)}
	for _, i := range plain {
		if len(ShortAll(plain)[i]) != 8 {
			t.Fatal("a column that separates at once was lengthened anyway")
		}
	}
}
