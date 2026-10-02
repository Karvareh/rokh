// Package conformance reads the two specification files and the code that
// claims to realise them, and reports the distance between the two.
//
// It holds no rules of its own. Every id it knows comes from parsing
// رساله.md and without-consensus.md; every judgement it makes comes from
// conformance.md §2. When the specification gains a proposition, this
// package learns about it by reading, not by being edited.
package conformance

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Prefix marks which specification a proposition came from. The two files
// number their propositions independently, so an id is only unique with it.
const (
	Treatise = "T" // رساله.md
	Ledger   = "N" // without-consensus.md
)

// Item is one proposition, named by its own id.
type Item struct {
	ID   string // "T3.3", "N4.7", "N-Axiom5"
	Line int    // where it stands in its file, for the report
	File string
	Text string
}

var (
	// **۳٫۳** or **3.3** at the head of a line: a numbered proposition.
	reNumbered = regexp.MustCompile(`^\*\*([۰-۹0-9]+(?:[٫.][۰-۹0-9]+)?)\*\*\s*(.*)$`)
	// > **آکسیومِ سوم** — an axiom, which carries a name instead; in the
	// English rendering, > **Axiom 3** — with its number.
	reAxiom   = regexp.MustCompile(`^>\s*\*\*آکسیومِ\s+(\S+?)(?:\*\*|\s+—)`)
	reAxiomEN = regexp.MustCompile(`^>\s*\*\*Axiom\s+([1-7])\b`)
)

// ordinals maps the seven axiom names to the citation form conformance.md
// fixes: N-Axiom1 … N-Axiom7. The names are the document's; the numbers are
// the work order's. Neither is ours to invent.
var ordinals = map[string]int{
	"یکم": 1, "دوم": 2, "سوم": 3, "چهارم": 4,
	"پنجم": 5, "ششم": 6, "هفتم": 7,
}

var faDigits = []rune("۰۱۲۳۴۵۶۷۸۹")

// latin turns ۳٫۳ into 3.3 so that ids read the same in Go source, in a
// citation comment, and in a test name.
func latin(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '٫', r == '.':
			b.WriteByte('.')
		default:
			hit := false
			for i, d := range faDigits {
				if r == d {
					fmt.Fprintf(&b, "%d", i)
					hit = true
					break
				}
			}
			if !hit {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// ParseSpec reads one specification file and returns its propositions in the
// order they stand on the page.
func ParseSpec(path, prefix string) ([]Item, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Item
	seen := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())

		if m := reNumbered.FindStringSubmatch(line); m != nil {
			id := prefix + latin(m[1])
			if prev, dup := seen[id]; dup {
				return nil, fmt.Errorf("%s:%d: %s already stands at line %d — "+
					"a number is an identifier and cannot be reused", path, n, id, prev)
			}
			seen[id] = n
			out = append(out, Item{ID: id, Line: n, File: filepath.Base(path), Text: trimText(m[2])})
			continue
		}

		if m := reAxiomEN.FindStringSubmatch(line); m != nil {
			id := prefix + "-Axiom" + m[1]
			if prev, dup := seen[id]; dup {
				return nil, fmt.Errorf("%s:%d: %s already stands at line %d", path, n, id, prev)
			}
			seen[id] = n
			out = append(out, Item{ID: id, Line: n, File: filepath.Base(path), Text: trimText(line)})
			continue
		}

		if m := reAxiom.FindStringSubmatch(line); m != nil {
			k, ok := ordinals[m[1]]
			if !ok {
				return nil, fmt.Errorf("%s:%d: unknown axiom name %q — the seven "+
					"names are fixed; a new one means the document changed shape", path, n, m[1])
			}
			id := fmt.Sprintf("%s-Axiom%d", prefix, k)
			if prev, dup := seen[id]; dup {
				return nil, fmt.Errorf("%s:%d: %s already stands at line %d", path, n, id, prev)
			}
			seen[id] = n
			out = append(out, Item{ID: id, Line: n, File: filepath.Base(path), Text: trimText(line)})
		}
	}
	return out, sc.Err()
}

func trimText(s string) string {
	s = strings.NewReplacer("**", "", "> ", "", "*", "").Replace(s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 90 {
		return string(r[:90]) + "…"
	}
	return s
}

// Citation is one place in the code that claims a proposition.
type Citation struct {
	ID     string
	File   string
	Line   int
	InTest bool // a citation inside a _test.go file is what makes a ruling *exercised*
}

// A citation is an em dash, a space, and an id at the end of a comment:
//
//	// A rejected event is never stored.  — T3.3
//
// Several ids may share one dash, separated by commas — on one line. A
// citation that wraps onto a second comment line is a trap: the eye reads it
// as one list and the scanner sees only the first half, so the ids below the
// fold go silently uncounted. reCiteTail catches that shape and the scan
// refuses it rather than quietly reading less than is written.
var reCite = regexp.MustCompile(`—\s*((?:[TN](?:-Axiom)?[0-9.]+)(?:\s*,\s*[TN](?:-Axiom)?[0-9.]+)*)`)

// reCiteTail matches a comment line that is nothing but ids: the continuation
// of a citation that was allowed to wrap.
var reCiteTail = regexp.MustCompile(`^\s*//\s*[TN](?:-Axiom)?[0-9.]+(\s*,\s*[TN](?:-Axiom)?[0-9.]+)*\s*$`)

// reBareNumber matches a citation dash followed by a number with no source
// prefix.
//
// Thirty-six numbers exist in *both* documents and mean different things in
// each: 6.5 is the bound of a keeping in one and the descriptor rule in the
// other. So a bare number is not merely untidy — it names two rulings at once,
// and reCite, which requires a prefix, would pass over it without a word. That
// is the same failure as a wrapped citation: something written down, silently
// not counted. It is refused for the same reason.
var reBareNumber = regexp.MustCompile(`—\s*(?:[TN](?:-Axiom)?[0-9.]+\s*,\s*)*([0-9]+\.[0-9]+)`)

// ScanCode walks the Go tree and collects every citation it finds in a comment.
func ScanCode(root string) ([]Citation, error) {
	var out []Citation
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			// The map does not measure itself. Its own doc comments quote
			// citation forms as examples, and an instrument that reads its
			// own markings reports coverage it did not find.
			case ".git", "node_modules", "testdata", "conformance":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		inTest := strings.HasSuffix(p, "_test.go")
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
		prevCited := false
		for n := 1; sc.Scan(); n++ {
			line := sc.Text()
			if prevCited && reCiteTail.MatchString(line) {
				return fmt.Errorf("%s:%d: this citation wraps onto a second line, "+
					"and everything below the fold would go uncounted — put the ids "+
					"on one line", rel, n)
			}
			prevCited = false
			// Only comments carry citations. A dash inside a string is prose,
			// not a claim about the specification.
			c := strings.Index(line, "//")
			if c < 0 {
				continue
			}
			if m := reBareNumber.FindStringSubmatch(line[c:]); m != nil {
				return fmt.Errorf("%s:%d: %q is cited with no source prefix, and "+
					"thirty-six numbers mean different things in the two documents "+
					"— write T%s or N%s", rel, n, m[1], m[1], m[1])
			}
			for _, m := range reCite.FindAllStringSubmatch(line[c:], -1) {
				prevCited = true
				for _, id := range strings.Split(m[1], ",") {
					out = append(out, Citation{
						ID: strings.TrimSpace(id), File: rel, Line: n, InTest: inTest,
					})
				}
			}
		}
		return sc.Err()
	})
	return out, err
}
