// Package event builds, reads and checks events.
//
// An event answers six questions and nothing else:
//
//	who?           author     — public key of the signer
//	by what right? authority  — id of the grant that conferred it
//	after what?    parents    — causal predecessors
//	about what?    address    — anything addressable
//	saying what?   verb + payload
//	witnessed how? attest     — oracle testimony
//
// What is deliberately not a field, and why that is a ruling:
//
//   - No timestamp field. Time is testimony (a clock oracle), not a column.
//     Because it is not a field, no implementation can order by it even if
//     it wanted to.
//   - No local counter. Causality comes from parents. Two writers sharing a
//     key would make a counter meaningless; they cannot make a parent
//     meaningless.
//   - No owner field. The carrier binding and the authority chain both
//     terminate at genesis on their own.
//   - No visibility policy field. Encryption at rest belongs to the carrier;
//     selective sharing belongs to a later layer.
package event

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"rokh/frame"
)

// Event field tags.
const (
	TagCarrier   frame.Tag = 0x0001 // 32 bytes, ledger anchor. Absent iff genesis
	TagAuthor    frame.Tag = 0x0002 // 32 bytes, Ed25519 public key
	TagAuthority frame.Tag = 0x0003 // 32 bytes, id of a grant. Absent implies author is root
	TagParents   frame.Tag = 0x0004 // 32*n, ascending, no duplicates. Absent iff none
	TagAddress   frame.Tag = 0x0005 // UTF-8
	TagVerb      frame.Tag = 0x0006 // UTF-8
	TagPayload   frame.Tag = 0x0007 // bytes. Absent iff empty
	TagAttest    frame.Tag = 0x0008 // attestations. Absent iff empty
	TagFresh     frame.Tag = 0x0009 // FreshSize bytes, never absent
	TagBody      frame.Tag = 0x000A // head: 32 bytes, SHA-256 of the body bytes
	TagSystem    frame.Tag = 0x000B // head: 0x01 when the address is "rokh"; otherwise absent
	TagSalt      frame.Tag = 0x000C // body: SaltSize random bytes, always present
)

// Which side of the event each field lives on (contract 3.4). The head is
// signed, hashed and stored; it names the body by hash. The head proves
// lineage and authorship; only the body says what was done.
var headTags = map[frame.Tag]bool{TagCarrier: true, TagAuthor: true, TagAuthority: true,
	TagParents: true, TagFresh: true, TagBody: true, TagSystem: true}

// Limits, all derived from one question: an event must fit through the
// narrowest link we intend to support.
//
// The inline payload limit is deliberately small. Rokh is not a content
// store; large content stays outside and the event carries only its hash and
// descriptor. The previous 64 KiB limit left that boundary open and was
// exactly what made the low-bandwidth path unusable.
//
// The maximum-size arithmetic is in docs/01-grammar.md and adds up to
// frame.MaxFrame.
const (
	MaxAddress   = 255 // whole address
	MaxComponent = 64  // one address component
	MaxVerb      = 64  //
	// The payload ceiling is small so the ledger passes through the narrowest
	// road. The number is a choice of the design, not a limit of the world.
	//
	//	— T3.4
	MaxPayload    = 4096 // inline payload: a generous note, not a file
	MaxParents    = 16   // merging sixteen branches is already excessive
	MaxAttest     = 8    // testimony is a stamp, not cargo
	MaxOracleName = 32   //
	MaxClaim      = 256  //
)

// Reserved verbs. Any verb beginning with "rokh." belongs to the core; apart
// from these four, a reserved verb is unknown and rejected.
const (
	ReservedPrefix = "rokh."

	VerbGenesis = "rokh.genesis" // birth of a ledger
	VerbGrant   = "rokh.grant"   // confer a bounded right
	VerbRevoke  = "rokh.revoke"  // withdraw a right, from here on, not in the past
	VerbMerge   = "rokh.merge"   // join branches; carries no payload, claims nothing
	VerbKeyring = "rokh.keyring" // add or revoke one generation of a key (contract 4.2)
	VerbSeed    = "rokh.seed"    // give or take a seed (contract 4.7)
)

// AddressRoot is the address space reserved for the core.
const AddressRoot = "rokh"

func IsReserved(v string) bool { return strings.HasPrefix(v, ReservedPrefix) }

func knownReserved(v string) bool {
	switch v {
	case VerbGenesis, VerbGrant, VerbRevoke, VerbMerge, VerbKeyring, VerbSeed:
		return true
	}
	return false
}

var (
	ErrShape   = errors.New("event: bad shape")
	ErrName    = errors.New("event: bad name")
	ErrSig     = errors.New("event: signature does not verify")
	ErrUnknown = errors.New("event: unknown field")
	// ErrBody is a body that does not hash to the value its signed head
	// names. It is a forgery, and nothing of it is read (contract E1).
	ErrBody = errors.New("event: the body does not hash to the value its head names")
)

// Attestation is what an oracle reported at the moment the event was made.
//
// The hard line: the signature proves the author *said* the oracle reported
// this. It does not prove the report is true. Testimony is not verification.
type Attestation struct {
	Oracle string
	Claim  []byte
}

// Event is an unsigned event.
// An event is a recorded happening: what happened, at what address, by
// whose authority, and with what attestation. Every event names its
// parents, and "earlier" means nothing else.
//
//	— T1.1, T5.1, N4.3
//
// FreshSize is how many bytes of freshness an event carries, in the base
// profile. Four is chosen against the population it actually has to separate
// — byte-identical events written by one author at one place — not against
// the whole ledger.
//
//	— T3.6
const FreshSize = 4

// SaltSize is the size of the body's salt: 32 random bytes that make the
// body's hash, which the head carries, hide the body (contract 3.4).
const SaltSize = 32

type Event struct {
	Carrier   *frame.ID // nil iff genesis
	Author    ed25519.PublicKey
	Authority *frame.ID // nil implies author must be root
	Parents   []frame.ID
	Address   string
	Verb      string
	Payload   []byte
	Attest    []Attestation
	// Fresh is this event's own freshness: a random string drawn from a
	// source whose output is unpredictable and independent of every earlier
	// event. Without it, two identical writes from one hand become one
	// event, and "twice, separately" stops being distinguishable from "once,
	// repeated".
	//
	// It shrinks that chance; it does not remove it. Two writers who cannot
	// see each other can still draw the same bytes, and after they meet
	// nobody can tell there were two acts.
	//
	//	— T3.6
	Fresh [FreshSize]byte
	// Salt is the body's own randomness, so that the hash of the body in the
	// head says nothing about the body to someone who cannot open it.
	Salt [SaltSize]byte
}

// Signed is a signed, parsed event.
//
// Head is the signed head and ID is its hash. Body is the body the head names
// by hash, or nil for a head only. Raw is head || body, exactly what a store
// keeps for a whole event; for a head only it is the head.
//
// A head only carries the head's fields and nothing else: Address, Verb,
// Payload and Attest are empty, and HeadOnly says so. It proves lineage and
// authorship and nothing of content.
type Signed struct {
	ID       frame.ID
	Raw      []byte
	Head     []byte
	Body     []byte
	BodyHash [32]byte
	// System is the head's own mark: the event sits at the address "rokh".
	System   bool
	HeadOnly bool
	Event    Event
}

// WithoutBody returns this event without its body: a head only.
func (s Signed) WithoutBody() Signed {
	out := s
	out.Raw, out.Body, out.HeadOnly = s.Head, nil, true
	out.Event.Address, out.Event.Verb, out.Event.Payload, out.Event.Attest = "", "", nil, nil
	out.Event.Salt = [SaltSize]byte{}
	return out
}

func (s Signed) IsGenesis() bool { return s.Event.Carrier == nil }

// ---------- name rules ----------

// badRune rejects runes that are either invisible or that write one meaning
// in two different byte sequences.
//
// Why reject rather than normalize: full Unicode normalization needs large
// tables and a dependency-free core cannot carry them. So instead of
// *repairing* ambiguity we *refuse* it. With no combining marks permitted,
// the decomposed form of a character simply cannot be expressed, leaving
// exactly one possible encoding. A macOS box writing NFD and a Linux box
// writing NFC arrive at the same bytes without a byte of tables.
//
// Format and bidirectional controls are refused too: an address that used a
// right-to-left override could display as something other than what it is.
func badRune(r rune) bool {
	switch {
	case r == utf8.RuneError:
		return true
	case r < 0x20 || r == 0x7F: // ASCII controls
		return true
	case r >= 0x80 && r <= 0x9F: // C1 controls
		return true
	case r >= 0x0300 && r <= 0x036F: // combining diacritics
		return true
	case r >= 0x0483 && r <= 0x0489:
		return true
	case r >= 0x0591 && r <= 0x05BD, r == 0x05BF, r == 0x05C1, r == 0x05C2,
		r == 0x05C4, r == 0x05C5, r == 0x05C7:
		return true
	case r >= 0x0610 && r <= 0x061A:
		return true
	case r >= 0x064B && r <= 0x065F: // Arabic marks, including hamza above/below
		return true
	case r == 0x0670:
		return true
	case r >= 0x06D6 && r <= 0x06DC, r >= 0x06DF && r <= 0x06E4,
		r == 0x06E7, r == 0x06E8, r >= 0x06EA && r <= 0x06ED:
		return true
	case r == 0x0711, r >= 0x0730 && r <= 0x074A:
		return true
	case r >= 0x1AB0 && r <= 0x1AFF, r >= 0x1DC0 && r <= 0x1DFF:
		return true
	case r >= 0x20D0 && r <= 0x20F0:
		return true
	case r >= 0x200B && r <= 0x200F: // ZWSP, ZWNJ, ZWJ, LRM, RLM
		return true
	case r >= 0x202A && r <= 0x202E: // bidi embedding and override
		return true
	case r >= 0x2060 && r <= 0x2064, r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0xFEFF:
		return true
	case r >= 0xFE00 && r <= 0xFE0F, r >= 0xFE20 && r <= 0xFE2F:
		return true
	case r >= 0xE0000 && r <= 0xE007F:
		return true
	}
	return false
}

func validText(s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%w: invalid UTF-8", ErrName)
	}
	for _, r := range s {
		if badRune(r) {
			return fmt.Errorf("%w: disallowed rune U+%04X", ErrName, r)
		}
	}
	return nil
}

// ValidAddress checks an address.
//
// Addresses are hierarchical, separated by "/". Because the ledger is
// content-addressed, "/" never reaches the filesystem, so the old problem of
// a path component containing a separator simply does not arise.
func ValidAddress(a string) error {
	if len(a) == 0 || len(a) > MaxAddress {
		return fmt.Errorf("%w: address length (%d)", ErrName, len(a))
	}
	if err := validText(a); err != nil {
		return err
	}
	for _, part := range strings.Split(a, "/") {
		if len(part) == 0 {
			return fmt.Errorf("%w: empty address component", ErrName)
		}
		if len(part) > MaxComponent {
			return fmt.Errorf("%w: address component too long (%d)", ErrName, len(part))
		}
		if part == "." || part == ".." {
			return fmt.Errorf("%w: address component %q", ErrName, part)
		}
		if strings.HasPrefix(part, " ") || strings.HasSuffix(part, " ") {
			return fmt.Errorf("%w: leading or trailing space in address component", ErrName)
		}
	}
	return nil
}

// ValidVerb checks a verb.
func ValidVerb(v string) error {
	if len(v) == 0 || len(v) > MaxVerb {
		return fmt.Errorf("%w: verb length (%d)", ErrName, len(v))
	}
	if err := validText(v); err != nil {
		return err
	}
	if strings.Contains(v, "/") {
		return fmt.Errorf("%w: \"/\" in verb", ErrName)
	}
	if IsReserved(v) && !knownReserved(v) {
		return fmt.Errorf("%w: unknown reserved verb %q", ErrName, v)
	}
	return nil
}

// ---------- construction ----------

func normParents(ps []frame.ID) ([]frame.ID, error) {
	if len(ps) == 0 {
		return nil, nil
	}
	if len(ps) > MaxParents {
		return nil, fmt.Errorf("%w: too many parents (%d)", ErrShape, len(ps))
	}
	cp := make([]frame.ID, len(ps))
	copy(cp, ps)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Compare(cp[j]) < 0 })
	for i := 1; i < len(cp); i++ {
		if cp[i-1] == cp[i] {
			return nil, fmt.Errorf("%w: duplicate parent", ErrShape)
		}
	}
	return cp, nil
}

func normAttest(as []Attestation) ([]Attestation, error) {
	if len(as) == 0 {
		return nil, nil
	}
	if len(as) > MaxAttest {
		return nil, fmt.Errorf("%w: too many attestations (%d)", ErrShape, len(as))
	}
	cp := make([]Attestation, len(as))
	copy(cp, as)
	for _, a := range cp {
		if len(a.Oracle) == 0 || len(a.Oracle) > MaxOracleName {
			return nil, fmt.Errorf("%w: oracle name length (%d)", ErrShape, len(a.Oracle))
		}
		if err := validText(a.Oracle); err != nil {
			return nil, err
		}
		if len(a.Claim) == 0 || len(a.Claim) > MaxClaim {
			return nil, fmt.Errorf("%w: claim size (%d)", ErrShape, len(a.Claim))
		}
	}
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].Oracle != cp[j].Oracle {
			return cp[i].Oracle < cp[j].Oracle
		}
		return bytes.Compare(cp[i].Claim, cp[j].Claim) < 0
	})
	for i := 1; i < len(cp); i++ {
		if cp[i-1].Oracle == cp[i].Oracle && bytes.Equal(cp[i-1].Claim, cp[i].Claim) {
			return nil, fmt.Errorf("%w: duplicate attestation", ErrShape)
		}
	}
	return cp, nil
}

func encodeAttest(as []Attestation) []byte {
	var out []byte
	var l4 [4]byte
	for _, a := range as {
		out = append(out, byte(len(a.Oracle)))
		out = append(out, a.Oracle...)
		binary.BigEndian.PutUint32(l4[:], uint32(len(a.Claim)))
		out = append(out, l4[:]...)
		out = append(out, a.Claim...)
	}
	return out
}

func decodeAttest(b []byte) ([]Attestation, error) {
	var out []Attestation
	for len(b) > 0 {
		n := int(b[0])
		b = b[1:]
		if n == 0 || len(b) < n {
			return nil, fmt.Errorf("%w: truncated oracle name", ErrShape)
		}
		name := string(b[:n])
		b = b[n:]
		if len(b) < 4 {
			return nil, fmt.Errorf("%w: truncated attestation", ErrShape)
		}
		cl := binary.BigEndian.Uint32(b[:4])
		b = b[4:]
		if cl == 0 || uint64(len(b)) < uint64(cl) {
			return nil, fmt.Errorf("%w: truncated claim", ErrShape)
		}
		claim := make([]byte, cl)
		copy(claim, b[:cl])
		b = b[cl:]
		out = append(out, Attestation{Oracle: name, Claim: claim})
		if len(out) > MaxAttest {
			return nil, fmt.Errorf("%w: too many attestations", ErrShape)
		}
	}
	return out, nil
}

// check validates the logical shape before a single byte is produced.
func (e *Event) check() error {
	if len(e.Author) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: author key size (%d)", ErrShape, len(e.Author))
	}
	if err := ValidAddress(e.Address); err != nil {
		return err
	}
	if err := ValidVerb(e.Verb); err != nil {
		return err
	}
	if len(e.Payload) > MaxPayload {
		return fmt.Errorf("%w: payload over limit (%d)", ErrShape, len(e.Payload))
	}

	// The address "rokh" and everything beneath it is core territory, in
	// both directions: a reserved verb sits nowhere else, and a user verb
	// never sits here. Without this boundary, core and user events would mix
	// on one address.
	inRoot := e.Address == AddressRoot || strings.HasPrefix(e.Address, AddressRoot+"/")
	if IsReserved(e.Verb) && e.Address != AddressRoot {
		return fmt.Errorf("%w: reserved verb must sit at address %q", ErrShape, AddressRoot)
	}
	if !IsReserved(e.Verb) && inRoot {
		return fmt.Errorf("%w: address %q is core territory", ErrShape, AddressRoot)
	}
	if err := e.checkHead(e.Address == AddressRoot); err != nil {
		return err
	}
	if e.Verb == VerbGenesis && e.Carrier != nil {
		return fmt.Errorf("%w: a second genesis is not allowed", ErrShape)
	}
	if e.Carrier == nil && e.Verb != VerbGenesis {
		return fmt.Errorf("%w: an event without a carrier must be genesis", ErrShape)
	}
	if e.Verb == VerbMerge {
		if len(e.Parents) < 2 {
			return fmt.Errorf("%w: merge with fewer than two parents", ErrShape)
		}
		// A merge claims nothing, so it carries nothing.
		if len(e.Payload) != 0 {
			return fmt.Errorf("%w: merge carries no payload", ErrShape)
		}
	}
	switch e.Verb {
	case VerbGrant:
		if _, err := DecodeGrant(e.Payload); err != nil {
			return err
		}
	case VerbRevoke:
		if _, err := DecodeRevoke(e.Payload); err != nil {
			return err
		}
	case VerbKeyring:
		if _, err := DecodeKeyring(e.Payload); err != nil {
			return err
		}
	case VerbSeed:
		if _, err := DecodeSeed(e.Payload); err != nil {
			return err
		}
	}
	return nil
}

// checkHead validates what a head alone can say: genesis has no carrier, no
// authority and no parents and is a system event; every other event has a
// carrier and at least one parent.
func (e *Event) checkHead(system bool) error {
	if len(e.Author) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: author key size (%d)", ErrShape, len(e.Author))
	}
	if e.Carrier == nil {
		if e.Authority != nil {
			return fmt.Errorf("%w: genesis takes no external authority", ErrShape)
		}
		if len(e.Parents) != 0 {
			return fmt.Errorf("%w: genesis has no parents", ErrShape)
		}
		if !system {
			return fmt.Errorf("%w: an event without a carrier must be genesis", ErrShape)
		}
		return nil
	}
	if len(e.Parents) == 0 {
		return fmt.Errorf("%w: a non-genesis event must have parents", ErrShape)
	}
	return nil
}

// headFields are the signed head's fields for a body whose hash is bodyHash.
func (e *Event) headFields(bodyHash [32]byte) (frame.Fields, error) {
	parents, err := normParents(e.Parents)
	if err != nil {
		return nil, err
	}
	e.Parents = parents
	fs := frame.Fields{
		{Tag: TagAuthor, Value: append([]byte(nil), e.Author...)},
		{Tag: TagFresh, Value: append([]byte(nil), e.Fresh[:]...)},
		{Tag: TagBody, Value: append([]byte(nil), bodyHash[:]...)},
	}
	if e.Carrier != nil {
		fs = append(fs, frame.Field{Tag: TagCarrier, Value: append([]byte(nil), e.Carrier[:]...)})
	}
	if e.Authority != nil {
		fs = append(fs, frame.Field{Tag: TagAuthority, Value: append([]byte(nil), e.Authority[:]...)})
	}
	if len(parents) > 0 {
		v := make([]byte, 0, len(parents)*frame.IDSize)
		for _, p := range parents {
			v = append(v, p[:]...)
		}
		fs = append(fs, frame.Field{Tag: TagParents, Value: v})
	}
	if e.Address == AddressRoot {
		fs = append(fs, frame.Field{Tag: TagSystem, Value: []byte{0x01}})
	}
	return fs, nil
}

// bodyFields are the body's fields: address, verb, payload, attestations and
// the salt that keeps the body's hash from saying anything about it.
func (e *Event) bodyFields() (frame.Fields, error) {
	attest, err := normAttest(e.Attest)
	if err != nil {
		return nil, err
	}
	e.Attest = attest
	fs := frame.Fields{
		{Tag: TagAddress, Value: []byte(e.Address)},
		{Tag: TagVerb, Value: []byte(e.Verb)},
		{Tag: TagSalt, Value: append([]byte(nil), e.Salt[:]...)},
	}
	if len(e.Payload) > 0 {
		fs = append(fs, frame.Field{Tag: TagPayload, Value: append([]byte(nil), e.Payload...)})
	}
	if len(attest) > 0 {
		fs = append(fs, frame.Field{Tag: TagAttest, Value: encodeAttest(attest)})
	}
	return fs, nil
}

// Entropy is the randomness a host gives the core: the commands, the booths
// and the home set it to their system's source. The core draws no entropy of
// its own (C5): with it unset, SignFresh and NewFresh refuse rather than
// guess, and SignFrom takes a reader directly.
var Entropy io.Reader

var errNoEntropy = errors.New("event: no randomness was given to this program; the host sets event.Entropy")

// NewFresh draws freshness from the host's randomness.
//
// It is deliberately not called by Sign. Signing stays a pure function of what
// it is given, so the same event always produces the same bytes — and the
// caller is the one who has to say "this is a new act".
//
//	— T3.6, T4.1
func NewFresh() ([FreshSize]byte, error) {
	var f [FreshSize]byte
	if Entropy == nil {
		return f, errNoEntropy
	}
	if _, err := io.ReadFull(Entropy, f[:]); err != nil {
		return f, fmt.Errorf("event: no freshness available: %w", err)
	}
	return f, nil
}

// SignFresh draws freshness and salt from the host's randomness and signs.
// It refuses an event that already carries freshness: this function is for
// making a new act, and re-signing somebody else's draw is not that.
//
//	— T3.6
func SignFresh(e Event, priv ed25519.PrivateKey) (Signed, error) {
	if Entropy == nil {
		return Signed{}, errNoEntropy
	}
	return SignFrom(e, priv, Entropy)
}

// SignFrom is SignFresh with the randomness injected: the same event, key and
// random stream give the same bytes on every platform (contract C5).
func SignFrom(e Event, priv ed25519.PrivateKey, rnd io.Reader) (Signed, error) {
	var zero [FreshSize]byte
	if e.Fresh != zero {
		return Signed{}, fmt.Errorf("%w: this event already carries freshness; "+
			"SignFresh is for a new act", ErrShape)
	}
	if _, err := io.ReadFull(rnd, e.Fresh[:]); err != nil {
		return Signed{}, fmt.Errorf("event: no freshness available: %w", err)
	}
	if _, err := io.ReadFull(rnd, e.Salt[:]); err != nil {
		return Signed{}, fmt.Errorf("event: no salt available: %w", err)
	}
	return Sign(e, priv)
}

// Sign signs an event and returns the finished event: a head that names its
// body by hash, and the body. The private key must match Author; otherwise
// this is an error rather than an unowned signature.
//
//	— T4, T4.2
func Sign(e Event, priv ed25519.PrivateKey) (Signed, error) {
	var out Signed
	if len(priv) != ed25519.PrivateKeySize {
		return out, fmt.Errorf("%w: private key size", ErrShape)
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return out, fmt.Errorf("%w: bad private key", ErrShape)
	}
	if e.Author == nil {
		e.Author = pub
	}
	if !bytes.Equal(e.Author, pub) {
		return out, fmt.Errorf("%w: private key does not match author", ErrShape)
	}
	if err := e.check(); err != nil {
		return out, err
	}
	bfs, err := e.bodyFields()
	if err != nil {
		return out, err
	}
	body, err := frame.EncodeFields(bfs)
	if err != nil {
		return out, err
	}
	if len(body) > frame.MaxBody {
		return out, fmt.Errorf("%w: body over limit (%d)", frame.ErrTooLarge, len(body))
	}
	hfs, err := e.headFields(sha256.Sum256(body))
	if err != nil {
		return out, err
	}
	signed, err := frame.BuildHead(frame.KindEvent, hfs)
	if err != nil {
		return out, err
	}
	head, err := frame.SealHead(signed, ed25519.Sign(priv, signed))
	if err != nil {
		return out, err
	}
	// Read back what we just wrote: if it will not pass our own door, stop
	// here rather than storing it.
	return Parse(append(head, body...))
}

// Parse reads an event — a head and its body, or a head only — checks its
// shape and verifies its signature and the body's hash. A clean result means:
// this head was signed by this key, its form is canonical, and the body is the
// one the head names.
func Parse(raw []byte) (Signed, error) {
	var out Signed
	h, rest, err := frame.ParseHead(raw)
	if err != nil {
		return out, err
	}
	var e Event
	var bodyHash [32]byte
	system := false
	for _, fl := range h.Fields {
		switch fl.Tag {
		case TagCarrier:
			if len(fl.Value) != frame.IDSize {
				return out, fmt.Errorf("%w: carrier size", ErrShape)
			}
			var id frame.ID
			copy(id[:], fl.Value)
			e.Carrier = &id
		case TagAuthor:
			if len(fl.Value) != ed25519.PublicKeySize {
				return out, fmt.Errorf("%w: author key size", ErrShape)
			}
			e.Author = ed25519.PublicKey(fl.Value)
		case TagAuthority:
			if len(fl.Value) != frame.IDSize {
				return out, fmt.Errorf("%w: authority size", ErrShape)
			}
			var id frame.ID
			copy(id[:], fl.Value)
			e.Authority = &id
		case TagParents:
			if len(fl.Value)%frame.IDSize != 0 {
				return out, fmt.Errorf("%w: parents length", ErrShape)
			}
			n := len(fl.Value) / frame.IDSize
			if n == 0 || n > MaxParents {
				return out, fmt.Errorf("%w: parent count (%d)", ErrShape, n)
			}
			ps := make([]frame.ID, n)
			for i := 0; i < n; i++ {
				copy(ps[i][:], fl.Value[i*frame.IDSize:])
				if i > 0 && ps[i-1].Compare(ps[i]) >= 0 {
					return out, fmt.Errorf("%w: parents not ascending and distinct", ErrShape)
				}
			}
			e.Parents = ps
		case TagFresh:
			if len(fl.Value) != FreshSize {
				return out, fmt.Errorf("%w: freshness is %d bytes, want %d",
					ErrShape, len(fl.Value), FreshSize)
			}
			copy(e.Fresh[:], fl.Value)
		case TagBody:
			if len(fl.Value) != 32 {
				return out, fmt.Errorf("%w: body hash size", ErrShape)
			}
			copy(bodyHash[:], fl.Value)
		case TagSystem:
			if len(fl.Value) != 1 || fl.Value[0] != 0x01 {
				return out, fmt.Errorf("%w: the system mark is 0x01 or absent", ErrShape)
			}
			system = true
		default:
			return out, fmt.Errorf("%w in head (tag=%d)", ErrUnknown, fl.Tag)
		}
	}
	if e.Author == nil || !h.Fields.Has(TagFresh) || !h.Fields.Has(TagBody) {
		return out, fmt.Errorf("%w: required head field missing", ErrShape)
	}
	if !ed25519.Verify(e.Author, h.Signed, h.Sig) {
		return out, ErrSig
	}
	// One content, one encoding. Re-encoding must reproduce the input byte
	// for byte; anything else is a second spelling of the same content and
	// is refused.  — N4.1
	re, err := h.Reencode()
	if err != nil {
		return out, err
	}
	if !bytes.Equal(re, h.Raw) {
		return out, fmt.Errorf("%w: non-canonical head", ErrShape)
	}
	out = Signed{ID: h.ID, Head: h.Raw, BodyHash: bodyHash, System: system}
	if len(rest) == 0 {
		// A head only: lineage and authorship, nothing of content.
		if err := e.checkHead(system); err != nil {
			return Signed{}, err
		}
		out.Raw, out.HeadOnly, out.Event = h.Raw, true, e
		return out, nil
	}
	if len(rest) > frame.MaxBody {
		return Signed{}, fmt.Errorf("%w: body over limit (%d)", frame.ErrTooLarge, len(rest))
	}
	if sha256.Sum256(rest) != bodyHash {
		return Signed{}, ErrBody
	}
	body := append([]byte(nil), rest...)
	bfs, err := frame.DecodeFields(body)
	if err != nil {
		return Signed{}, err
	}
	for _, fl := range bfs {
		switch fl.Tag {
		case TagAddress:
			e.Address = string(fl.Value)
		case TagVerb:
			e.Verb = string(fl.Value)
		case TagPayload:
			e.Payload = fl.Value
		case TagAttest:
			as, err := decodeAttest(fl.Value)
			if err != nil {
				return Signed{}, err
			}
			for i := 1; i < len(as); i++ {
				if as[i-1].Oracle > as[i].Oracle ||
					(as[i-1].Oracle == as[i].Oracle && bytes.Compare(as[i-1].Claim, as[i].Claim) >= 0) {
					return Signed{}, fmt.Errorf("%w: attestations not ordered and distinct", ErrShape)
				}
			}
			e.Attest = as
		case TagSalt:
			if len(fl.Value) != SaltSize {
				return Signed{}, fmt.Errorf("%w: salt is %d bytes, want %d", ErrShape, len(fl.Value), SaltSize)
			}
			copy(e.Salt[:], fl.Value)
		default:
			return Signed{}, fmt.Errorf("%w in body (tag=%d)", ErrUnknown, fl.Tag)
		}
	}
	if !bfs.Has(TagAddress) || !bfs.Has(TagVerb) || !bfs.Has(TagSalt) {
		return Signed{}, fmt.Errorf("%w: required body field missing", ErrShape)
	}
	if system != (e.Address == AddressRoot) {
		return Signed{}, fmt.Errorf("%w: the head's system mark disagrees with the address", ErrShape)
	}
	if err := e.check(); err != nil {
		return Signed{}, err
	}
	reb, err := frame.EncodeFields(bfs)
	if err != nil {
		return Signed{}, err
	}
	if !bytes.Equal(reb, body) {
		return Signed{}, fmt.Errorf("%w: non-canonical body", ErrShape)
	}
	out.Raw = append(append([]byte(nil), h.Raw...), body...)
	out.Head = out.Raw[:len(h.Raw):len(h.Raw)]
	out.Body = out.Raw[len(h.Raw):]
	out.Event = e
	return out, nil
}

// Split cuts stored bytes into head and body without verifying either. The
// body is empty for a head only.
func Split(raw []byte) (head, body []byte, err error) {
	n, err := frame.HeadLen(raw)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) < n {
		return nil, nil, frame.ErrShort
	}
	return raw[:n], raw[n:], nil
}

// Objects are the byte strings a content-addressed store keeps for an event:
// the head, named by the event's id, and the body, named by the hash the head
// gives it. A head only has one object.
func Objects(s Signed) [][]byte {
	if s.HeadOnly || len(s.Body) == 0 {
		return [][]byte{s.Head}
	}
	return [][]byte{s.Head, s.Body}
}

// Fetch turns a content-addressed getter into one that returns whole events:
// the head by the event's id, then the body by the hash the head names. A body
// that is not there gives the head only, which proves lineage and nothing of
// content; a head that is not there is an error.
func Fetch(get func(frame.ID) ([]byte, error)) func(frame.ID) ([]byte, error) {
	return func(id frame.ID) ([]byte, error) {
		head, err := get(id)
		if err != nil {
			return nil, err
		}
		h, rest, err := frame.ParseHead(head)
		if err != nil {
			return nil, err
		}
		if len(rest) != 0 {
			return head, nil // already a whole event
		}
		v, ok := h.Fields.Get(TagBody)
		if !ok || len(v) != frame.IDSize {
			return head, nil
		}
		var bid frame.ID
		copy(bid[:], v)
		body, err := get(bid)
		if err != nil {
			return head, nil
		}
		return append(append([]byte(nil), head...), body...), nil
	}
}
