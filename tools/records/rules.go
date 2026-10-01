package main

import (
	"fmt"
	"regexp"
	"strings"
)

var number = regexp.MustCompile(`^[0-9]{4}$`)

// The records of rokh-docs are its rulings. A ruling is never edited once
// it is in force: a ruling that changes it is a new record that names it in
// replaces, and the old record then names the new one in replaced-by.
var collections = []Collection{{
	Name: "rulings",
	Dir:  "rulings",
	Kind: Files,
	ID:   number,
	Fields: []Field{
		{Key: "id"},
		{Key: "title"},
		{Key: "date", Pattern: regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)},
		{Key: "status", Allowed: []string{"in-force", "replaced"}},
		{Key: "answers", List: true, Item: regexp.MustCompile(`^D-[0-9]{2,}$`)},
		{Key: "replaces", List: true, Item: number},
		{Key: "replaced-by", List: true, Item: number},
	},
	Rules: func(r *Record, all map[string]*Record) []string {
		var out []string
		by := r.List("replaced-by")
		switch {
		case r.Get("status") == "replaced" && len(by) != 1:
			out = append(out, "it is replaced, so replaced-by names the one ruling that replaces it")
		case r.Get("status") == "in-force" && len(by) != 0:
			out = append(out, "it is in force, so nothing replaces it yet")
		}
		for _, n := range by {
			s, ok := all[n]
			if !ok {
				out = append(out, fmt.Sprintf("replaced-by names %s, which is not a ruling here", n))
				continue
			}
			if !contains(s.List("replaces"), r.ID()) {
				out = append(out, fmt.Sprintf("ruling %s does not say that it replaces this one", n))
			}
		}
		for _, n := range r.List("replaces") {
			old, ok := all[n]
			if !ok {
				out = append(out, fmt.Sprintf("replaces names %s, which is not a ruling here", n))
				continue
			}
			if !contains(old.List("replaced-by"), r.ID()) {
				out = append(out, fmt.Sprintf("ruling %s does not say that this one replaces it", n))
			}
		}
		return out
	},
	Index: "rulings/README.md",
	Columns: []Column{
		{"Ruling", func(r *Record) string { return fmt.Sprintf("[%s](%s)", r.ID(), r.Link("rulings/README.md")) }},
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
}}

func listOrDash(l []string) string {
	if len(l) == 0 {
		return "—"
	}
	return strings.Join(l, ", ")
}
