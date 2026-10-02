// Package frame defines the byte grammar of Rokh: one canonical form,
// self-delimiting and versioned.
//
// The founding rule, and the reason this package exists:
//
//	The bytes that are signed, the bytes that are hashed, and the bytes
//	that are stored are the same bytes.
//
// Nothing is ever re-serialized. An event's head is a byte string; its id is
// the hash of that byte string; the head names its body by hash. So "the
// recomputed hash does not match the stored hash" is structurally impossible
// rather than something carefully avoided.
//
// Layout of generation RKH3 (contract 3.4):
//
//	event  := head || body
//	head   := magic(4) || kind(1) || hlen(u32 BE) || fields(hlen) || sig(64)
//	body   := fields
//	fields := field*        ordered by tag, strictly ascending, no duplicates
//	field  := tag(u16 BE) || len(u32 BE) || value(len)
//
// The signature covers every byte of the head before it. The id is sha256 of
// the whole head, signature included. A head without its body is a head only:
// it proves lineage and authorship and nothing of content.
package frame

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const (
	// Magic begins every head, and it is the generation's own name. A second
	// version is a different frame, with its own name and its own verifier,
	// and it does not touch what was written under the first.
	Magic = "RKH3"
	// SigSize is the size of an Ed25519 signature.
	SigSize = 64
	// IDSize is the size of an id (sha256).
	IDSize = 32
	// HeaderSize is magic + kind + hlen.
	HeaderSize = len(Magic) + 1 + 4
	// FieldHeaderSize is tag + len.
	FieldHeaderSize = 2 + 4
)

// Superseded names the generations before RKH3. v1 reads none of them; a
// reader names the generation it does not hold instead of calling the bytes
// malformed.
var Superseded = []string{"RKH1", "RKH2"}

// Limits are part of the grammar, not an implementation preference: a head or
// a body that exceeds them is rejected.
const (
	MaxHead   = 1024    // whole head, signature included
	MaxBody   = 8 << 10 // body
	MaxFrame  = MaxHead + MaxBody
	MaxFields = 16 // fields per head or body
)

var (
	ErrMagic      = errors.New("frame: unknown magic")
	ErrShort      = errors.New("frame: shorter than minimum")
	ErrTooLarge   = errors.New("frame: over limit")
	ErrOrder      = errors.New("frame: fields not strictly ascending")
	ErrEmptyValue = errors.New("frame: empty field, a field is present or absent")
	ErrTruncated  = errors.New("frame: truncated field")
	ErrTooMany    = errors.New("frame: too many fields")
)

// ID is the content address of a frame.
type ID [IDSize]byte

// Zero is the empty id. It is only ever used to mean "absent"; nothing
// assumes a hash can never be zero.
var Zero ID

func (i ID) String() string { return hex.EncodeToString(i[:]) }

// Short is a human-readable abbreviation. It is never a reference and never
// appears on a trust path: acceptance goes by the full name and by the
// bytes.
//
//	— T3.5
func (i ID) Short() string { return hex.EncodeToString(i[:4]) }

func (i ID) IsZero() bool { return i == Zero }

// Compare gives the deterministic byte order of two ids. Every tie-break in
// Rokh rests on this and on nothing else.
func (i ID) Compare(o ID) int {
	for k := 0; k < IDSize; k++ {
		if i[k] != o[k] {
			if i[k] < o[k] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ParseID reads an id from hex. Only the full 64-character form is accepted:
// a truncated id never passes through this door.
func ParseID(s string) (ID, error) {
	var id ID
	if len(s) != IDSize*2 {
		return id, fmt.Errorf("id: bad length (%d, want %d)", len(s), IDSize*2)
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, fmt.Errorf("id: %w", err)
	}
	copy(id[:], b)
	return id, nil
}

// Hash returns the id of a byte string. An event's name is the hash of its
// own bytes: nothing else names it, and nothing is read back out of the
// name. The hash says two things only — equality, and the guess test.
//
//	— T3.2, N4.2
func Hash(b []byte) ID { return sha256.Sum256(b) }

// Kind is the frame type. Room is left for later types, but an unknown one
// is rejected.
type Kind byte

const (
	KindEvent Kind = 0x01
)

func (k Kind) known() bool { return k == KindEvent }

// Tag is the numeric id of a field.
type Tag uint16

// Field is one raw field. Value is never empty.
type Field struct {
	Tag   Tag
	Value []byte
}

// Fields is the field set of a frame, always ordered and duplicate-free.
type Fields []Field

// Get returns a field value.
func (f Fields) Get(t Tag) ([]byte, bool) {
	for _, x := range f {
		if x.Tag == t {
			return x.Value, true
		}
	}
	return nil, false
}

// Has reports whether a field is present.
func (f Fields) Has(t Tag) bool { _, ok := f.Get(t); return ok }

// EncodeFields writes fields in canonical form: ordered by tag, no
// duplicates, no empty values.
func EncodeFields(fs Fields) ([]byte, error) {
	if len(fs) > MaxFields {
		return nil, ErrTooMany
	}
	cp := make(Fields, len(fs))
	copy(cp, fs)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Tag < cp[j].Tag })

	total := 0
	for i, f := range cp {
		if len(f.Value) == 0 {
			return nil, fmt.Errorf("%w (tag=%d)", ErrEmptyValue, f.Tag)
		}
		if i > 0 && cp[i-1].Tag == f.Tag {
			return nil, fmt.Errorf("frame: duplicate field (tag=%d)", f.Tag)
		}
		if len(f.Value) > MaxFrame {
			return nil, ErrTooLarge
		}
		total += FieldHeaderSize + len(f.Value)
	}
	out := make([]byte, 0, total)
	var hdr [FieldHeaderSize]byte
	for _, f := range cp {
		binary.BigEndian.PutUint16(hdr[0:2], uint16(f.Tag))
		binary.BigEndian.PutUint32(hdr[2:6], uint32(len(f.Value)))
		out = append(out, hdr[:]...)
		out = append(out, f.Value...)
	}
	return out, nil
}

// DecodeFields reads fields and enforces the canonical form. Any deviation
// is an error, never something waved through.
//
// Values are copied, so the result does not alias the input buffer.
func DecodeFields(b []byte) (Fields, error) { return decodeFields(b, false) }

// decodeFields is DecodeFields with a choice: alias makes each value a window
// on the input rather than a copy, for a caller that owns the buffer and
// keeps it — Parse, whose Frame holds Raw for as long as its fields live.
func decodeFields(b []byte, alias bool) (Fields, error) {
	var out Fields
	var last Tag
	first := true
	for len(b) > 0 {
		if len(b) < FieldHeaderSize {
			return nil, ErrTruncated
		}
		t := Tag(binary.BigEndian.Uint16(b[0:2]))
		n := binary.BigEndian.Uint32(b[2:6])
		if n == 0 {
			return nil, fmt.Errorf("%w (tag=%d)", ErrEmptyValue, t)
		}
		if uint64(n) > uint64(MaxFrame) {
			return nil, ErrTooLarge
		}
		b = b[FieldHeaderSize:]
		if uint64(len(b)) < uint64(n) {
			return nil, ErrTruncated
		}
		if !first && t <= last {
			return nil, fmt.Errorf("%w (tag=%d after %d)", ErrOrder, t, last)
		}
		if len(out) == MaxFields {
			return nil, ErrTooMany
		}
		v := b[:n:n]
		if !alias {
			v = append([]byte(nil), v...)
		}
		out = append(out, Field{Tag: t, Value: v})
		b = b[n:]
		last, first = t, false
	}
	return out, nil
}

// BuildHead produces the signable part of a head: magic || kind || hlen ||
// fields.
//
// What is signed, what is hashed and what is stored is one byte string, not
// three. Everything downstream reads back exactly these bytes.
func BuildHead(kind Kind, fs Fields) ([]byte, error) {
	if !kind.known() {
		return nil, fmt.Errorf("frame: unknown kind (0x%02x)", byte(kind))
	}
	body, err := EncodeFields(fs)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, HeaderSize+len(body)+SigSize)
	out = append(out, Magic...)
	out = append(out, byte(kind))
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(body)))
	out = append(out, n[:]...)
	out = append(out, body...)
	if len(out)+SigSize > MaxHead {
		return nil, ErrTooLarge
	}
	return out, nil
}

// SealHead appends the signature to the signable part, producing a whole head.
func SealHead(signed []byte, sig []byte) ([]byte, error) {
	if len(sig) != SigSize {
		return nil, fmt.Errorf("frame: bad signature size (%d)", len(sig))
	}
	out := make([]byte, 0, len(signed)+SigSize)
	out = append(out, signed...)
	out = append(out, sig...)
	if len(out) > MaxHead {
		return nil, ErrTooLarge
	}
	return out, nil
}

// Head is a head checked at the grammar level, not yet at the signature
// level.
//
// Raw is an independent copy of the head's bytes; Signed and Sig are slices
// of it.
type Head struct {
	Kind   Kind
	Fields Fields
	Signed []byte // the bytes the signature covers
	Sig    []byte
	Raw    []byte // the whole head, exactly as stored
	ID     ID     // sha256(Raw)
}

// HeadLen reads the length of the head that begins b, without checking more
// than its header. It is how a stored event is split into head and body.
func HeadLen(b []byte) (int, error) {
	if len(b) < HeaderSize {
		return 0, ErrShort
	}
	if got := string(b[:len(Magic)]); got != Magic {
		return 0, magicError(got)
	}
	hlen := binary.BigEndian.Uint32(b[len(Magic)+1 : HeaderSize])
	n := uint64(HeaderSize) + uint64(hlen) + SigSize
	if n > MaxHead {
		return 0, ErrTooLarge
	}
	return int(n), nil
}

func magicError(got string) error {
	for _, old := range Superseded {
		if got == old {
			return fmt.Errorf("%w: %q is an earlier generation, which v1 does not hold", ErrMagic, got)
		}
	}
	return fmt.Errorf("%w: %q", ErrMagic, got)
}

// ParseHead reads the head at the start of raw and enforces the canonical
// form. It returns the head and whatever follows it, which is the body or
// nothing. The signature is not checked here; that belongs to the event
// layer, which knows the author key.
func ParseHead(raw []byte) (*Head, []byte, error) {
	n, err := HeadLen(raw)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) < n {
		return nil, nil, ErrShort
	}
	cp := append([]byte(nil), raw[:n]...)
	kind := Kind(cp[len(Magic)])
	if !kind.known() {
		return nil, nil, fmt.Errorf("frame: unknown kind (0x%02x)", byte(kind))
	}
	cut := n - SigSize
	fs, err := decodeFields(cp[HeaderSize:cut], true)
	if err != nil {
		return nil, nil, err
	}
	return &Head{
		Kind:   kind,
		Fields: fs,
		Signed: cp[:cut],
		Sig:    cp[cut:],
		Raw:    cp,
		ID:     Hash(cp),
	}, raw[n:], nil
}

// Reencode rebuilds the head from the decoded fields. If the result is not
// byte-identical to Raw, the canonical form is broken. A test rests on this.
func (h *Head) Reencode() ([]byte, error) {
	signed, err := BuildHead(h.Kind, h.Fields)
	if err != nil {
		return nil, err
	}
	return SealHead(signed, h.Sig)
}

// ShortIn is the abbreviation as it must appear when other names are in view.
//
// A short name is a guide for the eye and never a reference: acceptance goes
// by the full name and by the bytes. But a guide that points at two things is
// not a guide, so wherever two abbreviations would collide the abbreviation
// lengthens — by one byte at a time, until it is unambiguous among the names
// actually shown, and at worst until it is the whole name.
//
// It lengthens against the names given, not against the whole ledger, because
// ambiguity is a property of what is in front of the reader. A short name that
// is unique on this screen has done its whole job.
//
//	— T3.5
func (i ID) ShortIn(among []ID) string {
	n := 4
	for n < IDSize {
		clash := false
		for _, o := range among {
			if o == i {
				continue
			}
			if string(o[:n]) == string(i[:n]) {
				clash = true
				break
			}
		}
		if !clash {
			break
		}
		n++
	}
	return hex.EncodeToString(i[:n])
}

// ShortAll abbreviates a whole set at once, every name to the same length, so
// that a column of them lines up and no two are the same.
//
// One length for the set rather than a length each is a readability choice,
// not a rule: names of differing lengths in one column read as differing
// names. The rule is only that no two collide.
//
//	— T3.5
func ShortAll(ids []ID) map[ID]string {
	n := 4
	for n < IDSize {
		seen := map[string]bool{}
		clash := false
		for _, i := range ids {
			k := string(i[:n])
			if seen[k] {
				clash = true
				break
			}
			seen[k] = true
		}
		if !clash {
			break
		}
		n++
	}
	out := make(map[ID]string, len(ids))
	for _, i := range ids {
		out[i] = hex.EncodeToString(i[:n])
	}
	return out
}

// MarshalJSON writes an id as its lowercase hex, which is what it is called
// everywhere else.
//
// Without this an id is a [32]byte, and encoding/json writes a byte *array* as
// a list of numbers — so a name inside a payload would read [62,35,232,…]
// rather than its hex. That is not wrong so much as unshared: every other
// spelling of an id in Rokh, in its documents and on screen, is the hex.
//
//	— T3.2, N4.1
func (i ID) MarshalJSON() ([]byte, error) {
	b := make([]byte, 0, IDSize*2+2)
	b = append(b, '"')
	b = append(b, []byte(i.String())...)
	return append(b, '"'), nil
}

// UnmarshalJSON reads an id from its hex, and refuses anything that is not
// exactly one.
func (i *ID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	got, err := ParseID(s)
	if err != nil {
		return err
	}
	*i = got
	return nil
}
