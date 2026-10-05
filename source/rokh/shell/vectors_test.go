package shell

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the sentence conformance vectors")

// Sentence conformance vectors.
//
// Every canonical sentence, parsed to its operation and slots, with the reply
// template it answers in. The file is UTF-8 with ZWNJ written as the JSON
// escape \u200c, so the file documents its own invisibles.
//
// The frame is English and the slots are not: what a person writes into a
// sentence is their content, and the surface's language has no claim on it.
// Several vectors carry Persian addresses and payloads for exactly that
// reason, and they are not left over from the Persian grammar — they are the
// point.
//
// Any other implementation of the sentence surface must map these sentences
// to these operations and slots.
type vector struct {
	Name     string `json:"name"`
	Sentence string `json:"sentence"`
	Op       string `json:"op"`
	Slot     struct {
		Name    string `json:"ledger,omitempty"`
		Address string `json:"address,omitempty"`
		Text    string `json:"text,omitempty"`
		Path    string `json:"path,omitempty"`
		Who     string `json:"who,omitempty"`
		ID      string `json:"id,omitempty"`
		N       int    `json:"n,omitempty"`
	} `json:"slots"`
	Reply string `json:"reply_template"`
}

// replyOf maps each operation to the template it answers with. Operations
// whose answer is free-form listing (read, see) are marked as such.
var replyOf = map[string]string{
	opOpen:         "opened",
	opOpenNew:      "opened_new",
	opClose:        "closed",
	opWrite:        "drafted",
	opWriteClose:   "written",
	opCancel:       "cancelled",
	opRead:         "listing",
	opSeeLedger:    "seen",
	opSeeLedgers:   "listing",
	opSeeGrants:    "listing",
	opImport:       "imported | import_failed",
	opReunite:      "reunited_ask | reunited_settled",
	opReconcile:    "merged",
	opEntrustWrite: "granted",
	opEntrustRead:  "shared",
	opTakeBack:     "revoked",
	opCarryLedger:  "copied",
	opCarryAddress: "exported",
	opCarryBundle:  "bundled",
}

func buildVectors() []vector {
	mk := func(name, sentence, op string, fill func(*vector)) vector {
		v := vector{Name: name, Sentence: sentence, Op: op, Reply: replyOf[op]}
		if fill != nil {
			fill(&v)
		}
		return v
	}
	full := strings.Repeat("ab12", 16) // a well-formed 64-hex id for the vector

	return []vector{
		mk("open", "open the ledger خانه", opOpen,
			func(v *vector) { v.Slot.Name = "خانه" }),
		// Case is the only thing the frame folds, and it folds it whole.
		mk("open, mixed case", "OpEn ThE LeDgEr خانه", opOpen,
			func(v *vector) { v.Slot.Name = "خانه" }),
		mk("open new", "open a new ledger named خانه", opOpenNew,
			func(v *vector) { v.Slot.Name = "خانه" }),
		mk("close", "leave", opClose, nil),
		mk("write", "write at کارها: نخستین سطر", opWrite,
			func(v *vector) { v.Slot.Address = "کارها"; v.Slot.Text = "نخستین سطر" }),
		mk("write zwnj payload", "write at کارها: می‌خواهیم نیم‌فاصله‌ها بمانند", opWrite,
			func(v *vector) {
				v.Slot.Address = "کارها"
				v.Slot.Text = "می‌خواهیم نیم‌فاصله‌ها بمانند"
			}),
		// The frame is shouted; the payload holds a DECOMPOSED alef-madda
		// (U+0627 U+0653) and an address in Arabic yeh, and both must survive
		// untouched. Folding is for keywords, and the keywords are the frame.
		mk("write folded frame, decomposed payload",
			"WRITE AT يادها: \u0627\u0653ب", opWrite,
			func(v *vector) { v.Slot.Address = "يادها"; v.Slot.Text = "\u0627\u0653ب" }),
		// A period is not frame in this grammar, so it belongs to whoever
		// typed it. Under the Persian sentences it was stripped as frame;
		// here it is content and it stays.
		mk("write keeps a final period", "write at کارها: تمام.", opWrite,
			func(v *vector) { v.Slot.Address = "کارها"; v.Slot.Text = "تمام." }),
		// The one act that records, and its numbered form; and the act that
		// lets a waiting sentence go.
		mk("write close", "write", opWriteClose, nil),
		mk("write close the second", "write 2", opWriteClose, func(v *vector) { v.Slot.N = 2 }),
		mk("cancel", "cancel", opCancel, nil),
		mk("cancel the first", "cancel 1", opCancel, func(v *vector) { v.Slot.N = 1 }),
		mk("read", "read کارها", opRead,
			func(v *vector) { v.Slot.Address = "کارها" }),
		mk("see ledger", "see the ledger", opSeeLedger, nil),
		mk("see ledgers", "see the ledgers", opSeeLedgers, nil),
		mk("see grants", "see the grants", opSeeGrants, nil),
		mk("import file", "bring /tmp/a b/note.md to کارها", opImport,
			func(v *vector) { v.Slot.Path = "/tmp/a b/note.md"; v.Slot.Address = "کارها" }),
		mk("reunite", "bring the returned ledger", opReunite, nil),
		mk("entrust write", "entrust writing at کارها to دستیار", opEntrustWrite,
			func(v *vector) { v.Slot.Address = "کارها"; v.Slot.Who = "دستیار" }),
		mk("entrust read", "entrust reading کارها to دستیار", opEntrustRead,
			func(v *vector) { v.Slot.Address = "کارها"; v.Slot.Who = "دستیار" }),
		mk("take back", "take back the grant "+full, opTakeBack,
			func(v *vector) { v.Slot.ID = full }),
		mk("carry ledger", "carry the ledger to /tmp/berth one", opCarryLedger,
			func(v *vector) { v.Slot.Path = "/tmp/berth one" }),
		mk("carry address", "carry کارها to /tmp/out", opCarryAddress,
			func(v *vector) { v.Slot.Address = "کارها"; v.Slot.Path = "/tmp/out" }),
		mk("carry bundle", "carry the bundle for دستیار", opCarryBundle,
			func(v *vector) { v.Slot.Who = "دستیار" }),
		mk("reconcile", "reconcile", opReconcile, nil),
	}
}

func vectorsPath() string { return filepath.Join("testdata", "sentences.json") }

// escapeZWNJ rewrites the raw ZWNJ bytes as the JSON escape, so the vector
// file shows its invisibles instead of hiding them.
func escapeZWNJ(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("\u200c"), []byte(`\u200c`))
	return bytes.ReplaceAll(b, []byte("\u0653"), []byte(`\u0653`))
}

// The shell of Rokh is a language, not a command set. Eight verbs, and a
// sentence you can read aloud and understand what happened — that is the
// measure of it. These vectors pin the meaning, not the spelling: the syntax
// may change or be translated; the eight meanings do not.
//
//	— T9, T9.1, T9.3, T9.4
func TestSentenceVectors(t *testing.T) {
	got := buildVectors()

	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		b, err := json.MarshalIndent(got, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorsPath(), append(escapeZWNJ(b), '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d sentence vectors", len(got))
	}

	raw, err := os.ReadFile(vectorsPath())
	if err != nil {
		t.Fatalf("vectors unreadable (regenerate with -update): %v", err)
	}
	var want []vector
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(got) {
		t.Fatalf("vector count changed: %d != %d", len(got), len(want))
	}

	// Every vector must parse to exactly its operation and slots.
	for _, w := range want {
		c, err := parse(w.Sentence)
		if err != nil {
			t.Errorf("%s: did not parse: %v", w.Name, err)
			continue
		}
		if c.Op != w.Op {
			t.Errorf("%s: op %q, want %q", w.Name, c.Op, w.Op)
		}
		check := func(field, gotV, wantV string) {
			if gotV != wantV {
				t.Errorf("%s: %s %q, want %q", w.Name, field, gotV, wantV)
			}
		}
		check("ledger", c.Name, w.Slot.Name)
		check("address", c.Address, w.Slot.Address)
		check("text", c.Text, w.Slot.Text)
		check("path", c.Path, w.Slot.Path)
		check("who", c.Who, w.Slot.Who)
		check("id", c.ID, w.Slot.ID)
		if _, ok := replyOf[c.Op]; !ok {
			t.Errorf("%s: op %q has no reply template", w.Name, c.Op)
		}
	}

	// The payload slot is byte-exact: the decomposed-payload vector must hold
	// the combining mark untouched even though its frame was folded.
	for _, w := range want {
		if w.Name == "write folded frame, decomposed payload" {
			if !strings.Contains(w.Slot.Text, "\u0653") {
				t.Fatal("the decomposed payload vector lost its combining mark")
			}
			c, _ := parse(w.Sentence)
			if !bytes.Equal([]byte(c.Text), []byte(w.Slot.Text)) {
				t.Fatal("the payload was not preserved byte for byte")
			}
		}
	}
}

// A line that is not a canonical sentence is refused, not guessed at.
func TestNonSentencesAreRefused(t *testing.T) {
	for _, bad := range []string{
		"", "hello", "دفتر", "بنویس چیزی", "دفترِ خانه بگشا",
		"در کارها بنویس", "چیزی را جایی ببر",
	} {
		if c, err := parse(bad); err == nil {
			t.Errorf("%q parsed as %s", bad, c.Op)
		}
	}
}
