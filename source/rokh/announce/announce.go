// Package announce is the narrow path: news of an event, never the event.
//
// # What this is for
//
// A Rokh event is 230 to 400 bytes and fits in no LoRa frame. Rather than
// fragment events across a link that carries 51 bytes per frame under a one
// percent duty cycle, the narrow mesh carries only *news*: "ledger X has a
// new head H." The event itself arrives later over a link that can afford it.
//
// That is the simplest design that captures most of the value, and it is the
// one implemented here.
//
// # An announcement is a hint, not evidence
//
// Announcements are **unsigned**, deliberately. A signature would add 64
// bytes and push the frame past the smallest LoRa payload, and it would buy
// nothing: an announcement asserts no right and transfers no data. Nothing is
// ever accepted on the strength of an announcement. The worst a forged one
// can do is cause a wasted fetch, which the receiver then rejects at the
// ledger door like any other bad bytes.
//
// So the rule from the transport layer holds here too, with one clause added:
// the narrow path grants arrival, not permission — and not truth either.
//
// # Layout
//
//	byte  0      magic 'R'
//	byte  1      version 0x01
//	byte  2      flags; bit 0 set if metadata follows
//	bytes 3..10  ledger anchor, first 8 bytes
//	bytes 11..18 head id, first 8 bytes
//	bytes 19..   optional metadata, tag(u8) len(u8) value
//
// Nineteen bytes without metadata, at most MaxSize with it.
//
// # Truncated ids
//
// Eight bytes identify which ledger and which head. That is enough to *point*
// and never enough to *trust*: the receiver fetches the full event and
// verifies it against the full id. frame.ParseID refuses anything shorter
// than a complete id, so a truncated form cannot leak onto a trust path.
package announce

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"rokh/frame"
)

const (
	Magic   byte = 'R'
	Version byte = 0x01

	// ShortLen is how much of an id an announcement carries.
	ShortLen = 8

	// HeaderSize is magic + version + flags + ledger + head.
	HeaderSize = 3 + ShortLen + ShortLen

	// MaxSize is the smallest LoRa payload we intend to support (SF12,
	// EU868). An announcement always fits one such frame; that is the whole
	// point of this package and it is enforced, not hoped for.
	MaxSize = 51

	// MaxAddressHint keeps the whole announcement inside MaxSize even with
	// every optional field present.
	MaxAddressHint = 24

	flagMeta byte = 1 << 0

	tagCount   byte = 0x01
	tagAddress byte = 0x02
)

var (
	ErrMagic     = errors.New("announce: unknown magic")
	ErrVersion   = errors.New("announce: unknown version")
	ErrShort     = errors.New("announce: too short")
	ErrTooLarge  = errors.New("announce: over limit")
	ErrTruncated = errors.New("announce: truncated metadata")
	ErrOrder     = errors.New("announce: metadata not strictly ascending")
	ErrFlags     = errors.New("announce: flags do not match content")
)

// Short is the truncated form of an id used on the narrow path.
type Short [ShortLen]byte

func (s Short) String() string { return hex.EncodeToString(s[:]) }

// shorten takes the leading bytes of a full id.
func shorten(id frame.ID) Short {
	var s Short
	copy(s[:], id[:ShortLen])
	return s
}

// Announcement is news that a ledger has a new head.
type Announcement struct {
	Ledger Short
	Head   Short

	// Count is the announcer's claim about how many accepted events its
	// ledger holds, so a receiver can size the fetch before asking. Zero
	// means "not stated". It saturates at 65535; it is a hint, not a census.
	Count uint16

	// Address is a hint about what changed, truncated to MaxAddressHint.
	// Empty means "not stated".
	Address string
}

// New builds an announcement from full ids.
//
// The address is truncated on a rune boundary. It is only ever a hint, so a
// clipped one is not a defect.
func New(anchor, head frame.ID, count int, address string) Announcement {
	a := Announcement{Ledger: shorten(anchor), Head: shorten(head)}
	if count > 0 {
		if count > 0xFFFF {
			count = 0xFFFF
		}
		a.Count = uint16(count)
	}
	a.Address = clip(address, MaxAddressHint)
	return a
}

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// Encode writes the announcement.
func (a Announcement) Encode() ([]byte, error) {
	if len(a.Address) > MaxAddressHint {
		return nil, fmt.Errorf("%w: address hint (%d)", ErrTooLarge, len(a.Address))
	}
	if a.Address != "" && !utf8.ValidString(a.Address) {
		return nil, errors.New("announce: address hint is not valid UTF-8")
	}
	if strings.ContainsRune(a.Address, 0) {
		return nil, errors.New("announce: NUL in address hint")
	}

	var meta []byte
	// Metadata tags are written in ascending order, so one announcement has
	// exactly one encoding.
	if a.Count > 0 {
		var n [2]byte
		binary.BigEndian.PutUint16(n[:], a.Count)
		meta = append(meta, tagCount, 2, n[0], n[1])
	}
	if a.Address != "" {
		meta = append(meta, tagAddress, byte(len(a.Address)))
		meta = append(meta, a.Address...)
	}

	out := make([]byte, 0, HeaderSize+len(meta))
	flags := byte(0)
	if len(meta) > 0 {
		flags |= flagMeta
	}
	out = append(out, Magic, Version, flags)
	out = append(out, a.Ledger[:]...)
	out = append(out, a.Head[:]...)
	out = append(out, meta...)

	if len(out) > MaxSize {
		return nil, fmt.Errorf("%w: %d > %d", ErrTooLarge, len(out), MaxSize)
	}
	return out, nil
}

// Decode reads an announcement and enforces the canonical form.
func Decode(b []byte) (Announcement, error) {
	var a Announcement
	if len(b) > MaxSize {
		return a, ErrTooLarge
	}
	if len(b) < HeaderSize {
		return a, ErrShort
	}
	if b[0] != Magic {
		return a, ErrMagic
	}
	if b[1] != Version {
		return a, ErrVersion
	}
	flags := b[2]
	if flags&^flagMeta != 0 {
		return a, fmt.Errorf("%w: unknown flag bits", ErrFlags)
	}
	copy(a.Ledger[:], b[3:3+ShortLen])
	copy(a.Head[:], b[3+ShortLen:HeaderSize])

	meta := b[HeaderSize:]
	if (len(meta) > 0) != (flags&flagMeta != 0) {
		return a, ErrFlags
	}

	var last byte
	first := true
	for len(meta) > 0 {
		if len(meta) < 2 {
			return a, ErrTruncated
		}
		tag, n := meta[0], int(meta[1])
		meta = meta[2:]
		if len(meta) < n || n == 0 {
			return a, ErrTruncated
		}
		if !first && tag <= last {
			return a, ErrOrder
		}
		switch tag {
		case tagCount:
			if n != 2 {
				return a, fmt.Errorf("%w: count length", ErrTruncated)
			}
			a.Count = binary.BigEndian.Uint16(meta[:2])
			if a.Count == 0 {
				return a, errors.New("announce: count present but zero")
			}
		case tagAddress:
			if n > MaxAddressHint {
				return a, ErrTooLarge
			}
			a.Address = string(meta[:n])
			if !utf8.ValidString(a.Address) {
				return a, errors.New("announce: address hint is not valid UTF-8")
			}
		default:
			return a, fmt.Errorf("announce: unknown metadata tag 0x%02x", tag)
		}
		meta = meta[n:]
		last, first = tag, false
	}
	return a, nil
}

// MatchesLedger reports whether a full anchor could be the one announced.
// A match is a reason to look, never a reason to trust.
func (a Announcement) MatchesLedger(anchor frame.ID) bool {
	return a.Ledger == shorten(anchor)
}

// MatchesHead reports whether a full id could be the head announced.
func (a Announcement) MatchesHead(id frame.ID) bool {
	return a.Head == shorten(id)
}

// String is a one-line human form.
func (a Announcement) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "ledger=%s head=%s", a.Ledger, a.Head)
	if a.Count > 0 {
		fmt.Fprintf(&sb, " count=%d", a.Count)
	}
	if a.Address != "" {
		fmt.Fprintf(&sb, " address=%q", a.Address)
	}
	return sb.String()
}

// ---------- hello ----------

// A hello is a knock: one public key, announced on whatever path carried it.
//
// It is the only frame besides an announcement that may travel the narrow
// path, and it is the smallest useful thing that can: 34 bytes, well inside an
// SF12 frame.
//
// It says exactly one thing - "this key exists and was reachable this way" -
// and it is therefore:
//
//   - not a right;
//   - not the start of a transfer;
//   - not a share decision;
//   - not evidence, being unsigned like an announcement.
//
// A distinct magic byte, rather than a new version of the announcement, so
// that neither decoder can ever accept the other's frame.
const (
	HelloMagic   byte = 'K'
	HelloVersion byte = 0x01
	HelloSize         = 2 + 32
)

var ErrHelloShape = errors.New("announce: not a hello frame")

// Hello carries one Ed25519 public key.
type Hello struct {
	Key [32]byte
}

// NewHello builds a knock from a public key.
func NewHello(key []byte) (Hello, error) {
	var h Hello
	if len(key) != len(h.Key) {
		return h, fmt.Errorf("%w: key must be %d bytes, got %d", ErrHelloShape, len(h.Key), len(key))
	}
	copy(h.Key[:], key)
	return h, nil
}

// Encode writes the knock. It is always exactly HelloSize bytes.
func (h Hello) Encode() []byte {
	out := make([]byte, 0, HelloSize)
	out = append(out, HelloMagic, HelloVersion)
	return append(out, h.Key[:]...)
}

// DecodeHello reads a knock and enforces its exact shape.
func DecodeHello(b []byte) (Hello, error) {
	var h Hello
	if len(b) != HelloSize {
		return h, fmt.Errorf("%w: length %d, want %d", ErrHelloShape, len(b), HelloSize)
	}
	if b[0] != HelloMagic {
		return h, ErrHelloShape
	}
	if b[1] != HelloVersion {
		return h, ErrVersion
	}
	copy(h.Key[:], b[2:])
	return h, nil
}

func (h Hello) String() string { return "key=" + hex.EncodeToString(h.Key[:]) }
