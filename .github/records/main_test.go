package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// testCols is a collection of the test's own, so that these tests hold
// whatever rules.go says.
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
		Rules: func(r *Record, all Records) []string {
			var out []string
			for _, n := range r.List("needs") {
				if _, ok := all.Get("notes", n); !ok {
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
	// Away from a workflow the body names the record's path; in one, it
	// links to it. These tests are of the first.
	t.Setenv("GITHUB_SERVER_URL", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	root := t.TempDir()
	write(t, root, "README.md", readme)
	write(t, root, "notes/N-01-one.md", note("N-01", "One", "open", "none"))     // no issue: open one
	write(t, root, "notes/N-02-two.md", note("N-02", "Two", "open", "none"))     // issue closed, title, body and labels stale: reopen, edit
	write(t, root, "notes/N-03-three.md", note("N-03", "Three", "done", "none")) // issue open: close it
	write(t, root, "notes/N-04-four.md", note("N-04", "Four", "done", "none"))   // no issue, done: nothing
	write(t, root, "notes/N-05-five.md", note("N-05", "Five", "open", "none"))   // in step: nothing
	write(t, root, "notes/N-06-six.md", note("N-06", "Six", "open", "none"))     // moved here from elsewhere: its body names the old place
	body := func(path string) string { return issueBody(&Record{Path: path}) }
	type label struct {
		Name string `json:"name"`
	}
	type issue struct {
		Number int     `json:"number"`
		Title  string  `json:"title"`
		State  string  `json:"state"`
		Body   string  `json:"body,omitempty"`
		Labels []label `json:"labels"`
	}
	list, err := json.Marshal([]issue{
		{7, "N-02: Old", "CLOSED", "", []label{{"p1"}, {"kept"}}},
		{8, "N-03: Three", "OPEN", body("notes/N-03-three.md"), []label{{"open"}}},
		{9, "N-05: Five", "OPEN", strings.ReplaceAll(body("notes/N-05-five.md"), "\n", "\r\n"), []label{{"open"}}},
		{10, "N-06: Six", "OPEN", body("elsewhere/N-06-six.md"), []label{{"open"}}},
		{3, "Not a record", "OPEN", "", []label{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "issues.json", string(list))
	var out bytes.Buffer
	if err := Issues(root, testCols(), true, filepath.Join(root, "issues.json"), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		`gh "issue" "create" "--title" "N-01: One" "--body" "This issue stands for the record ` + "`notes/N-01-one.md`",
		`"--label" "open"`,
		`gh "issue" "edit" "7" "--title" "N-02: Two" "--body" "This issue stands for the record ` + "`notes/N-02-two.md`",
		`"--add-label" "open" "--remove-label" "p1"`,
		`gh "issue" "reopen" "7"`,
		`gh "issue" "close" "8" "--comment" "The record of N-03 now says it is done."`,
		`gh "issue" "edit" "10" "--body" "This issue stands for the record ` + "`notes/N-06-six.md`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the plan does not say %s; it says:\n%s", want, got)
		}
	}
	for _, not := range []string{`"9"`, "N-04", `"3"`, "kept", "elsewhere"} {
		if strings.Contains(got, not) {
			t.Errorf("the plan touches %s:\n%s", not, got)
		}
	}
	if n := strings.Count(got, "\n"); n != 5 {
		t.Errorf("the plan has %d steps, not 5:\n%s", n, got)
	}
}

// TestRecordsNameRecordsOfOtherFoldersThatExist builds a small tree with the
// rules of Rokh's own records, and holds every name that crosses a folder to
// a record that exists. A study and a ruling share a number, for each is
// unique among its own kind.
func TestRecordsNameRecordsOfOtherFoldersThatExist(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/rulings/README.md", "<!-- records:rulings -->\n<!-- /records:rulings -->\n")
	write(t, root, "lab/README.md", "<!-- records:studies -->\n<!-- /records:studies -->\n<!-- records:questions -->\n<!-- /records:questions -->\n")
	write(t, root, "work/README.md", "<!-- records:missions -->\n<!-- /records:missions -->\n")
	write(t, root, "docs/rulings/0001-one.md", "---\nid: 0001\ntitle: \"One\"\ndate: 2026-10-02\nstatus: in-force\nanswers: D-01, D-09\nreplaces: none\nreplaced-by: none\n---\n")
	write(t, root, "lab/studies/0001-one/README.md", "---\nid: 0001\ntitle: \"One\"\nstatus: examined\nof: \"the tree\"\ndate: 2026-10-02\n---\n")
	write(t, root, "lab/questions/D-01-one.md", "---\nid: D-01\ntitle: \"One\"\npriority: p1\nstatus: ruled\nblocks: W-01, W-07\nevidence: \"none\"\nruling: 0002\n---\n")
	write(t, root, "work/missions/W-01-one.md", "---\nid: W-01\ntitle: \"One\"\nkind: mission\npriority: p1\nstatus: blocked\nneeds: D-01, D-08, W-05\nlands-in: source\nevidence: \"none\"\n---\n")
	if err := WriteIndexes(root, collections); err != nil {
		t.Fatal(err)
	}
	faults := strings.Join(Check(root, collections), "\n")
	for _, want := range []string{
		"docs/rulings/0001-one.md: it answers D-09, which is not a question of lab/",
		"lab/questions/D-01-one.md: it is ruled by 0002, which is not a ruling of docs/rulings",
		"lab/questions/D-01-one.md: it blocks W-07, which is not a mission of work/",
		"work/missions/W-01-one.md: it needs D-08, which is not a question of lab/",
		"work/missions/W-01-one.md: it needs W-05, which is not a mission of work/",
	} {
		if !strings.Contains(faults, want) {
			t.Errorf("no fault says %q; the faults are:\n%s", want, faults)
		}
	}
	for _, not := range []string{"already the identifier", "D-01, which", "W-01, which", "it answers D-01"} {
		if strings.Contains(faults, not) {
			t.Errorf("a fault says %q, which holds:\n%s", not, faults)
		}
	}
	if n := strings.Count(faults, "\n") + 1; n != 5 {
		t.Errorf("%d faults, not 5:\n%s", n, faults)
	}
}

func TestTheFlagsMayStandOnEitherSideOfTheCommand(t *testing.T) {
	for _, args := range [][]string{
		{"-root", "../..", "-dry", "-from", "list.json", "issues"},
		{"-root", "../..", "issues", "-dry", "-from", "list.json"},
		{"issues", "-root", "../..", "-from", "list.json", "-dry"},
	} {
		o, err := ParseArgs(args)
		if err != nil {
			t.Fatalf("%q: %v", args, err)
		}
		if o != (Options{Command: "issues", Root: "../..", Dry: true, From: "list.json"}) {
			t.Fatalf("%q read as %+v", args, o)
		}
	}
	for _, args := range [][]string{
		{},
		{"-root", "../.."},
		{"check", "index"},
		{"publish"},
		{"check", "-dry"},
		{"index", "-from", "list.json"},
		{"issues", "-nothing"},
	} {
		if o, err := ParseArgs(args); err == nil {
			t.Errorf("%q read as %+v, without a fault", args, o)
		}
	}
}
