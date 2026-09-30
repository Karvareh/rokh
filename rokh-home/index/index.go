// Package index is a home's lexical index: a reading layer over exact bytes.
//
// It folds what Persian writing varies in — Arabic and Persian yeh and kaf, the
// forms of heh, hamza carriers, harakat, tatweel, the zero-width non-joiner and
// joiner, Persian and Arabic digits, Latin case — so a search finds a word
// however it was typed. The fold is applied only to the key a word is looked up
// by. Every hit points at the exact bytes at their exact offsets, and a snippet
// is a slice of those bytes, never a rewritten copy; nothing here changes a text
// or a name.
//
// Visibility is decided before anything is counted: a document a reader may not
// see is never matched, never numbered and never allowed to use up a limit.
//
// The index is rebuilt from what it indexes, and a home keeps it sealed like
// everything else.
package index

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Format names the index's encoding.
const Format = "rokh-home.index/1"

// fold maps one rune to its search form; ok=false drops it from the key.
func fold(r rune) (rune, bool) {
	switch r {
	case 'ي', 'ى', 'ئ':
		return 'ی', true
	case 'ك':
		return 'ک', true
	case 'ة', 'ۀ', 'ە':
		return 'ه', true
	case 'أ', 'إ', 'آ', 'ٱ':
		return 'ا', true
	case 'ؤ':
		return 'و', true
	case '‌', '‍', '‎', '‏', 'ـ':
		return 0, false
	}
	switch {
	case r >= 'ً' && r <= 'ٟ', r == 'ٰ':
		return 0, false
	case r >= '۰' && r <= '۹':
		return '0' + (r - '۰'), true
	case r >= '٠' && r <= '٩':
		return '0' + (r - '٠'), true
	case r >= '̀' && r <= 'ͯ':
		return 0, false
	}
	return unicode.ToLower(r), true
}

// Key is the search form of a word.
func Key(word string) string {
	var b strings.Builder
	for _, r := range word {
		if f, ok := fold(r); ok {
			b.WriteRune(f)
		}
	}
	return b.String()
}

func inWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) ||
		r == '‌' || r == '‍' || r == 'ـ'
}

// Token is a word at its exact place in the text.
type Token struct {
	Key   string
	Start int
	End   int
}

// Tokenize finds the words of a text with their byte offsets. A zero-width
// non-joiner is inside a word, as it is when a person reads one.
func Tokenize(text []byte) []Token {
	var out []Token
	start := -1
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRune(text[i:])
		if inWord(r) && r != utf8.RuneError {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			if k := Key(string(text[start:i])); k != "" {
				out = append(out, Token{Key: k, Start: start, End: i})
			}
			start = -1
		}
		i += size
	}
	if start >= 0 {
		if k := Key(string(text[start:])); k != "" {
			out = append(out, Token{Key: k, Start: start, End: len(text)})
		}
	}
	return out
}

// Doc is one indexed text: a version of an item, or a text inside a tree.
type Doc struct {
	ID      string `json:"id"`
	Item    string `json:"item"`
	Path    string `json:"path"`
	Version int    `json:"version"`
	Sub     string `json:"sub,omitempty"`
}

// Posting is one occurrence of a key.
type Posting struct {
	Doc   int `json:"d"`
	Start int `json:"s"`
	End   int `json:"e"`
}

// Index is keys to occurrences.
type Index struct {
	Format string               `json:"format"`
	Docs   []Doc                `json:"docs"`
	Terms  map[string][]Posting `json:"terms"`
}

// New is an empty index.
func New() *Index { return &Index{Format: Format, Terms: map[string][]Posting{}} }

// Add indexes one text.
func (ix *Index) Add(d Doc, text []byte) {
	n := len(ix.Docs)
	ix.Docs = append(ix.Docs, d)
	for _, t := range Tokenize(text) {
		ix.Terms[t.Key] = append(ix.Terms[t.Key], Posting{Doc: n, Start: t.Start, End: t.End})
	}
}

// Span is a byte range in a document's exact bytes.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Hit is a visible document that holds the query, with where.
type Hit struct {
	Doc   Doc    `json:"doc"`
	Spans []Span `json:"spans"`
}

// Search finds the visible documents that hold every word of the query — or,
// for a query of several words, the words written together as one, since a
// space and a joiner are the same choice made differently. The limit counts
// only what the reader may see.
func (ix *Index) Search(query string, visible func(Doc) bool, limit int) []Hit {
	var keys []string
	for _, t := range Tokenize([]byte(query)) {
		keys = append(keys, t.Key)
	}
	if len(keys) == 0 {
		return nil
	}
	spans := map[int][]Span{}
	all := func(ks []string) map[int][]Span {
		found := map[int][]Span{}
		for i, k := range ks {
			seen := map[int][]Span{}
			for _, p := range ix.Terms[k] {
				seen[p.Doc] = append(seen[p.Doc], Span{p.Start, p.End})
			}
			if i == 0 {
				found = seen
				continue
			}
			for d := range found {
				if s, ok := seen[d]; ok {
					found[d] = append(found[d], s...)
				} else {
					delete(found, d)
				}
			}
		}
		return found
	}
	for d, s := range all(keys) {
		spans[d] = s
	}
	if len(keys) > 1 {
		for d, s := range all([]string{strings.Join(keys, "")}) {
			spans[d] = append(spans[d], s...)
		}
	}
	var docs []int
	for d := range spans {
		if visible == nil || visible(ix.Docs[d]) {
			docs = append(docs, d)
		}
	}
	sort.Slice(docs, func(i, j int) bool {
		a, b := ix.Docs[docs[i]], ix.Docs[docs[j]]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Version != b.Version {
			return a.Version > b.Version
		}
		return a.Sub < b.Sub
	})
	var hits []Hit
	for _, d := range docs {
		if limit > 0 && len(hits) == limit {
			break
		}
		s := spans[d]
		sort.Slice(s, func(i, j int) bool { return s[i].Start < s[j].Start })
		hits = append(hits, Hit{Doc: ix.Docs[d], Spans: s})
	}
	return hits
}

// Snippet is a slice of the exact bytes around a span, widened by radius bytes
// and trimmed to whole characters.
func Snippet(text []byte, span Span, radius int) (string, Span) {
	start, end := span.Start-radius, span.End+radius
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	for start > 0 && start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	for end < len(text) && end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return string(text[start:end]), Span{start, end}
}

// Passage is the paragraph around a span, at most max bytes, as exact bytes.
func Passage(text []byte, span Span, max int) (string, Span) {
	start := strings.LastIndex(string(text[:span.Start]), "\n\n")
	if start < 0 {
		start = 0
	} else {
		start += 2
	}
	end := strings.Index(string(text[span.End:]), "\n\n")
	if end < 0 {
		end = len(text)
	} else {
		end += span.End
	}
	if max > 0 && end-start > max {
		return Snippet(text, span, (max-(span.End-span.Start))/2)
	}
	return string(text[start:end]), Span{start, end}
}
