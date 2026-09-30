package index

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTheFoldFindsAWordHoweverItWasTyped(t *testing.T) {
	for _, group := range [][]string{
		{"کتابخانه", "كتابخانه", "کتاب‌خانه", "كتاب‌خانه"},
		{"۱۴۰۵", "1405", "١٤٠٥"},
		{"آزمون", "ازمون"},
		{"مسئله", "مسيله", "مسیله"},
		{"خانهٔ", "خانه"},
		{"Rokh", "rokh", "ROKH"},
	} {
		want := Key(group[0])
		for _, w := range group[1:] {
			if got := Key(w); got != want {
				t.Errorf("%q folds to %q, %q to %q", group[0], want, w, got)
			}
		}
	}
	if Key("کتاب") == Key("کتابخانه") {
		t.Fatal("different words folded together")
	}
}

func TestTokensPointAtExactBytes(t *testing.T) {
	text := []byte("این «کتاب‌خانهٔ» ساختگی ۱۴۰۵ است.\nسطر دوم")
	for _, tok := range Tokenize(text) {
		word := string(text[tok.Start:tok.End])
		if !utf8.ValidString(word) || Key(word) != tok.Key {
			t.Fatalf("token %q at [%d,%d) does not read back", word, tok.Start, tok.End)
		}
	}
	toks := Tokenize(text)
	if got := string(text[toks[1].Start:toks[1].End]); got != "کتاب‌خانهٔ" {
		t.Fatalf("the joiner split a word: %q", got)
	}
}

func TestSearchSeesOnlyWhatTheReaderMaySee(t *testing.T) {
	ix := New()
	texts := map[string]string{
		"a": "پژوهش ساختگی دربارهٔ کتابخانه و خانه.",
		"b": "نوشتهٔ خصوصی ساختگی: کتاب‌خانهٔ پنهان.",
		"c": "کتاب خانه جدا نوشته شده؛ کتابخانه هم هست.",
	}
	for _, id := range []string{"a", "b", "c"} {
		path := "research/" + id
		if id == "b" {
			path = "private/" + id
		}
		ix.Add(Doc{ID: id, Item: id, Path: path, Version: 1}, []byte(texts[id]))
	}
	visible := func(d Doc) bool { return strings.HasPrefix(d.Path, "research/") }
	hits := ix.Search("كتابخانه", visible, 1)
	if len(hits) != 1 || hits[0].Doc.ID != "a" {
		t.Fatalf("limit 1 over visible docs: %+v", hits)
	}
	all := ix.Search("کتابخانه", visible, 0)
	for _, h := range all {
		if h.Doc.ID == "b" {
			t.Fatal("an invisible document was matched")
		}
		for _, s := range h.Spans {
			if Key(texts[h.Doc.ID][s.Start:s.End]) != Key("کتابخانه") && Key(texts[h.Doc.ID][s.Start:s.End]) != Key("کتاب") && Key(texts[h.Doc.ID][s.Start:s.End]) != Key("خانه") {
				t.Fatalf("a span points at %q", texts[h.Doc.ID][s.Start:s.End])
			}
		}
	}
	if len(all) != 2 {
		t.Fatalf("visible hits: %d", len(all))
	}
	joined := ix.Search("کتاب خانه", visible, 0)
	if len(joined) != 2 {
		t.Fatalf("two words finding the joined word: %+v", joined)
	}
}

func TestSnippetsAndPassagesAreSlicesOfTheBytes(t *testing.T) {
	text := []byte("بند یکم ساختگی.\n\nبند دوم دربارهٔ کتاب‌خانه است و ادامه دارد.\n\nبند سوم.")
	toks := Tokenize(text)
	var span Span
	for _, tok := range toks {
		if tok.Key == Key("کتابخانه") {
			span = Span{tok.Start, tok.End}
		}
	}
	s, at := Snippet(text, span, 7)
	if !utf8.ValidString(s) || string(text[at.Start:at.End]) != s || !strings.Contains(s, "کتاب‌خانه") {
		t.Fatalf("snippet %q", s)
	}
	p, at := Passage(text, span, 0)
	if p != "بند دوم دربارهٔ کتاب‌خانه است و ادامه دارد." || string(text[at.Start:at.End]) != p {
		t.Fatalf("passage %q", p)
	}
}
