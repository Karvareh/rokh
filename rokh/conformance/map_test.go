package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const root = ".."

type world struct {
	items map[string]Item // every proposition in the two documents
	order []string
	obs   []Obligation
	byID  map[string][]Obligation
	weak  map[string]string
	syms  *Symbols
}

func load(t *testing.T) *world {
	t.Helper()
	w := &world{items: map[string]Item{}, byID: map[string][]Obligation{}}
	for _, s := range []struct{ path, prefix string }{
		{filepath.Join(texts, "رساله.md"), Treatise},
		{filepath.Join(texts, "without-consensus.md"), Ledger},
	} {
		got, err := ParseSpec(s.path, s.prefix)
		if err != nil {
			t.Fatalf("reading the specification: %v", err)
		}
		if len(got) == 0 {
			t.Fatalf("%s: no propositions found — a map that reads nothing "+
				"passes everything", s.path)
		}
		for _, it := range got {
			w.items[it.ID] = it
			w.order = append(w.order, it.ID)
		}
	}
	var err error
	if w.obs, err = ReadObligations("obligations.tsv"); err != nil {
		t.Fatalf("reading the obligations: %v", err)
	}
	if w.weak, err = ReadWeak("weak.tsv"); err != nil {
		t.Fatalf("reading the weak tests: %v", err)
	}
	if w.syms, err = Index(root); err != nil {
		t.Fatalf("indexing the tree: %v", err)
	}
	for _, o := range w.obs {
		w.byID[o.ID] = append(w.byID[o.ID], o)
	}
	return w
}

// Every proposition in both documents carries at least one obligation, and no
// obligation invents a proposition. Without this the denominator is whatever
// the author chose to write down, which is how a map comes to flatter itself.
func TestEveryPropositionCarriesAnObligation(t *testing.T) {
	w := load(t)
	var silent []string
	for _, id := range w.order {
		if len(w.byID[id]) == 0 {
			silent = append(silent, id)
		}
	}
	if len(silent) > 0 {
		t.Errorf("%d propositions carry no obligation at all:\n  %s",
			len(silent), strings.Join(silent, " "))
	}
	var invented []string
	for id := range w.byID {
		if _, ok := w.items[id]; !ok {
			invented = append(invented, id)
		}
	}
	sort.Strings(invented)
	if len(invented) > 0 {
		t.Errorf("%d obligations name a proposition that does not exist:\n  %s",
			len(invented), strings.Join(invented, " "))
	}
}

// "Met" is a claim about the tree, so the tree is asked. The production path
// must name something that exists, the test must name a test that exists, and
// that test must say in its own words which ruling it is for — otherwise the
// pairing lives only in this file and the code knows nothing of it.
func TestMetMeansThePathAndTheTestBothExist(t *testing.T) {
	w := load(t)
	var bad []string
	for _, o := range w.obs {
		if o.Status != Met {
			continue
		}
		if o.Where == "" || o.Where == "-" {
			bad = append(bad, fmt.Sprintf("%s is met with no production path", o.Name()))
		} else if ok, why := w.syms.HasDecl(o.Where); !ok {
			bad = append(bad, fmt.Sprintf("%s points at %s — %s", o.Name(), o.Where, why))
		}
		if o.Test == "" || o.Test == "-" {
			bad = append(bad, fmt.Sprintf("%s is met with no test", o.Name()))
			continue
		}
		if ok, why := w.syms.HasTest(o.Test); !ok {
			bad = append(bad, fmt.Sprintf("%s names test %s — %s", o.Name(), o.Test, why))
			continue
		}
		doc := w.syms.TestDoc[o.Test]
		if !Cites(doc, o.ID) {
			bad = append(bad, fmt.Sprintf("%s names test %s, and that test does not "+
				"say it is for %s", o.Name(), o.Test, o.ID))
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%d obligations claim more than the tree holds:\n  %s",
			len(bad), strings.Join(bad, "\n  "))
	}
}

// A test that searches the source for a word proves the word is absent, not
// that the behaviour is. Those tests are kept as an alarm and refused as
// proof: nothing may be met on one of them alone.
func TestNothingIsMetOnAWeakTestAlone(t *testing.T) {
	w := load(t)
	var leaning []string
	for _, o := range w.obs {
		if o.Status != Met {
			continue
		}
		if why, isWeak := w.weak[o.Test]; isWeak {
			leaning = append(leaning, fmt.Sprintf("%s rests on %s — %s",
				o.Name(), o.Test, why))
		}
	}
	sort.Strings(leaning)
	if len(leaning) > 0 {
		t.Errorf("%d obligations rest on a test that is not evidence:\n  %s",
			len(leaning), strings.Join(leaning, "\n  "))
	}
}

// The gate. Every closed ruling and every line of the base profile is either
// met, or settled another way and said so. "Owed" is the honest place for
// stated-and-not-built, and it is the one status that does not close.
func TestNothingBindingIsLeftOwed(t *testing.T) {
	w := load(t)
	var owed []string
	for _, id := range w.order {
		for _, o := range w.byID[id] {
			if o.Kind.Bearing() && o.Status == Owed {
				owed = append(owed, fmt.Sprintf("%-16s %s", o.Name(), o.Note))
			}
		}
	}
	if len(owed) > 0 {
		t.Errorf("%d binding obligations are owed:\n  %s",
			len(owed), strings.Join(owed, "\n  "))
	}
}

// TestReport writes conformance/STATE.md. It never fails: its job is to say
// where the work stands, not to judge.
func TestReport(t *testing.T) {
	w := load(t)
	lines, _ := readLines(filepath.Join(texts, "رساله.md"))
	_ = lines

	count := map[Status]int{}
	bearing := 0
	byBand := map[string][]Obligation{}
	var bandOrder []string
	for _, id := range w.order {
		for _, o := range w.byID[id] {
			count[o.Status]++
			if o.Kind.Bearing() {
				bearing++
			}
			b := bandOf(id)
			if _, ok := byBand[b]; !ok {
				bandOrder = append(bandOrder, b)
			}
			byBand[b] = append(byBand[b], o)
		}
	}

	var b strings.Builder
	b.WriteString("# State of conformance\n\n")
	b.WriteString("Nobody writes this file by hand. `go test ./conformance` makes it.\n\n")
	b.WriteString("Every row is an **obligation**, not a number: a compound proposition has several.\n")
	b.WriteString("*Met* means both a named production path that exists and a test whose own\n")
	b.WriteString("comment says which ruling it is for. A test that only searches the source text\n")
	b.WriteString("is no evidence and is listed in `weak.tsv`.\n\n")

	fmt.Fprintf(&b, "| status | count |\n|---|---|\n")
	for _, s := range []struct {
		st   Status
		name string
	}{
		{Met, "**met**"}, {Owed, "**owed**"}, {RulingOpen, "ruling open"},
		{Outside, "outside the design"}, {Field, "field"}, {Note, "note"},
	} {
		fmt.Fprintf(&b, "| %s | %d |\n", s.name, count[s.st])
	}
	fmt.Fprintf(&b, "\n**Evidence-bearing obligations: %d — met: %d — owed: %d**\n\n",
		bearing, count[Met], count[Owed])

	b.WriteString("## Band by band\n\n")
	for _, band := range bandOrder {
		var owed []string
		for _, o := range byBand[band] {
			if o.Kind.Bearing() && o.Status == Owed {
				owed = append(owed, o.Name())
			}
		}
		if len(owed) == 0 {
			fmt.Fprintf(&b, "- **%s** — complete.\n", band)
			continue
		}
		fmt.Fprintf(&b, "- **%s** — owed %d: %s\n", band, len(owed), strings.Join(owed, " "))
	}

	// The two halves of the report are one fact seen twice. If they ever
	// disagree, one of them is lying.
	fromBands := 0
	for _, band := range bandOrder {
		for _, o := range byBand[band] {
			if o.Kind.Bearing() && o.Status == Owed {
				fromBands++
			}
		}
	}
	if fromBands != count[Owed] {
		t.Fatalf("the report contradicts itself: the summary owes %d, the bands owe %d",
			count[Owed], fromBands)
	}
	if err := os.WriteFile("STATE.md", []byte(b.String()), 0o644); err != nil {
		t.Fatalf("writing the report: %v", err)
	}
	t.Logf("\n%s", b.String())
}

func bandOf(id string) string {
	if i := strings.Index(id, "-"); i > 0 {
		return id[:i] + "-Axiom"
	}
	n := strings.TrimLeft(id, "TN")
	if i := strings.Index(n, "."); i > 0 {
		n = n[:i]
	}
	if _, err := strconv.Atoi(n); err != nil {
		return id
	}
	return id[:1] + n
}
