package main

import (
	"fmt"
	"regexp"
	"strings"
)

// The records of rokh-work are its missions. A mission (W) changes
// something; an investigation (I) finds something out, and what it finds
// becomes a study of rokh-lab.
var collections = []Collection{{
	Name:     "missions",
	Dir:      "missions",
	Kind:     Files,
	ID:       regexp.MustCompile(`^[WI]-[0-9]{2,}$`),
	Prefixes: []string{"W", "I"},
	Fields: []Field{
		{Key: "id"},
		{Key: "title"},
		{Key: "kind", Allowed: []string{"mission", "investigation"}},
		{Key: "priority", Allowed: []string{"p1", "p2", "p3"}},
		{Key: "status", Allowed: []string{"ready", "blocked", "done", "withdrawn"}},
		{Key: "needs", List: true, Item: regexp.MustCompile(`^[WID]-[0-9]{2,}$`)},
		{Key: "lands-in", Allowed: []string{"rokh", "rokh-docs", "rokh-lab", "rokh-work"}},
		{Key: "evidence"},
	},
	Rules: missionRules,
	Index: "README.md",
	Columns: []Column{
		{"Mission", func(r *Record) string { return fmt.Sprintf("[%s](%s)", r.ID(), r.Link("README.md")) }},
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
}}

// missionRules holds a mission's fields to one another: its kind follows
// its identifier, and its status follows what it needs. A question of
// rokh-lab is a need until it is ruled; then the need is struck here, in the
// change that says what the ruling allows.
func missionRules(r *Record, all map[string]*Record) []string {
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
			waiting = true
		default:
			m, ok := all[n]
			if !ok {
				out = append(out, fmt.Sprintf("it needs %s, which is not a mission of this repository", n))
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
