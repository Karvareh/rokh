package shell

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// The terminal is technical English, whole: no Arabic script and no Persian
// written in Latin letters in anything the sentence surface, the passphrase
// prompt or the size parser says, and none in their comments either, which
// are read by whoever builds them. The one word allowed from outside English
// is Rokh. Test files are not read: the Persian in them is content, and it
// proves that content passes byte for byte.
//
// Typographic glyphs are limited to a written list, so a new one is a
// decision somebody makes on purpose and not something that slips in.
func TestTheSurfaceSpeaksEnglishOnly(t *testing.T) {
	for _, problem := range languageProblems(t, ".", "../passphrase", "../size") {
		t.Error(problem)
	}
}

// allowedGlyphs are the characters outside ASCII the surface may draw or
// name: dashes, the ellipsis and the middle dot of its sentences; the frames,
// marks, arrows and cursor of its screen; curly quotes around a payload; and
// the replacement character that stands for bytes that are not text.
const allowedGlyphs = "—–…·“”" + // punctuation
	"─│╭╮╰╯" + // frames
	"◇▸●›█" + // marks and the cursor
	"←→↑↓" + // arrows
	"\ufffd" // the replacement character

// deniedWords are Persian words written in Latin letters that were found in
// this code once; the list grows when another is found.
var deniedWords = regexp.MustCompile(`(?i)\b(salam|khoob|daftar|khaneh|kelid)\b`)

func languageProblems(t *testing.T, dirs ...string) []string {
	t.Helper()
	var out []string
	check := func(fset *token.FileSet, pos token.Pos, what, text string) {
		p := fset.Position(pos)
		for _, r := range text {
			switch {
			case unicode.Is(unicode.Arabic, r):
				out = append(out, p.String()+": Arabic script in a "+what+": "+strings.TrimSpace(text))
				return
			case r > unicode.MaxASCII && !strings.ContainsRune(allowedGlyphs, r):
				out = append(out, p.String()+": "+string(r)+" is not on the list of glyphs, in a "+what)
				return
			}
		}
		if m := deniedWords.FindString(text); m != "" {
			out = append(out, p.String()+": Persian in Latin letters ("+m+") in a "+what)
		}
	}
	for _, dir := range dirs {
		names, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(names) == 0 {
			t.Fatalf("%s holds no Go file; the guard would watch nothing", dir)
		}
		for _, name := range names {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			src, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			for _, g := range f.Comments {
				for _, c := range g.List {
					check(fset, c.Pos(), "comment", c.Text)
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && (lit.Kind == token.STRING || lit.Kind == token.CHAR) {
					check(fset, lit.Pos(), "string", lit.Value)
				}
				return true
			})
		}
	}
	return out
}

// The guard finds what it is for, so that a pass means something.
func TestTheLanguageGuardFindsWhatItShould(t *testing.T) {
	dir := t.TempDir()
	src := "package x\n\n// the word is daftar\nconst a = \"\u0633\u0644\u0627\u0645\"\n\n// fine — and fine…\nconst b = \"\u00e9\"\n"
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(languageProblems(t, dir), "\n")
	for _, want := range []string{"x.go:3:1: Persian in Latin letters (daftar)", "x.go:4:11: Arabic script in a string", "x.go:7:11: \u00e9 is not on the list"} {
		if !strings.Contains(got, want) {
			t.Errorf("the guard did not say %q; it said:\n%s", want, got)
		}
	}
	if strings.Contains(got, "x.go:6") {
		t.Errorf("the guard refused glyphs on its own list:\n%s", got)
	}
}
