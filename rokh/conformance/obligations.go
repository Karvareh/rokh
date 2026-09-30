package conformance

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Status is what is actually true of one obligation right now.
//
// The old map had two states — cited or excused — and an id was "closed" as
// soon as its number appeared in any test file. That let a citation stand in
// for a check, and it let one number hide several obligations at once. These
// five say what is so.
type Status string

const (
	// Met: there is a production path that realises it and a named test that
	// exercises that path. Both are checked to exist.
	Met Status = "met"
	// Owed: the ruling is closed and the work is not done. This is the
	// honest resting place for "stated, not built".
	Owed Status = "owed"
	// RulingOpen: T13 marks the ruling itself unsettled.
	RulingOpen Status = "ruling-open"
	// Outside: deliberately excluded from the design.
	Outside Status = "outside"
	// Field: not a code question — it is answered by use, not by a test.
	Field Status = "field"
	// Note: a status line or a field guess. It owes nothing.
	Note Status = "note"
)

var statuses = map[Status]bool{
	Met: true, Owed: true, RulingOpen: true, Outside: true, Field: true, Note: true,
}

// Closed reports whether an obligation is settled one way or another. Owed is
// the only status that is not.
func (s Status) Closed() bool { return s != Owed }

// Bearing reports whether an obligation owes evidence at all.
func (s Status) Bearing() bool { return s != Note }

// Obligation is one thing the specification requires, named by the
// proposition it comes from and by which part of it this is.
//
// A single proposition may carry several: T13.1 names both a sealing that is
// closed-and-unbuilt and a byte form that is an open ruling, and calling the
// whole number one thing hides one of them.
type Obligation struct {
	ID     string
	Part   string // "-" when the proposition carries only one
	Kind   Kind
	Status Status
	// Where is the production path, as path.go:Symbol. Required for Met.
	Where string
	// Test is the test that exercises it, as pkg:TestName. Required for Met.
	Test string
	Note string
	Line int
}

// Name is how an obligation is referred to in a report.
func (o Obligation) Name() string {
	if o.Part == "" || o.Part == "-" {
		return o.ID
	}
	return o.ID + "/" + o.Part
}

// ReadObligations loads the ledger of obligations.
func ReadObligations(path string) ([]Obligation, error) {
	rows, err := readTSV(path)
	if err != nil {
		return nil, err
	}
	var out []Obligation
	seen := map[string]bool{}
	for _, r := range rows {
		if len(r) < 5 {
			return nil, fmt.Errorf("%s:%s: want id, part, kind, status, where, test[, note]",
				path, r[0])
		}
		o := Obligation{ID: r[1], Part: r[2], Kind: Kind(r[3]), Status: Status(r[4])}
		if len(r) > 5 {
			o.Where = r[5]
		}
		if len(r) > 6 {
			o.Test = r[6]
		}
		if len(r) > 7 {
			o.Note = r[7]
		}
		fmt.Sscanf(r[0], "%d", &o.Line)
		if o.Kind != Ruling && o.Kind != Profile && o.Kind != KindNote {
			return nil, fmt.Errorf("%s:%s: %s has kind %q", path, r[0], o.Name(), o.Kind)
		}
		if !statuses[o.Status] {
			return nil, fmt.Errorf("%s:%s: %s has status %q; one of met, owed, "+
				"ruling-open, outside, field, note", path, r[0], o.Name(), o.Status)
		}
		if (o.Kind == KindNote) != (o.Status == Note) {
			return nil, fmt.Errorf("%s:%s: %s is kind %q with status %q; a note is a "+
				"note in both columns or in neither", path, r[0], o.Name(), o.Kind, o.Status)
		}
		if seen[o.Name()] {
			return nil, fmt.Errorf("%s:%s: %s listed twice", path, r[0], o.Name())
		}
		seen[o.Name()] = true
		out = append(out, o)
	}
	return out, nil
}

// ReadWeak loads the tests that are not evidence.
//
// A test that greps the source for a word proves that the word is absent, not
// that the behaviour is. Such tests are useful as an alarm and are refused as
// proof: an obligation may not be Met on one of these alone.
func ReadWeak(path string) (map[string]string, error) {
	rows, err := readTSV(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, r := range rows {
		if len(r) < 2 {
			return nil, fmt.Errorf("%s:%s: want test[, why]", path, r[0])
		}
		why := ""
		if len(r) > 2 {
			why = r[2]
		}
		out[r[1]] = why
	}
	return out, nil
}

// Symbols is what the tree actually contains, so a claim about a production
// path or a test can be checked rather than believed.
type Symbols struct {
	// Decls maps "path.go" to the top-level names declared in it.
	Decls map[string]map[string]bool
	// Tests maps "pkg" to the test function names in it.
	Tests map[string]map[string]bool
	// TestDoc maps "pkg:TestName" to the comment above it, so the map can
	// require that a test says which ruling it is for.
	TestDoc map[string]string
}

// Index walks the tree and records what is declared where.
func Index(root string) (*Symbols, error) {
	s := &Symbols{
		Decls:   map[string]map[string]bool{},
		Tests:   map[string]map[string]bool{},
		TestDoc: map[string]string{},
	}
	fset := token.NewFileSet()
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "testdata", "conformance":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		pkg := filepath.ToSlash(filepath.Dir(rel))
		if s.Decls[rel] == nil {
			s.Decls[rel] = map[string]bool{}
		}
		if s.Tests[pkg] == nil {
			s.Tests[pkg] = map[string]bool{}
		}
		isTest := strings.HasSuffix(rel, "_test.go")
		for _, d := range f.Decls {
			switch n := d.(type) {
			case *ast.FuncDecl:
				name := n.Name.Name
				if n.Recv != nil && len(n.Recv.List) > 0 {
					name = recvName(n.Recv.List[0].Type) + "." + name
				}
				s.Decls[rel][name] = true
				if isTest && strings.HasPrefix(n.Name.Name, "Test") {
					s.Tests[pkg][n.Name.Name] = true
					if n.Doc != nil {
						s.TestDoc[pkg+":"+n.Name.Name] = n.Doc.Text()
					}
				}
			case *ast.GenDecl:
				for _, sp := range n.Specs {
					switch v := sp.(type) {
					case *ast.TypeSpec:
						s.Decls[rel][v.Name.Name] = true
					case *ast.ValueSpec:
						for _, nm := range v.Names {
							s.Decls[rel][nm.Name] = true
						}
					}
				}
			}
		}
		return nil
	})
	return s, err
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvName(t.X)
	}
	return "?"
}

// HasDecl reports whether "path.go:Symbol" names something that exists.
func (s *Symbols) HasDecl(where string) (bool, string) {
	file, sym, ok := strings.Cut(where, ":")
	if !ok {
		return false, "want path.go:Symbol"
	}
	decls, found := s.Decls[file]
	if !found {
		return false, "no such file"
	}
	if sym == "*" {
		return true, ""
	}
	if !decls[sym] {
		return false, "the file declares no " + sym
	}
	return true, ""
}

// HasTest reports whether "pkg:TestName" names a test that exists.
func (s *Symbols) HasTest(ref string) (bool, string) {
	pkg, name, ok := strings.Cut(ref, ":")
	if !ok {
		return false, "want pkg:TestName"
	}
	tests, found := s.Tests[pkg]
	if !found {
		return false, "no such package"
	}
	if !tests[name] {
		return false, "the package has no " + name
	}
	return true, ""
}

// Cites reports whether a doc comment cites this exact ruling.
//
// It is not a substring search, and the difference is not pedantic: "T1" sits
// inside "T11.8", "T2" inside "T2.3", "N4" inside "N4.10". A whole band's
// obligation could be marked met against a test that never mentions the band —
// only one of its propositions — and the map would agree, because the letters
// were there. So the citations are read as citations and compared whole.
//
//	— T3.2
func Cites(doc, id string) bool {
	for _, m := range reCite.FindAllStringSubmatch(doc, -1) {
		for _, got := range strings.Split(m[1], ",") {
			if strings.TrimSpace(got) == id {
				return true
			}
		}
	}
	return false
}

// readLines is used by the report to quote a proposition.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out, sc.Err()
}
