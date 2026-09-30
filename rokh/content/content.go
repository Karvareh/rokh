// Package content is the minimal content descriptor: hash, size, type.
//
// Bulk content never sits inside an event - the 4 KiB inline payload limit
// makes that structurally impossible. An event carries this descriptor
// instead, and the bytes themselves live wherever an application puts them.
//
// # This is not core
//
// The core sees an opaque payload under 4 KiB and asks nothing about it.
// Nothing here is reserved, nothing here is privileged, and an implementation
// that ignores this package is still a correct Rokh implementation. It is a
// bundle-layer codec offered so that two programs can mean the same thing by
// "here is a file".
//
// # Three fields, and no fourth
//
// There is deliberately no filename. Content is addressed by its hash; the
// media type supplies an extension when one is wanted. A name would be a
// fourth thing to disagree about.
package content

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"rokh/frame"
)

// Descriptor field tags. Same canonical TLV codec as a grant payload.
const (
	tagHash frame.Tag = 0x0001 // 32 bytes, sha256 of the content
	tagSize frame.Tag = 0x0002 // 8 bytes big-endian, the content length
	tagType frame.Tag = 0x0003 // UTF-8 media type
)

// MaxType bounds the media type.
const MaxType = 64

// Conventional verbs for content events. Ordinary, non-reserved verbs: the
// core neither knows nor cares about them.
const (
	VerbPut    = "content.put"
	VerbDelete = "content.delete"
)

var ErrShape = errors.New("content: bad descriptor")

// Descriptor names one blob of content.
// Heavy content travels as a descriptor — hash, size, type. Rokh records
// the handing over and the receipt; it does not hold the item itself. So if
// the file is lost, Rokh knows that it was and what it was — and does not
// bring it back.
//
//	— N2.11, N4.5, N6.5
type Descriptor struct {
	Hash frame.ID
	Size uint64
	Type string
}

// New builds a descriptor from the content bytes themselves, so the hash and
// size can never disagree with what is being described.
func New(body []byte, mediaType string) (Descriptor, error) {
	d := Descriptor{Hash: frame.Hash(body), Size: uint64(len(body)), Type: mediaType}
	if err := d.check(); err != nil {
		return Descriptor{}, err
	}
	return d, nil
}

func (d Descriptor) check() error {
	if d.Hash.IsZero() {
		return fmt.Errorf("%w: no hash", ErrShape)
	}
	if len(d.Type) == 0 || len(d.Type) > MaxType {
		return fmt.Errorf("%w: media type length (%d)", ErrShape, len(d.Type))
	}
	if !utf8.ValidString(d.Type) {
		return fmt.Errorf("%w: media type is not valid UTF-8", ErrShape)
	}
	for _, r := range d.Type {
		if r < 0x21 || r > 0x7E {
			return fmt.Errorf("%w: media type must be printable ASCII", ErrShape)
		}
	}
	return nil
}

// Encode writes the descriptor in canonical form.
func (d Descriptor) Encode() ([]byte, error) {
	if err := d.check(); err != nil {
		return nil, err
	}
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], d.Size)
	return frame.EncodeFields(frame.Fields{
		{Tag: tagHash, Value: append([]byte(nil), d.Hash[:]...)},
		{Tag: tagSize, Value: size[:]},
		{Tag: tagType, Value: []byte(d.Type)},
	})
}

// Decode reads a descriptor and enforces the canonical form.
func Decode(b []byte) (Descriptor, error) {
	var d Descriptor
	fs, err := frame.DecodeFields(b)
	if err != nil {
		return d, err
	}
	var haveHash, haveSize, haveType bool
	for _, f := range fs {
		switch f.Tag {
		case tagHash:
			if len(f.Value) != frame.IDSize {
				return d, fmt.Errorf("%w: hash size", ErrShape)
			}
			copy(d.Hash[:], f.Value)
			haveHash = true
		case tagSize:
			if len(f.Value) != 8 {
				return d, fmt.Errorf("%w: size field must be 8 bytes", ErrShape)
			}
			d.Size = binary.BigEndian.Uint64(f.Value)
			haveSize = true
		case tagType:
			d.Type = string(f.Value)
			haveType = true
		default:
			return d, fmt.Errorf("%w: unknown field (tag=%d)", ErrShape, f.Tag)
		}
	}
	if !haveHash || !haveSize || !haveType {
		return d, fmt.Errorf("%w: a required field is missing", ErrShape)
	}
	if err := d.check(); err != nil {
		return d, err
	}
	return d, nil
}

// Verify reports whether these bytes are the content this descriptor names.
//
// A receiver runs this before storing anything. A hash that arrives without an
// accepted descriptor event is not a reason to fetch, and bytes that do not
// match a descriptor are not a reason to keep them.
func (d Descriptor) Verify(body []byte) error {
	if uint64(len(body)) != d.Size {
		return fmt.Errorf("%w: size %d, want %d", ErrShape, len(body), d.Size)
	}
	if got := frame.Hash(body); got != d.Hash {
		return fmt.Errorf("%w: hash %s, want %s", ErrShape, got.Short(), d.Hash.Short())
	}
	return nil
}

// Extension is the conventional file suffix for a media type, used when
// content is written to a filesystem. It is a convenience for adapters, never
// part of the identity of anything.
func (d Descriptor) Extension() string {
	switch d.Type {
	case "text/plain":
		return ".txt"
	case "text/markdown":
		return ".md"
	case "application/json":
		return ".json"
	}
	if i := strings.IndexByte(d.Type, '/'); i >= 0 && i+1 < len(d.Type) {
		sub := d.Type[i+1:]
		if j := strings.IndexByte(sub, '+'); j >= 0 {
			sub = sub[j+1:]
		}
		return "." + sub
	}
	return ".bin"
}

func (d Descriptor) String() string {
	return fmt.Sprintf("%s %d bytes %s", d.Hash.Short(), d.Size, d.Type)
}
