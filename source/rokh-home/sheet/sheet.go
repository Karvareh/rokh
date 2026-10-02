// Package sheet is a provenance sheet: for one version of one item, who wrote
// the text, who recorded it, where it came from, what it is called and what it
// belongs to — each kept apart from the others.
//
// The registrar is whoever brought the bytes in and signed their recording: a
// program or the owner. The author is who wrote the text, and that is known only
// from a declaration with its evidence — the owner's own recorded word, or an
// attribution that names what it rests on. A registrar's signature is never an
// authorship claim, and a guess is never a declaration. What nobody has declared
// stays unknown. A mixed file carries its attributions segment by segment, and a
// message a person sent may be a quotation rather than their own writing.
//
// A correction is a new revision of the sheet that names the revision it
// supersedes. The superseded revision, the bytes and the ledger's history stay
// exactly as they were.
package sheet

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
)

// Format names the sheet's encoding.
const Format = "rokh-home.sheet/1"

// Authorship of the whole version.
const (
	Unknown       = "unknown"
	OwnerDeclared = "owner-declared"
	Attributed    = "attributed"
	Mixed         = "mixed"
)

// Quoted is a segment's state when it is someone's text quoted as such.
const Quoted = "quoted"

// DeclarationVerb is the verb an owner's authorship declaration is recorded
// under, at DeclarationAddress of the item.
const DeclarationVerb = "home.author"

// DeclarationAddress is where declarations about an item are recorded.
func DeclarationAddress(item string) string { return "home/items/" + item }

// Party is someone a sheet names.
type Party struct {
	Kind string `json:"kind"` // owner | consumer | peer
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Key  string `json:"key,omitempty"`
}

// Declaration is one statement of authorship and what it rests on.
type Declaration struct {
	By        Party  `json:"by"`
	About     string `json:"about"` // whole | segments
	Author    string `json:"author"`
	Statement string `json:"statement"`
	// Event is the recorded event that carries an owner's declaration.
	Event string `json:"event,omitempty"`
	// Evidence is what an attribution rests on.
	Evidence string `json:"evidence,omitempty"`
}

// Segment is a byte range of the version with its own attribution.
type Segment struct {
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
	Status   string `json:"status"`            // owner-declared | attributed | quoted | unknown
	Speaker  string `json:"speaker,omitempty"` // the role it had where it came from: user | machine | unknown
	Author   string `json:"author,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

// Name is what the item is called: the exact name, and the other names it has
// been seen under, each exactly as seen.
type Name struct {
	Exact string   `json:"exact"`
	Seen  []string `json:"seen,omitempty"`
}

// Origin is where the bytes came from.
type Origin struct {
	Via          string `json:"via"`
	Source       string `json:"source,omitempty"`
	SourceSHA256 string `json:"source_sha256,omitempty"`
	Start        int64  `json:"start,omitempty"`
	End          int64  `json:"end,omitempty"`
}

// Sheet is one revision of one version's provenance.
type Sheet struct {
	Format       string        `json:"format"`
	Item         string        `json:"item"`
	Version      int           `json:"version"`
	Revision     int           `json:"revision"`
	Supersedes   string        `json:"supersedes,omitempty"`
	Kind         string        `json:"kind"`
	SHA256       string        `json:"sha256"`
	Size         int64         `json:"size"`
	Name         Name          `json:"name"`
	Context      string        `json:"context,omitempty"`
	Origin       Origin        `json:"origin"`
	Registrar    Party         `json:"registrar"`
	Authorship   string        `json:"authorship"`
	Declarations []Declaration `json:"declarations,omitempty"`
	Segments     []Segment     `json:"segments,omitempty"`
	Previous     int           `json:"previous_version,omitempty"`
}

// EventFacts is what the ledger says about an event a sheet cites.
type EventFacts struct {
	Accepted bool
	ByOwner  bool
	Address  string
	Verb     string
}

// Ledger answers for the events a sheet cites.
type Ledger func(id string) (EventFacts, bool)

func hex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == s
}

// ownerEvent says whether an event is the owner's recorded declaration about
// this item.
func ownerEvent(led Ledger, item, id string) error {
	if !hex64(id) {
		return fmt.Errorf("sheet: %q does not name an event", id)
	}
	f, found := led(id)
	switch {
	case !found || !f.Accepted:
		return fmt.Errorf("sheet: the ledger holds no accepted event %s", id)
	case !f.ByOwner:
		return fmt.Errorf("sheet: event %s is not signed by the owner; a registrar's or a program's signature is not the owner's word", id)
	case f.Address != DeclarationAddress(item) || f.Verb != DeclarationVerb:
		return fmt.Errorf("sheet: event %s is not a declaration about this item", id)
	}
	return nil
}

// Check validates a sheet against itself and against the ledger for every
// owner's declaration it cites.
func (s Sheet) Check(led Ledger) error {
	switch {
	case s.Format != Format:
		return fmt.Errorf("sheet: unknown format %q", s.Format)
	case s.Item == "" || s.Version < 1 || s.Revision < 1:
		return errors.New("sheet: item, version and revision are required")
	case s.Revision > 1 && s.Supersedes == "":
		return errors.New("sheet: a later revision names the revision it supersedes")
	case s.Kind != "file" && s.Kind != "tree":
		return fmt.Errorf("sheet: unknown kind %q", s.Kind)
	case !hex64(s.SHA256) || s.Size < 0:
		return errors.New("sheet: a version is named by its SHA-256 and has a size")
	case s.Name.Exact == "":
		return errors.New("sheet: the exact name is required")
	case s.Origin.Via == "":
		return errors.New("sheet: the origin says how the bytes came in")
	case s.Registrar.Kind != "owner" && s.Registrar.Kind != "consumer":
		return fmt.Errorf("sheet: a registrar is the owner or a program, not %q", s.Registrar.Kind)
	case s.Registrar.ID == "":
		return errors.New("sheet: the registrar is named")
	}
	wholeOwner, wholeAttributed := false, false
	for i, d := range s.Declarations {
		if d.Author == "" || (d.About != "whole" && d.About != "segments") {
			return fmt.Errorf("sheet: declaration %d names no author or no extent", i)
		}
		switch d.By.Kind {
		case "owner":
			if err := ownerEvent(led, s.Item, d.Event); err != nil {
				return fmt.Errorf("sheet: declaration %d: %w", i, err)
			}
			if d.About == "whole" {
				wholeOwner = true
			}
		case "consumer", "peer":
			// A program or a peer attributes; it does not declare for the owner.
			if d.Evidence == "" {
				return fmt.Errorf("sheet: attribution %d rests on nothing", i)
			}
			if d.About == "whole" {
				wholeAttributed = true
			}
		default:
			return fmt.Errorf("sheet: declaration %d is by %q", i, d.By.Kind)
		}
	}
	if err := s.checkSegments(led); err != nil {
		return err
	}
	switch s.Authorship {
	case Unknown:
		if wholeOwner || wholeAttributed {
			return errors.New("sheet: a declared author is not unknown")
		}
		for _, g := range s.Segments {
			if g.Status == OwnerDeclared || g.Status == Attributed {
				return errors.New("sheet: an unknown version has no attributed segment; that is mixed")
			}
		}
	case OwnerDeclared:
		if !wholeOwner {
			return errors.New("sheet: owner-declared needs the owner's recorded declaration about the whole version")
		}
	case Attributed:
		if !wholeAttributed && !wholeOwner {
			return errors.New("sheet: attributed needs an attribution about the whole version with its evidence")
		}
	case Mixed:
		states := map[string]bool{}
		for _, g := range s.Segments {
			states[g.Status] = true
		}
		if len(s.Segments) < 2 || len(states) < 2 {
			return errors.New("sheet: mixed needs segments with different attributions")
		}
	default:
		return fmt.Errorf("sheet: unknown authorship %q", s.Authorship)
	}
	return nil
}

func (s Sheet) checkSegments(led Ledger) error {
	segs := append([]Segment(nil), s.Segments...)
	sort.SliceStable(segs, func(i, j int) bool { return segs[i].Start < segs[j].Start })
	var end int64
	for i, g := range segs {
		if g.Start < end || g.Start >= g.End || g.End > s.Size {
			return fmt.Errorf("sheet: segment %d [%d,%d) overlaps, is empty or leaves the version", i, g.Start, g.End)
		}
		end = g.End
		switch g.Status {
		case OwnerDeclared:
			if err := ownerEvent(led, s.Item, g.Evidence); err != nil {
				return fmt.Errorf("sheet: segment %d: %w", i, err)
			}
		case Attributed:
			if g.Author == "" || g.Evidence == "" {
				return fmt.Errorf("sheet: attributed segment %d names no author or no evidence", i)
			}
		case Quoted:
			if g.Speaker == "" && g.Author == "" {
				return fmt.Errorf("sheet: quoted segment %d says nothing of whose text it is", i)
			}
		case Unknown:
		default:
			return fmt.Errorf("sheet: segment %d has unknown status %q", i, g.Status)
		}
	}
	return nil
}
