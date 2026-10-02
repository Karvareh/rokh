package main

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	date    = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	ruling  = regexp.MustCompile(`^[0-9]{4}$`)
	mission = regexp.MustCompile(`^[WI]-[0-9]{2,}$`)
)

// The records of rokh-lab are its studies, each a folder with its evidence,
// and its questions, each waiting for the owner's ruling.
var collections = []Collection{
	{
		Name: "studies",
		Dir:  "studies",
		Kind: Folders,
		ID:   regexp.MustCompile(`^[0-9]{4}$`),
		Fields: []Field{
			{Key: "id"},
			{Key: "title"},
			{Key: "status", Allowed: []string{"draft", "examined", "superseded"}},
			{Key: "of"},
			{Key: "date", Pattern: date},
		},
		Index: "README.md",
		Columns: []Column{
			{"Study", func(r *Record) string { return fmt.Sprintf("[%s](%s)", r.ID(), r.Link("README.md")) }},
			{"What", func(r *Record) string { return r.Get("title") }},
			{"Of", func(r *Record) string { return r.Get("of") }},
			{"Status", func(r *Record) string { return r.Get("status") }},
			{"Date", func(r *Record) string { return r.Get("date") }},
		},
		Empty: "No study is recorded.",
	},
	{
		Name:     "questions",
		Dir:      "questions",
		Kind:     Files,
		ID:       regexp.MustCompile(`^D-[0-9]{2,}$`),
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
		Rules: func(r *Record, all map[string]*Record) []string {
			var out []string
			switch v := r.Get("ruling"); {
			case r.Get("status") == "ruled" && !ruling.MatchString(v):
				out = append(out, fmt.Sprintf("it is ruled, so ruling names the record of rokh-docs that rules it (four digits), not %q", v))
			case r.Get("status") != "ruled" && v != None:
				out = append(out, fmt.Sprintf("it is %s, so ruling is %s, not %q", r.Get("status"), None, v))
			}
			return out
		},
		Index: "README.md",
		Columns: []Column{
			{"Question", func(r *Record) string { return fmt.Sprintf("[%s](%s)", r.ID(), r.Link("README.md")) }},
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
	},
}

func listOrDash(l []string) string {
	if len(l) == 0 {
		return "—"
	}
	return strings.Join(l, ", ")
}
