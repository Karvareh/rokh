// Command records keeps the records of a repository of Rokh true to their
// form.
//
// A record is a Markdown file that begins with front matter: a mission, a
// question, a study, a ruling. This program checks that every record carries
// the fields its kind asks for, with values its kind allows; that every
// identifier is unique and begins the name of its file; that what the fields
// say together holds; that every index table is the one its records make;
// and that every relative link in every document of the repository leads
// somewhere. It can also keep one issue for every record that has one: open
// while the record is, labelled with what the record says.
//
// Run it from tools/records:
//
//	go run . -root ../.. check    report every fault, and fail if there is one
//	go run . -root ../.. index    write the index tables again from the records
//	go run . -root ../.. issues   open, label, close and reopen the issues
//
// The rules of this repository's records are in rules.go. This file is the
// same in every repository of Rokh that keeps records; change it in all of
// them together.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Kind says how a collection keeps its records.
type Kind int

const (
	// Files keeps one Markdown file a record: DIR/ID-name.md.
	Files Kind = iota
	// Folders keeps one folder a record, with the record in its README.md:
	// DIR/ID-name/README.md. What else the folder holds is the record's own.
	Folders
)

// None is the value of a list that names nothing.
const None = "none"

// Field is one key of a record's front matter. Every field is required.
type Field struct {
	Key     string
	Allowed []string       // when set, the value is one of these
	Pattern *regexp.Regexp // when set, the value matches it
	List    bool           // a list of items separated by commas, or None
	Item    *regexp.Regexp // when set, every item of the list matches it
}

// Column is one column of an index table.
type Column struct {
	Head string
	Cell func(r *Record) string
}

// IssueRule says how the issue of a record follows it.
type IssueRule struct {
	// Managed names every label this program adds or removes. It leaves
	// every other label of an issue as it finds it.
	Managed []string
	// Labels are the managed labels the issue of r carries.
	Labels func(r *Record) []string
	// Open says whether the issue of r is open.
	Open func(r *Record) bool
}

// Collection is one kind of record.
type Collection struct {
	Name     string
	Dir      string
	Kind     Kind
	ID       *regexp.Regexp
	Prefixes []string // the order of identifiers: by prefix, then by number
	Fields   []Field
	// Rules returns what is wrong with r when its fields are read together
	// and beside every other record, which all holds by identifier.
	Rules   func(r *Record, all map[string]*Record) []string
	Index   string // the file that lists the collection, relative to the root
	Columns []Column
	Empty   string // what the index says while the collection is empty
	Issue   *IssueRule
}

// Record is one record, read.
type Record struct {
	Col    *Collection
	Path   string // relative to the root, with forward slashes
	Fields map[string]string
}

// ID is the record's identifier.
func (r *Record) ID() string { return r.Fields["id"] }

// Get is the value of one field.
func (r *Record) Get(k string) string { return r.Fields[k] }

// List is the items of a list field; None is no item.
func (r *Record) List(k string) []string {
	v := strings.TrimSpace(r.Fields[k])
	if v == "" || v == None {
		return nil
	}
	var out []string
	for _, it := range strings.Split(v, ",") {
		if it = strings.TrimSpace(it); it != "" {
			out = append(out, it)
		}
	}
	return out
}

// Link is the path of r as a link written in the file at from, both
// relative to the root.
func (r *Record) Link(from string) string {
	rel, err := filepath.Rel(path.Dir(from), r.Path)
	if err != nil {
		return r.Path
	}
	p := filepath.ToSlash(rel)
	if r.Col.Kind == Folders {
		p = path.Dir(p) + "/"
	}
	return p
}

// Options are what the command line asks for.
type Options struct {
	Command string // check, index or issues
	Root    string
	Dry     bool
	From    string
}

// ParseArgs reads the command line. Its flags may stand before the command
// or after it: "-root ../.. issues -dry" asks what "-root ../.. -dry issues"
// asks.
func ParseArgs(args []string) (Options, error) {
	var o Options
	fs := flag.NewFlagSet("records", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.Root, "root", ".", "the root of the repository")
	fs.BoolVar(&o.Dry, "dry", false, "issues: say what would be done, and do nothing")
	fs.StringVar(&o.From, "from", "", "issues: read the existing issues from this file, as the issue tool lists them, instead of asking for them")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() == 0 {
		return o, errors.New("no command: check, index or issues")
	}
	o.Command = fs.Arg(0)
	if err := fs.Parse(fs.Args()[1:]); err != nil {
		return o, err
	}
	if fs.NArg() != 0 {
		return o, fmt.Errorf("more than one command: %s", strings.Join(append([]string{o.Command}, fs.Args()...), " "))
	}
	switch o.Command {
	case "check", "index", "issues":
	default:
		return o, fmt.Errorf("no command %q: check, index or issues", o.Command)
	}
	if o.Command != "issues" && (o.Dry || o.From != "") {
		return o, errors.New("-dry and -from are for issues")
	}
	return o, nil
}

func main() {
	o, err := ParseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "records:", err)
		fmt.Fprintln(os.Stderr, "usage: records [-root DIR] check | index | issues [-dry] [-from FILE]")
		os.Exit(2)
	}
	switch o.Command {
	case "check":
		faults := Check(o.Root, collections)
		for _, f := range faults {
			fmt.Println(f)
		}
		if len(faults) > 0 {
			err = fmt.Errorf("%d faults", len(faults))
		} else {
			fmt.Println("records: every record, index and link holds")
		}
	case "index":
		err = WriteIndexes(o.Root, collections)
	case "issues":
		err = Issues(o.Root, collections, o.Dry, o.From, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "records:", err)
		os.Exit(1)
	}
}

// ---------- reading ----------

// plain is what a value may be without quotes: it then reads the same as
// front matter and as text, and a colon, a hash or a leading sign cannot
// change its meaning.
var plain = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ,./-]*$`)

// Front reads the front matter at the head of b: a line "---", lines
// "key: value", and a line "---". A value is plain, or quoted with double
// quotes.
func Front(b []byte) (map[string]string, error) {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil, errors.New("does not begin with front matter: a line ---")
	}
	body, _, ok := strings.Cut(s[4:], "\n---\n")
	if !ok {
		return nil, errors.New("its front matter has no closing line ---")
	}
	out := map[string]string{}
	for i, line := range strings.Split(body, "\n") {
		n := i + 2
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("line %d of the front matter is not key: value", n)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == "" {
			return nil, fmt.Errorf("line %d of the front matter has no key", n)
		}
		if strings.HasPrefix(v, `"`) {
			u, err := strconv.Unquote(v)
			if err != nil {
				return nil, fmt.Errorf("line %d: %s is not one quoted value", n, k)
			}
			v = u
		} else if v != "" && !plain.MatchString(v) {
			return nil, fmt.Errorf("line %d: the value of %s must be quoted", n, k)
		}
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("line %d: %s is given twice", n, k)
		}
		out[k] = v
	}
	return out, nil
}

// Load reads every record of every collection, and says what keeps a file
// from being one.
func Load(root string, cols []Collection) ([]*Record, []string) {
	var recs []*Record
	var faults []string
	for i := range cols {
		c := &cols[i]
		ents, err := os.ReadDir(filepath.Join(root, c.Dir))
		if err != nil {
			faults = append(faults, fmt.Sprintf("%s: %v", c.Dir, err))
			continue
		}
		for _, e := range ents {
			name := e.Name()
			if name == "README.md" || strings.HasPrefix(name, ".") {
				continue
			}
			var rel, stem string
			switch {
			case c.Kind == Files && !e.IsDir() && strings.HasSuffix(name, ".md"):
				rel, stem = path.Join(c.Dir, name), strings.TrimSuffix(name, ".md")
			case c.Kind == Folders && e.IsDir():
				rel, stem = path.Join(c.Dir, name, "README.md"), name
			default:
				want := "a file ID-name.md"
				if c.Kind == Folders {
					want = "a folder ID-name holding README.md"
				}
				faults = append(faults, fmt.Sprintf("%s: not a record; a record of %s is %s", path.Join(c.Dir, name), c.Name, want))
				continue
			}
			b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				faults = append(faults, fmt.Sprintf("%s: %v", rel, err))
				continue
			}
			f, err := Front(b)
			if err != nil {
				faults = append(faults, fmt.Sprintf("%s: %v", rel, err))
				continue
			}
			r := &Record{Col: c, Path: rel, Fields: f}
			if id := r.ID(); id != "" && !strings.HasPrefix(stem, id+"-") {
				faults = append(faults, fmt.Sprintf("%s: its name does not begin with its identifier %s and a dash", rel, id))
			}
			if !slug.MatchString(stem) {
				faults = append(faults, fmt.Sprintf("%s: its name is not ID-words, in small letters, digits, dots and dashes", rel))
			}
			recs = append(recs, r)
		}
	}
	return recs, faults
}

var slug = regexp.MustCompile(`^(?:[A-Z]+-)?[0-9]+-[a-z0-9.]+(?:-[a-z0-9.]+)*$`)

// ---------- checking ----------

// Check returns every fault of the records, the indexes and the links under
// root. None means everything holds.
func Check(root string, cols []Collection) []string {
	recs, faults := Load(root, cols)
	all := map[string]*Record{}
	for _, r := range recs {
		faults = append(faults, fields(r)...)
		id := r.ID()
		if id == "" {
			continue
		}
		if prev, dup := all[id]; dup {
			faults = append(faults, fmt.Sprintf("%s: %s is already the identifier of %s", r.Path, id, prev.Path))
			continue
		}
		all[id] = r
	}
	for _, r := range recs {
		if r.Col.Rules != nil {
			for _, f := range r.Col.Rules(r, all) {
				faults = append(faults, fmt.Sprintf("%s: %s", r.Path, f))
			}
		}
	}
	for i := range cols {
		c := &cols[i]
		if c.Index == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.Index)))
		if err != nil {
			faults = append(faults, fmt.Sprintf("%s: %v", c.Index, err))
			continue
		}
		got, err := between(string(b), c.Name)
		if err != nil {
			faults = append(faults, fmt.Sprintf("%s: %v", c.Index, err))
			continue
		}
		if want := Table(c, recs); got != want {
			faults = append(faults, fmt.Sprintf("%s: the table of %s is not the one its records make; run: go run . -root ../.. index", c.Index, c.Name))
		}
	}
	faults = append(faults, Links(root)...)
	return faults
}

func fields(r *Record) []string {
	var out []string
	known := map[string]bool{}
	for _, f := range r.Col.Fields {
		known[f.Key] = true
		v, ok := r.Fields[f.Key]
		if !ok || v == "" {
			out = append(out, fmt.Sprintf("%s: %s is missing", r.Path, f.Key))
			continue
		}
		if f.Key == "id" && r.Col.ID != nil && !r.Col.ID.MatchString(v) {
			out = append(out, fmt.Sprintf("%s: %q is not an identifier of %s", r.Path, v, r.Col.Name))
		}
		if f.Pattern != nil && !f.Pattern.MatchString(v) {
			out = append(out, fmt.Sprintf("%s: %s is %q, which is not of its form", r.Path, f.Key, v))
		}
		if f.Allowed != nil && !contains(f.Allowed, v) {
			out = append(out, fmt.Sprintf("%s: %s is %q, which is not one of %s", r.Path, f.Key, v, strings.Join(f.Allowed, ", ")))
		}
		if f.List && f.Item != nil {
			for _, it := range r.List(f.Key) {
				if !f.Item.MatchString(it) {
					out = append(out, fmt.Sprintf("%s: %s names %q, which is not an identifier it may name", r.Path, f.Key, it))
				}
			}
		}
	}
	var extra []string
	for k := range r.Fields {
		if !known[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		out = append(out, fmt.Sprintf("%s: %s is not a field of %s", r.Path, k, r.Col.Name))
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// ---------- indexes ----------

func begin(name string) string { return "<!-- records:" + name + " -->" }
func end(name string) string   { return "<!-- /records:" + name + " -->" }

func between(s, name string) (string, error) {
	_, rest, ok := strings.Cut(s, begin(name))
	if !ok {
		return "", fmt.Errorf("it has no line %s", begin(name))
	}
	got, _, ok := strings.Cut(rest, end(name))
	if !ok {
		return "", fmt.Errorf("it has no line %s", end(name))
	}
	return got, nil
}

// Table is what stands between the markers of c's index: the records of c,
// one row each, in the order of their identifiers.
func Table(c *Collection, recs []*Record) string {
	var mine []*Record
	for _, r := range recs {
		if r.Col == c && r.ID() != "" {
			mine = append(mine, r)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return less(c, mine[i].ID(), mine[j].ID()) })
	var b strings.Builder
	b.WriteString("\n")
	if len(mine) == 0 {
		b.WriteString(c.Empty + "\n")
		return b.String()
	}
	var heads, rule []string
	for _, col := range c.Columns {
		heads = append(heads, col.Head)
		rule = append(rule, "---")
	}
	b.WriteString("| " + strings.Join(heads, " | ") + " |\n")
	b.WriteString("|" + strings.Join(rule, "|") + "|\n")
	for _, r := range mine {
		var cells []string
		for _, col := range c.Columns {
			cells = append(cells, strings.ReplaceAll(col.Cell(r), "|", `\|`))
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	return b.String()
}

var idParts = regexp.MustCompile(`^([A-Za-z]*)-?([0-9]+)$`)

func less(c *Collection, a, b string) bool {
	rank := func(id string) (int, int) {
		m := idParts.FindStringSubmatch(id)
		if m == nil {
			return len(c.Prefixes), 0
		}
		n, _ := strconv.Atoi(m[2])
		for i, p := range c.Prefixes {
			if p == m[1] {
				return i, n
			}
		}
		return len(c.Prefixes), n
	}
	pa, na := rank(a)
	pb, nb := rank(b)
	if pa != pb {
		return pa < pb
	}
	if na != nb {
		return na < nb
	}
	return a < b
}

// WriteIndexes writes every index table again from the records.
func WriteIndexes(root string, cols []Collection) error {
	recs, faults := Load(root, cols)
	if len(faults) > 0 {
		return fmt.Errorf("some files are not records, so no index is written:\n  %s", strings.Join(faults, "\n  "))
	}
	for i := range cols {
		c := &cols[i]
		if c.Index == "" {
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(c.Index))
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		s := string(b)
		got, err := between(s, c.Name)
		if err != nil {
			return fmt.Errorf("%s: %v", c.Index, err)
		}
		want := Table(c, recs)
		if got == want {
			continue
		}
		s = strings.Replace(s, begin(c.Name)+got+end(c.Name), begin(c.Name)+want+end(c.Name), 1)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			return err
		}
		fmt.Printf("records: wrote the table of %s in %s\n", c.Name, c.Index)
	}
	return nil
}

// ---------- links ----------

var (
	reLink   = regexp.MustCompile(`\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	reInline = regexp.MustCompile("`[^`]*`")
)

// Links returns every relative link, in every Markdown document under root,
// that leads nowhere. Links to other hosts and to a place in the same
// document are not followed; code is not read.
func Links(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			out = append(out, err.Error())
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		f, err := os.Open(p)
		if err != nil {
			out = append(out, fmt.Sprintf("%s: %v", rel, err))
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
		fence := false
		for n := 1; sc.Scan(); n++ {
			line := sc.Text()
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				fence = !fence
				continue
			}
			if fence {
				continue
			}
			for _, m := range reLink.FindAllStringSubmatch(reInline.ReplaceAllString(line, ""), -1) {
				if f := follow(root, path.Dir(rel), m[1]); f != "" {
					out = append(out, fmt.Sprintf("%s:%d: %s", rel, n, f))
				}
			}
		}
		return nil
	})
	return out
}

func follow(root, dir, target string) string {
	if strings.HasPrefix(target, "#") || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return ""
	}
	t := target
	if i := strings.IndexAny(t, "#?"); i >= 0 {
		t = t[:i]
	}
	u, err := url.PathUnescape(t)
	if err != nil {
		return fmt.Sprintf("%s is not a well-formed link", target)
	}
	var p string
	if strings.HasPrefix(u, "/") {
		p = path.Clean(strings.TrimPrefix(u, "/"))
	} else {
		p = path.Join(dir, u)
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Sprintf("%s leads out of the repository", target)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil {
		return fmt.Sprintf("%s leads nowhere", target)
	}
	return ""
}

// ---------- issues ----------

// Issue is one issue as the issue tool lists it.
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// Issues keeps one issue for every record of a collection that has an
// IssueRule: it opens the issue of an open record that has none, sets its
// title and managed labels to what the record says, and closes or reopens
// it as the record is closed or open. It never opens an issue for a record
// that is already closed, and touches no issue whose title does not begin
// with a record's identifier and a colon. Dry, it says what it would do.
func Issues(root string, cols []Collection, dry bool, from string, w io.Writer) error {
	recs, faults := Load(root, cols)
	if len(faults) > 0 {
		return fmt.Errorf("some files are not records:\n  %s", strings.Join(faults, "\n  "))
	}
	var raw []byte
	var err error
	if from != "" {
		raw, err = os.ReadFile(from)
	} else {
		raw, err = exec.Command("gh", "issue", "list", "--state", "all", "--limit", "5000",
			"--json", "number,title,state,labels").Output()
	}
	if err != nil {
		return fmt.Errorf("listing the issues: %v", err)
	}
	var have []Issue
	if err := json.Unmarshal(raw, &have); err != nil {
		return fmt.Errorf("reading the list of issues: %v", err)
	}
	byID := map[string]Issue{}
	for _, is := range have {
		id, _, ok := strings.Cut(is.Title, ":")
		if !ok {
			continue
		}
		id = strings.TrimSpace(id)
		if prev, dup := byID[id]; dup && prev.Number < is.Number {
			continue // the first issue of an identifier is its own
		}
		byID[id] = is
	}
	run := func(args ...string) error {
		if dry {
			q := make([]string, len(args))
			for i, a := range args {
				q[i] = strconv.Quote(a)
			}
			fmt.Fprintln(w, "gh "+strings.Join(q, " "))
			return nil
		}
		cmd := exec.Command("gh", args...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	}
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].Col != recs[j].Col {
			return recs[i].Col.Name < recs[j].Col.Name
		}
		return less(recs[i].Col, recs[i].ID(), recs[j].ID())
	})
	for _, r := range recs {
		rule := r.Col.Issue
		if rule == nil || r.ID() == "" {
			continue
		}
		title := r.ID() + ": " + r.Get("title")
		want := rule.Labels(r)
		open := rule.Open(r)
		is, ok := byID[r.ID()]
		if !ok {
			if !open {
				continue
			}
			args := []string{"issue", "create", "--title", title, "--body", issueBody(r)}
			if len(want) > 0 {
				args = append(args, "--label", strings.Join(want, ","))
			}
			if err := run(args...); err != nil {
				return fmt.Errorf("%s: %v", r.ID(), err)
			}
			continue
		}
		num := strconv.Itoa(is.Number)
		hasLabel := map[string]bool{}
		for _, l := range is.Labels {
			hasLabel[l.Name] = true
		}
		wanted := map[string]bool{}
		var add, remove []string
		for _, l := range want {
			wanted[l] = true
			if !hasLabel[l] {
				add = append(add, l)
			}
		}
		for _, l := range rule.Managed {
			if hasLabel[l] && !wanted[l] {
				remove = append(remove, l)
			}
		}
		if is.Title != title || len(add)+len(remove) > 0 {
			args := []string{"issue", "edit", num}
			if is.Title != title {
				args = append(args, "--title", title)
			}
			if len(add) > 0 {
				args = append(args, "--add-label", strings.Join(add, ","))
			}
			if len(remove) > 0 {
				args = append(args, "--remove-label", strings.Join(remove, ","))
			}
			if err := run(args...); err != nil {
				return fmt.Errorf("%s: %v", r.ID(), err)
			}
		}
		var e error
		closed := strings.EqualFold(is.State, "closed")
		switch {
		case open && closed:
			e = run("issue", "reopen", num)
		case !open && !closed:
			e = run("issue", "close", num, "--comment",
				fmt.Sprintf("The record of %s now says it is %s.", r.ID(), r.Get("status")))
		}
		if e != nil {
			return fmt.Errorf("%s: %v", r.ID(), e)
		}
	}
	return nil
}

func issueBody(r *Record) string {
	where := "`" + r.Path + "`"
	server, repo := os.Getenv("GITHUB_SERVER_URL"), os.Getenv("GITHUB_REPOSITORY")
	if server != "" && repo != "" {
		where = fmt.Sprintf("[%s](%s/%s/blob/main/%s)", r.Path, server, repo, (&url.URL{Path: r.Path}).EscapedPath())
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "This issue stands for the record %s.\n\n", where)
	b.WriteString("The record is the truth: what it asks, what it waits for and when it is done are written there, and they change there, by a pull request. This issue is for claiming the record and talking about it. Its title, its labels and whether it is open follow the record.\n")
	return b.String()
}
