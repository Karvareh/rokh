package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// testCols is a collection of the test's own, so that these tests hold in
// every repository whatever its rules.go says.
func testCols() []Collection {
	return []Collection{{
		Name:     "notes",
		Dir:      "notes",
		Kind:     Files,
		ID:       regexp.MustCompile(`^N-[0-9]{2,}$`),
		Prefixes: []string{"N"},
		Fields: []Field{
			{Key: "id"},
			{Key: "title"},
			{Key: "status", Allowed: []string{"open", "done"}},
			{Key: "date", Pattern: regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)},
			{Key: "needs", List: true, Item: regexp.MustCompile(`^N-[0-9]{2,}$`)},
		},
		Rules: func(r *Record, all map[string]*Record) []string {
			var out []string
			for _, n := range r.List("needs") {
				if _, ok := all[n]; !ok {
					out = append(out, "it needs "+n+", which is not here")
				}
			}
			return out
		},
		Index: "README.md",
		Columns: []Column{
			{"Note", func(r *Record) string { return "[" + r.ID() + "](" + r.Link("README.md") + ")" }},
			{"Title", func(r *Record) string { return r.Get("title") }},
		},
		Empty: "No note.",
		Issue: &IssueRule{
			Managed: []string{"open", "p1"},
			Labels:  func(r *Record) []string { return []string{"open"} },
			Open:    func(r *Record) bool { return r.Get("status") == "open" },
		},
	}}
}

func write(t *testing.T, root, rel, s string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

const readme = "# Notes\n\n<!-- records:notes -->\n<!-- /records:notes -->\n"

func note(id, title, status, needs string) string {
	return "---\nid: " + id + "\ntitle: \"" + title + "\"\nstatus: " + status +
		"\ndate: 2026-10-01\nneeds: " + needs + "\n---\n\n# " + id + "\n"
}

func TestFrontMatterReadsPlainAndQuotedValuesAndRefusesTheRest(t *testing.T) {
	f, err := Front([]byte("---\nid: N-01\ntitle: \"a: b \\\"c\\\"\"\nneeds: N-02, N-03\n---\nbody\n"))
	if err != nil {
		t.Fatal(err)
	}
	if f["id"] != "N-01" || f["title"] != `a: b "c"` || f["needs"] != "N-02, N-03" {
		t.Fatalf("read %q", f)
	}
	for name, s := range map[string]string{
		"no front matter":      "# N-01\n",
		"not closed":           "---\nid: N-01\n",
		"not key: value":       "---\nid N-01\n---\n",
		"a colon unquoted":     "---\ntitle: a: b\n---\n",
		"a hash unquoted":      "---\ntitle: a #b\n---\n",
		"a sign first":         "---\nneeds: -\n---\n",
		"a key twice":          "---\nid: N-01\nid: N-02\n---\n",
		"a quote left open":    "---\ntitle: \"a\n---\n",
		"a key with no name":   "---\n: a\n---\n",
		"a value split in two": "---\ntitle: \"a\" \"b\"\n---\n",
	} {
		if _, err := Front([]byte(s)); err == nil {
			t.Errorf("%s: read without a fault", name)
		}
	}
}

func TestCheckFindsEveryKindOfFault(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", readme+"\n[gone](notes/N-09-gone.md) [here](notes/N-01-one.md) `[code](nowhere.md)`\n")
	write(t, root, "notes/N-01-one.md", note("N-01", "One", "open", "none"))
	write(t, root, "notes/N-02-two.md", note("N-02", "Two", "maybe", "N-77"))
	write(t, root, "notes/N-03-three.md", note("N-01", "Three", "done", "none"))
	write(t, root, "notes/N-04-four.md", "---\nid: N-04\ntitle: Four\nstatus: done\n---\n")
	write(t, root, "notes/N-05-five.md", note("N-05", "Five", "done", "none")+"[out](../../x.md)\n")
	write(t, root, "notes/N-06-six.md", strings.Replace(note("N-06", "Six", "done", "none"), "date: 2026-10-01", "date: soon", 1))
	write(t, root, "notes/stray.txt", "")
	write(t, root, "notes/N-07-Seven.md", note("N-07", "Seven", "done", "none")+"```\n[fenced](nowhere.md)\n```\n")
	write(t, root, "notes/N-08-eight.md", strings.Replace(note("N-08", "Eight", "done", "none"), "---\n\n#", "colour: red\n---\n\n#", 1))
	faults := strings.Join(Check(root, testCols()), "\n")
	for _, want := range []string{
		`notes/N-02-two.md: status is "maybe", which is not one of open, done`,
		"notes/N-02-two.md: it needs N-77, which is not here",
		"notes/N-03-three.md: its name does not begin with its identifier N-01",
		"notes/N-03-three.md: N-01 is already the identifier of notes/N-01-one.md",
		"notes/N-04-four.md: date is missing",
		"notes/N-04-four.md: needs is missing",
		"notes/N-05-five.md:10: ../../x.md leads out of the repository",
		`notes/N-06-six.md: date is "soon", which is not of its form`,
		"notes/stray.txt: not a record",
		"notes/N-07-Seven.md: its name is not ID-words",
		"notes/N-08-eight.md: colour is not a field of notes",
		"README.md: the table of notes is not the one its records make",
		"README.md:6: notes/N-09-gone.md leads nowhere",
	} {
		if !strings.Contains(faults, want) {
			t.Errorf("no fault says %q; the faults are:\n%s", want, faults)
		}
	}
	for _, not := range []string{"nowhere.md", "notes/N-01-one.md:", "README.md:6: notes/N-01-one.md"} {
		if strings.Contains(faults, not) {
			t.Errorf("a fault names %q, which holds:\n%s", not, faults)
		}
	}
}

func TestIndexWritesTheTableCheckWants(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", readme+"after\n")
	write(t, root, "notes/N-10-ten.md", note("N-10", "Ten | a pipe", "open", "none"))
	write(t, root, "notes/N-02-two.md", note("N-02", "Two", "done", "N-10"))
	if f := Check(root, testCols()); len(f) != 1 || !strings.Contains(f[0], "table of notes") {
		t.Fatalf("before the index is written, the faults are %q", f)
	}
	if err := WriteIndexes(root, testCols()); err != nil {
		t.Fatal(err)
	}
	if f := Check(root, testCols()); len(f) != 0 {
		t.Fatalf("after the index is written, the faults are %q", f)
	}
	b, _ := os.ReadFile(filepath.Join(root, "README.md"))
	want := "| Note | Title |\n|---|---|\n| [N-02](notes/N-02-two.md) | Two |\n| [N-10](notes/N-10-ten.md) | Ten \\| a pipe |\n<!-- /records:notes -->\nafter\n"
	if !strings.HasSuffix(string(b), want) {
		t.Fatalf("the index reads:\n%s", b)
	}
	// Empty, the index says so.
	os.RemoveAll(filepath.Join(root, "notes"))
	os.Mkdir(filepath.Join(root, "notes"), 0o755)
	if err := WriteIndexes(root, testCols()); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(root, "README.md"))
	if !strings.Contains(string(b), "<!-- records:notes -->\nNo note.\n<!-- /records:notes -->") {
		t.Fatalf("the empty index reads:\n%s", b)
	}
}

func TestIssuesFollowTheRecords(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", readme)
	write(t, root, "notes/N-01-one.md", note("N-01", "One", "open", "none"))     // no issue: open one
	write(t, root, "notes/N-02-two.md", note("N-02", "Two", "open", "none"))     // issue closed, labels stale: reopen, relabel, retitle
	write(t, root, "notes/N-03-three.md", note("N-03", "Three", "done", "none")) // issue open: close it
	write(t, root, "notes/N-04-four.md", note("N-04", "Four", "done", "none"))   // no issue, done: nothing
	write(t, root, "notes/N-05-five.md", note("N-05", "Five", "open", "none"))   // in step: nothing
	list := `[
	 {"number": 7, "title": "N-02: Old", "state": "CLOSED", "labels": [{"name": "p1"}, {"name": "kept"}]},
	 {"number": 8, "title": "N-03: Three", "state": "OPEN", "labels": [{"name": "open"}]},
	 {"number": 9, "title": "N-05: Five", "state": "OPEN", "labels": [{"name": "open"}]},
	 {"number": 3, "title": "Not a record", "state": "OPEN", "labels": []}
	]`
	write(t, root, "issues.json", list)
	var out bytes.Buffer
	if err := Issues(root, testCols(), true, filepath.Join(root, "issues.json"), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		`gh "issue" "create" "--title" "N-01: One" "--body" "This issue stands for the record`,
		`"--label" "open"`,
		`gh "issue" "edit" "7" "--title" "N-02: Two" "--add-label" "open" "--remove-label" "p1"`,
		`gh "issue" "reopen" "7"`,
		`gh "issue" "close" "8" "--comment" "The record of N-03 now says it is done."`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the plan does not say %s; it says:\n%s", want, got)
		}
	}
	for _, not := range []string{`"9"`, "N-04", `"3"`, "kept"} {
		if strings.Contains(got, not) {
			t.Errorf("the plan touches %s:\n%s", not, got)
		}
	}
	if n := strings.Count(got, "\n"); n != 4 {
		t.Errorf("the plan has %d steps, not 4:\n%s", n, got)
	}
}
