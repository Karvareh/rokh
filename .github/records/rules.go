package main

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	date     = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	number   = regexp.MustCompile(`^[0-9]{4}$`)
	question = regexp.MustCompile(`^D-[0-9]{2,}$`)
	mission  = regexp.MustCompile(`^[WI]-[0-9]{2,}$`)
)

// The records of Rokh are kept in three of its four folders: the rulings of
// docs/, the studies and the questions of lab/, and the missions of work/.
// Where a record names a record of another folder, the name is held to a
// record that exists: the questions a ruling answers, the missions a
// question blocks and the ruling that rules it, and what a mission needs.
var collections = []Collection{rulings, studies, questions, missions}

// A ruling is never edited once it is in force: a ruling that changes it is
// a new record that names it in replaces, and the old record then names the
// new one in replaced-by.
var rulings = Collection{
	Name: "rulings",
	Dir:  "docs/rulings",
	Kind: Files,
	ID:   number,
	Fields: []Field{
		{Key: "id"},
		{Key: "title"},
		{Key: "date", Pattern: date},
		{Key: "status", Allowed: []string{"in-force", "replaced"}},
		{Key: "answers", List: true, Item: question},
		{Key: "replaces", List: true, Item: number},
		{Key: "replaced-by", List: true, Item: number},
	},
	Rules: func(r *Record, all Records) []string {
		var out []string
		by := r.List("replaced-by")
		switch {
		case r.Get("status") == "replaced" && len(by) != 1:
			out = append(out, "it is replaced, so replaced-by names the one ruling that replaces it")
		case r.Get("status") == "in-force" && len(by) != 0:
			out = append(out, "it is in force, so nothing replaces it yet")
		}
		for _, n := range by {
			s, ok := all.Get("rulings", n)
			if !ok {
				out = append(out, fmt.Sprintf("replaced-by names %s, which is not a ruling of docs/rulings", n))
				continue
			}
			if !contains(s.List("replaces"), r.ID()) {
				out = append(out, fmt.Sprintf("ruling %s does not say that it replaces this one", n))
			}
		}
		for _, n := range r.List("replaces") {
			old, ok := all.Get("rulings", n)
			if !ok {
				out = append(out, fmt.Sprintf("replaces names %s, which is not a ruling of docs/rulings", n))
				continue
			}
			if !contains(old.List("replaced-by"), r.ID()) {
				out = append(out, fmt.Sprintf("ruling %s does not say that this one replaces it", n))
			}
		}
		for _, d := range r.List("answers") {
			if _, ok := all.Get("questions", d); !ok {
				out = append(out, fmt.Sprintf("it answers %s, which is not a question of lab/", d))
			}
		}
		return out
	},
	Index: "docs/rulings/README.md",
	Columns: []Column{
		{"Ruling", func(r *Record) string { return fmt.Sprintf("[%s](%s)", r.ID(), r.Link("docs/rulings/README.md")) }},
		{"What is ruled", func(r *Record) string { return r.Get("title") }},
		{"Date", func(r *Record) string { return r.Get("date") }},
		{"Answers", func(r *Record) string { return listOrDash(r.List("answers")) }},
		{"Status", func(r *Record) string {
			if by := r.List("replaced-by"); len(by) > 0 {
				return "replaced by " + by[0]
			}
			return r.Get("status")
		}},
	},
	Empty: "No ruling is recorded yet.",
}

// A study is a folder of lab/studies, with its evidence beside it.
var studies = Collection{
	Name: "studies",
	Dir:  "lab/studies",
	Kind: Folders,
	ID:   number,
	Fields: []Field{
		{Key: "id"},
		{Key: "title"},
		{Key: "status", Allowed: []string{"draft", "examined", "superseded"}},
		{Key: "of"},
		{Key: "date", Pattern: date},
	},
	Index: "lab/README.md",
	Columns: []Column{
		{"Study", func(r *Record) string { return fmt.Sprintf("[%s](%s)", r.ID(), r.Link("lab/README.md")) }},
		{"What", func(r *Record) string { return r.Get("title") }},
		{"Of", func(r *Record) string { return r.Get("of") }},
		{"Status", func(r *Record) string { return r.Get("status") }},
		{"Date", func(r *Record) string { return r.Get("date") }},
	},
	Empty: "No study is recorded.",
}

// A question waits for the owner's ruling; once the owner rules, the ruling
// is a record of docs/rulings and the question names it.
var questions = Collection{
	Name:     "questions",
	Dir:      "lab/questions",
	Kind:     Files,
	ID:       question,
	Prefixes: []string{"D"},
	Fields: []Field{
		{Key: "id"},
		{Key: "title"},
		{Key: "priority", Allowed: []string{"p1", "p2", "p3"}},
		{Key: "status", Allowed: []string{"open", "ruled", "withdrawn"}},
		{Key: "blocks", List: true, Item: mission},
		{Key: "evidence"},
		{Key: "ruling"},
	},
	Rules: func(r *Record, all Records) []string {
		var out []string
		switch v := r.Get("ruling"); {
		case r.Get("status") == "ruled" && !number.MatchString(v):
			out = append(out, fmt.Sprintf("it is ruled, so ruling names the record of docs/rulings that rules it (four digits), not %q", v))
		case r.Get("status") == "ruled":
			if _, ok := all.Get("rulings", v); !ok {
				out = append(out, fmt.Sprintf("it is ruled by %s, which is not a ruling of docs/rulings", v))
			}
		case v != None:
			out = append(out, fmt.Sprintf("it is %s, so ruling is %s, not %q", r.Get("status"), None, v))
		}
		for _, m := range r.List("blocks") {
			if _, ok := all.Get("missions", m); !ok {
				out = append(out, fmt.Sprintf("it blocks %s, which is not a mission of work/", m))
			}
		}
		return out
	},
	Index: "lab/README.md",
	Columns: []Column{
		{"Question", func(r *Record) string { return fmt.Sprintf("[%s](%s)", r.ID(), r.Link("lab/README.md")) }},
		{"What is asked", func(r *Record) string { return r.Get("title") }},
		{"Priority", func(r *Record) string { return r.Get("priority") }},
		{"Status", func(r *Record) string { return r.Get("status") }},
		{"Blocks", func(r *Record) string { return listOrDash(r.List("blocks")) }},
		{"Ruling", func(r *Record) string {
			if v := r.Get("ruling"); v != None {
				return v
			}
			return "—"
		}},
	},
	Empty: "No question is recorded.",
	Issue: &IssueRule{
		Managed: []string{"question", "p1", "p2", "p3", "needs-ruling"},
		Labels: func(r *Record) []string {
			out := []string{"question", r.Get("priority")}
			if r.Get("status") == "open" {
				out = append(out, "needs-ruling")
			}
			return out
		},
		Open: func(r *Record) bool { return r.Get("status") == "open" },
	},
}

// A mission (W) changes something; an investigation (I) finds something out,
// and what it finds becomes a study of lab/. Each lands in one of the four
// folders.
var missions = Collection{
	Name:     "missions",
	Dir:      "work/missions",
	Kind:     Files,
	ID:       mission,
	Prefixes: []string{"W", "I"},
	Fields: []Field{
		{Key: "id"},
		{Key: "title"},
		{Key: "kind", Allowed: []string{"mission", "investigation"}},
		{Key: "priority", Allowed: []string{"p1", "p2", "p3"}},
		{Key: "status", Allowed: []string{"ready", "blocked", "done", "withdrawn"}},
		{Key: "needs", List: true, Item: regexp.MustCompile(`^[WID]-[0-9]{2,}$`)},
		{Key: "lands-in", Allowed: []string{"source", "docs", "lab", "work"}},
		{Key: "evidence"},
	},
	Rules: missionRules,
	Index: "work/README.md",
	Columns: []Column{
		{"Mission", func(r *Record) string { return fmt.Sprintf("[%s](%s)", r.ID(), r.Link("work/README.md")) }},
		{"What", func(r *Record) string { return r.Get("title") }},
		{"Priority", func(r *Record) string { return r.Get("priority") }},
		{"Status", func(r *Record) string { return r.Get("status") }},
		{"Needs", func(r *Record) string { return listOrDash(r.List("needs")) }},
		{"Lands in", func(r *Record) string { return r.Get("lands-in") }},
	},
	Empty: "No mission is recorded.",
	Issue: &IssueRule{
		// Who has claimed a mission, and whether a pull request is open
		// for it, are said on its issue (claimed, in-review), not in its
		// record; this program leaves those labels alone.
		Managed: []string{"mission", "investigation", "p1", "p2", "p3",
			"ready", "blocked", "needs-ruling"},
		Labels: func(r *Record) []string {
			out := []string{r.Get("kind"), r.Get("priority")}
			switch s := r.Get("status"); s {
			case "ready", "blocked":
				out = append(out, s)
			}
			for _, n := range r.List("needs") {
				if strings.HasPrefix(n, "D-") {
					out = append(out, "needs-ruling")
					break
				}
			}
			return out
		},
		Open: func(r *Record) bool {
			s := r.Get("status")
			return s != "done" && s != "withdrawn"
		},
	},
}

// missionRules holds a mission's fields to one another: its kind follows
// its identifier, and its status follows what it needs. A question of lab/
// is a need until it is ruled; then the need is struck here, in the change
// that says what the ruling allows.
func missionRules(r *Record, all Records) []string {
	var out []string
	if want := map[byte]string{'W': "mission", 'I': "investigation"}[r.ID()[0]]; r.Get("kind") != want {
		out = append(out, fmt.Sprintf("by its identifier it is of the kind %s, not %s", want, r.Get("kind")))
	}
	waiting := false
	for _, n := range r.List("needs") {
		switch {
		case n == r.ID():
			out = append(out, "it needs itself")
		case strings.HasPrefix(n, "D-"):
			if _, ok := all.Get("questions", n); !ok {
				out = append(out, fmt.Sprintf("it needs %s, which is not a question of lab/", n))
			}
			waiting = true
		default:
			m, ok := all.Get("missions", n)
			if !ok {
				out = append(out, fmt.Sprintf("it needs %s, which is not a mission of work/", n))
				continue
			}
			if m.Get("status") != "done" {
				waiting = true
			}
		}
	}
	switch r.Get("status") {
	case "ready":
		if waiting {
			out = append(out, "it is ready, but something it needs is not done or not ruled")
		}
	case "blocked":
		if !waiting {
			out = append(out, "it is blocked, but everything it needs is done: it is ready")
		}
	}
	return out
}

func listOrDash(l []string) string {
	if len(l) == 0 {
		return "—"
	}
	return strings.Join(l, ", ")
}
