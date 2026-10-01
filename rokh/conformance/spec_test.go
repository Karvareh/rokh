package conformance

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// The two documents number independently: thirty-six numbers exist in both and
// mean different things in each — 6.5 is the bound of a keeping in one and the
// descriptor rule in the other. So a citation without a source prefix names two
// rulings at once, and the harvester, which requires a prefix, would pass over
// it without a word.
//
// That is the same failure as a citation that wraps onto a second line:
// something written down and silently not counted. This is the guard, and it
// is tested rather than trusted, because a guard nobody has watched fire is a
// comment.
func TestACitationWithNoSourcePrefixIsRefused(t *testing.T) {
	for _, bad := range []string{
		"//\t— 6.5",
		"// — 11.8, T3.2",
		"//\t— T3.2, 11.8",
		"// something something — 4.4",
	} {
		if !reBareNumber.MatchString(bad) {
			t.Errorf("a bare number passed: %q", bad)
		}
	}
	for _, good := range []string{
		"//\t— T6.5",
		"//\t— N6.5",
		"//\t— T11.8, N4.10",
		"//\t— N-Axiom2",
		"// four kilobytes in the base profile",
		"// see section 4.4 of the work order",
		"//\t— T3.5, T13.8",
	} {
		if reBareNumber.MatchString(good) {
			t.Errorf("a proper citation was refused: %q", good)
		}
	}
	// And the number it names is the offending one, so the message can say
	// what to write instead.
	m := reBareNumber.FindStringSubmatch("//\t— T3.2, 11.8")
	if m == nil || m[1] != "11.8" {
		t.Fatalf("the guard named %v", m)
	}
}

// A citation is compared whole, not as a run of letters.
//
// "T1" sits inside "T11.8" and "T2" inside "T2.3", so a substring search would
// let a whole band's obligation be met by a test that cites only one of its
// propositions. The map is the thing that says the work is done; a soft match
// here makes the number softer than it reads.
func TestABandIsNotCitedByOneOfItsPropositions(t *testing.T) {
	doc := "// Some behaviour.\n//\n//\t— T11.8, N4.10\n"
	for _, notCited := range []string{"T1", "T11", "N4", "T8", "T11.10"} {
		if Cites(doc, notCited) {
			t.Errorf("%q was read as cited by %q", notCited, "T11.8, N4.10")
		}
	}
	for _, cited := range []string{"T11.8", "N4.10"} {
		if !Cites(doc, cited) {
			t.Errorf("%q is cited and was not found", cited)
		}
	}
	// The band itself, cited as itself, is found.
	if !Cites("//\t— T11, T11.8\n", "T11") {
		t.Error("a band cited in its own right was not found")
	}
	// And a number appearing in prose is not a citation.
	if Cites("// four kilobytes, see T4.5 in passing\n", "T4.5") {
		t.Error("a number in prose was read as a citation")
	}
}

// The two guards in the harvester are wired to something.
//
// They were not. ScanCode carried both — the refusal of a citation that wraps
// onto a second line, and the refusal of a number with no source prefix — and
// nothing called ScanCode, so both were comments that looked like enforcement.
// A planted bare citation passed the whole suite. This runs it.
func TestTheHarvesterRunsOverTheTreeAndItsGuardsHold(t *testing.T) {
	cites, err := ScanCode("..")
	if err != nil {
		t.Fatalf("the tree does not satisfy the harvester's own rules: %v", err)
	}
	if len(cites) == 0 {
		t.Fatal("the harvester found no citations at all, which cannot be right")
	}
	// Every id it found is one the specification actually has, so a typo in a
	// citation is a failure rather than a line nobody reads.
	known := map[string]bool{}
	for _, s := range []struct{ path, prefix string }{
		{"../texts/رساله.md", "T"}, {"../texts/without-consensus.md", "N"},
	} {
		props, err := ParseSpec(s.path, s.prefix)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range props {
			known[p.ID] = true
		}
	}
	var unknown []string
	for _, c := range cites {
		if !known[c.ID] {
			unknown = append(unknown, fmt.Sprintf("%s:%d cites %s, which is not in "+
				"either document", c.File, c.Line, c.ID))
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		t.Errorf("%d citations name nothing:\n  %s", len(unknown),
			strings.Join(unknown, "\n  "))
	}
}
