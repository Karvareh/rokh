package sheet

import (
	"strings"
	"testing"
)

const (
	item       = "0123456789abcdef0123456789abcdef"
	ownerEvt   = "1111111111111111111111111111111111111111111111111111111111111111"
	programEvt = "2222222222222222222222222222222222222222222222222222222222222222"
	elsewhere  = "3333333333333333333333333333333333333333333333333333333333333333"
	sum        = "4444444444444444444444444444444444444444444444444444444444444444"
)

func ledger(id string) (EventFacts, bool) {
	switch id {
	case ownerEvt:
		return EventFacts{Accepted: true, ByOwner: true, Address: DeclarationAddress(item), Verb: DeclarationVerb}, true
	case programEvt:
		return EventFacts{Accepted: true, ByOwner: false, Address: DeclarationAddress(item), Verb: DeclarationVerb}, true
	case elsewhere:
		return EventFacts{Accepted: true, ByOwner: true, Address: DeclarationAddress("another-item"), Verb: DeclarationVerb}, true
	}
	return EventFacts{}, false
}

func imported() Sheet {
	return Sheet{
		Format: Format, Item: item, Version: 1, Revision: 1, Kind: "file", SHA256: sum, Size: 400,
		Name:       Name{Exact: "گفتگوی ساختگی.md", Seen: []string{"گفتگوی‌ساختگی.md"}},
		Origin:     Origin{Via: "import:synthetic", Source: "fixture/گفتگوی ساختگی.md", SourceSHA256: sum},
		Registrar:  Party{Kind: "consumer", ID: "cli-test", Name: "synthetic importer", Key: "abcd"},
		Authorship: Unknown,
	}
}

func TestAnImportIsRegisteredNotAuthored(t *testing.T) {
	s := imported()
	if err := s.Check(ledger); err != nil {
		t.Fatal(err)
	}
	// The importer cannot turn its own signature into the owner's word.
	s.Authorship = OwnerDeclared
	s.Declarations = []Declaration{{By: Party{Kind: "owner", ID: "cli-test"}, About: "whole",
		Author: "صاحب‌آزمایشیِ الف", Statement: "I wrote this", Event: programEvt}}
	if err := s.Check(ledger); err == nil || !strings.Contains(err.Error(), "not signed by the owner") {
		t.Fatalf("an importer's event passed as the owner's declaration: %v", err)
	}
	// Nor can a program declare for the owner by calling itself a program.
	s.Declarations = []Declaration{{By: Party{Kind: "consumer", ID: "cli-test"}, About: "whole",
		Author: "صاحب‌آزمایشیِ الف", Statement: "the owner wrote this", Evidence: "machine guess"}}
	if err := s.Check(ledger); err == nil {
		t.Fatal("a program's attribution became owner-declared")
	}
}

func TestTheOwnersRecordedWordDeclaresAuthorship(t *testing.T) {
	s := imported()
	s.Revision, s.Supersedes = 2, "sheet-revision-1"
	s.Authorship = OwnerDeclared
	s.Declarations = []Declaration{{By: Party{Kind: "owner", ID: "root"}, About: "whole",
		Author: "صاحب‌آزمایشیِ الف", Statement: "این متن را خودم نوشته‌ام.", Event: ownerEvt}}
	if err := s.Check(ledger); err != nil {
		t.Fatal(err)
	}
	s.Declarations[0].Event = elsewhere
	if err := s.Check(ledger); err == nil {
		t.Fatal("a declaration about another item was accepted")
	}
	s.Declarations[0].Event = ownerEvt
	s.Supersedes = ""
	if err := s.Check(ledger); err == nil {
		t.Fatal("a correction that names nothing it supersedes was accepted")
	}
}

func TestAMixedFileKeepsItsBoundaries(t *testing.T) {
	s := imported()
	s.Authorship = Mixed
	s.Declarations = []Declaration{{By: Party{Kind: "owner", ID: "root"}, About: "segments",
		Author: "صاحب‌آزمایشیِ الف", Statement: "بند نخست از من است.", Event: ownerEvt}}
	s.Segments = []Segment{
		{Start: 0, End: 100, Status: OwnerDeclared, Speaker: "user", Author: "صاحب‌آزمایشیِ الف", Evidence: ownerEvt},
		{Start: 100, End: 250, Status: Quoted, Speaker: "machine"},
		{Start: 250, End: 300, Status: Unknown, Speaker: "unknown"},
		{Start: 300, End: 400, Status: Quoted, Speaker: "user", Author: "machine"},
	}
	if err := s.Check(ledger); err != nil {
		t.Fatal(err)
	}
	bad := s
	bad.Segments = append([]Segment(nil), s.Segments...)
	bad.Segments[1].Start = 90
	if err := bad.Check(ledger); err == nil {
		t.Fatal("overlapping segments were accepted")
	}
	bad.Segments = append([]Segment(nil), s.Segments...)
	bad.Segments[3].End = 401
	if err := bad.Check(ledger); err == nil {
		t.Fatal("a segment past the end was accepted")
	}
	bad.Segments = append([]Segment(nil), s.Segments...)
	bad.Segments[0].Evidence = programEvt
	if err := bad.Check(ledger); err == nil {
		t.Fatal("a segment attributed to the owner on a program's signature was accepted")
	}
	one := s
	one.Segments = []Segment{{Start: 0, End: 400, Status: Unknown}}
	if err := one.Check(ledger); err == nil {
		t.Fatal("mixed with a single attribution was accepted")
	}
}

func TestUnknownStaysUnknown(t *testing.T) {
	s := imported()
	s.Segments = []Segment{{Start: 0, End: 400, Status: Attributed, Author: "someone", Evidence: "a guess"}}
	if err := s.Check(ledger); err == nil {
		t.Fatal("an unknown version carried an attributed segment")
	}
}
