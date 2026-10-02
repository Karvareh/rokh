package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// layouts are the sizes the shipped screens are drawn at, one per layout.
var layouts = []struct {
	name string
	o    Options
}{
	{"wide", Options{Columns: 118, Rows: 36}},
	{"medium", Options{Columns: 80, Rows: 24}},
	{"rail", Options{Columns: 48, Rows: 20}},
	{"compact", Options{Columns: 120, Rows: 12}},
	{"tiny", Options{Columns: 28, Rows: 8}},
}

// The screens ship as files: every view of the sample at every layout, drawn
// plain under screens/, so that the page of the art shows real screens. They
// are made by this test with -update and checked by it otherwise, and so is
// every screen the page shows, so none of them can go stale.
func TestTheShippedScreens(t *testing.T) {
	want := map[string]string{}
	for _, l := range layouts {
		for _, sh := range Shots() {
			want[filepath.Join("screens", l.name+"-"+sh.Name+".txt")] = Render(l.o, sh.View, sh.St) + "\n"
		}
	}
	if *update {
		if err := os.MkdirAll("screens", 0o755); err != nil {
			t.Fatal(err)
		}
		for path, text := range want {
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	names, err := filepath.Glob(filepath.Join("screens", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != len(want) {
		t.Errorf("screens/ holds %d files; the sample draws %d", len(names), len(want))
	}
	for path, text := range want {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != text {
			t.Errorf("%s is not the screen the sample draws (%v); run with -update if the drawing changed on purpose", path, err)
		}
	}

	// The page shows screens as fenced blocks, each after a line naming its
	// file; each must be that file, whole.
	page, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile("(?s)<!-- screen: (screens/[a-z-]+\\.txt) -->\n```text\n(.*?)```\n")
	shown := block.FindAllSubmatchIndex(page, -1)
	if len(shown) == 0 {
		t.Fatal("the page of the art shows no screen")
	}
	if *update {
		out := page[:0:0]
		last := 0
		for _, m := range shown {
			path := string(page[m[2]:m[3]])
			out = append(out, page[last:m[4]]...)
			out = append(out, want[path]...)
			out = append(out, page[m[5]:m[1]]...)
			last = m[1]
		}
		out = append(out, page[last:]...)
		if err := os.WriteFile("README.md", out, 0o644); err != nil {
			t.Fatal(err)
		}
		page = out
		shown = block.FindAllSubmatchIndex(page, -1)
	}
	for _, m := range shown {
		path := string(page[m[2]:m[3]])
		if text, ok := want[path]; !ok || string(page[m[4]:m[5]]) != text {
			t.Errorf("the page shows %s, and it is not that screen", path)
		}
	}
	if strings.Contains(string(page), "\x1b[") {
		t.Error("the page holds an escape sequence")
	}
}
