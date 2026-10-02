package event

import (
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
	"strings"

	"rokh/frame"
)

// Keyring payload fields (contract 4.2).
const (
	kTagOp     frame.Tag = 0x0001 // 0x01 add, 0x02 revoke
	kTagKey    frame.Tag = 0x0002 // 32 bytes, the key's id; 32 zero bytes is the owner
	kTagGen    frame.Tag = 0x0003 // u32, from 1
	kTagName   frame.Tag = 0x0004 // one address component, 1..64 bytes
	kTagReader frame.Tag = 0x0005 // 32 bytes X25519
	kTagSigner frame.Tag = 0x0006 // 32 bytes Ed25519; absent means the key cannot write
	kTagReads  frame.Tag = 0x0007 // list of len(1) || prefix; absent reads nothing; one empty prefix reads everything
	kTagSlot   frame.Tag = 0x0008 // 160 bytes, the key's wrapped private half
	kTagHeir   frame.Tag = 0x0009 // 92 bytes, the previous generation's reader sealed to this reader
	kTagTarget frame.Tag = 0x000A // revoke only: id of the add being taken back
	kTagSystem frame.Tag = 0x000B // 92 bytes, the system reader's private half sealed to this reader
)

// Keyring operations.
const (
	KeyringAdd    byte = 0x01
	KeyringRevoke byte = 0x02
)

// Sizes of the keyring's sealed parts.
const (
	ReaderSize    = 32
	SlotSize      = 160
	SealedKeySize = 92
	MaxReads      = 32
)

// Keyring is the payload of rokh.keyring: one generation of one key, added
// or taken back. A generation is never edited; a change of name, scope or key
// pair is an add of the next generation and a revoke of the old one (K3).
//
// Reads is nil when the key reads nothing, and holds the one empty prefix ""
// when it reads everything. Signer is nil when the key cannot write. Writing
// itself is still a rokh.grant to the signer (K2).
type Keyring struct {
	Op     byte
	Key    [32]byte
	Gen    uint32
	Name   string
	Reader []byte
	Signer ed25519.PublicKey
	Reads  []string
	Slot   []byte
	Heir   []byte
	Target frame.ID
	System []byte
}

// IsOwner reports whether this is the owner's key: the key id of 32 zeros.
func (k Keyring) IsOwner() bool { return k.Key == [32]byte{} }

// Covers reports whether this generation's reads cover an address.
func (k Keyring) Covers(addr string) bool {
	for _, p := range k.Reads {
		if ScopeCovers(p, addr) {
			return true
		}
	}
	return false
}

// Prefix lists have one spelling: strictly ascending byte order, no repeat,
// on encode and on decode alike (R3).
func encodePrefixes(ps []string) ([]byte, error) {
	var out []byte
	for i, p := range ps {
		if i > 0 && ps[i-1] >= p {
			return nil, fmt.Errorf("%w: prefixes are listed once each, in ascending byte order", ErrShape)
		}
		if p != "" {
			if err := ValidAddress(p); err != nil {
				return nil, err
			}
		}
		if len(p) > 255 {
			return nil, fmt.Errorf("%w: prefix too long", ErrShape)
		}
		out = append(out, byte(len(p)))
		out = append(out, p...)
	}
	return out, nil
}

func decodePrefixes(b []byte, allowEmpty bool) ([]string, error) {
	var out []string
	for len(b) > 0 {
		n := int(b[0])
		b = b[1:]
		if len(b) < n {
			return nil, fmt.Errorf("%w: truncated prefix list", ErrShape)
		}
		p := string(b[:n])
		b = b[n:]
		if p == "" {
			if !allowEmpty {
				return nil, fmt.Errorf("%w: empty prefix", ErrShape)
			}
		} else if err := ValidAddress(p); err != nil {
			return nil, err
		}
		if len(out) > 0 && out[len(out)-1] >= p {
			return nil, fmt.Errorf("%w: prefixes are listed once each, in ascending byte order", ErrShape)
		}
		out = append(out, p)
		if len(out) > MaxReads {
			return nil, fmt.Errorf("%w: too many prefixes", ErrShape)
		}
	}
	return out, nil
}

func validName(n string) error {
	if len(n) == 0 || len(n) > MaxComponent {
		return fmt.Errorf("%w: key name length (%d)", ErrName, len(n))
	}
	if strings.Contains(n, "/") {
		return fmt.Errorf("%w: a key name is one address component", ErrName)
	}
	return ValidAddress(n)
}

func (k Keyring) check() error {
	switch k.Op {
	case KeyringAdd:
		if err := validName(k.Name); err != nil {
			return err
		}
		if len(k.Reader) != ReaderSize {
			return fmt.Errorf("%w: a keyring add names its reader (32 bytes)", ErrShape)
		}
		if !k.Target.IsZero() {
			return fmt.Errorf("%w: an add has no target", ErrShape)
		}
	case KeyringRevoke:
		if k.Target.IsZero() {
			return fmt.Errorf("%w: a keyring revoke names the add it takes back", ErrShape)
		}
		if k.Name != "" {
			if err := validName(k.Name); err != nil {
				return err
			}
		}
		if len(k.Reader) != 0 && len(k.Reader) != ReaderSize {
			return fmt.Errorf("%w: reader size", ErrShape)
		}
	default:
		return fmt.Errorf("%w: keyring op 0x%02x", ErrShape, k.Op)
	}
	if k.Gen == 0 {
		return fmt.Errorf("%w: a key generation starts at 1", ErrShape)
	}
	if k.Signer != nil && len(k.Signer) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: signer size", ErrShape)
	}
	if k.Slot != nil && len(k.Slot) != SlotSize {
		return fmt.Errorf("%w: slot size (%d)", ErrShape, len(k.Slot))
	}
	if k.Heir != nil && len(k.Heir) != SealedKeySize {
		return fmt.Errorf("%w: heir size (%d)", ErrShape, len(k.Heir))
	}
	if k.System != nil && len(k.System) != SealedKeySize {
		return fmt.Errorf("%w: system seal size (%d)", ErrShape, len(k.System))
	}
	if k.IsOwner() && k.Slot != nil {
		return fmt.Errorf("%w: the owner's slot is never placed in an event", ErrShape)
	}
	return nil
}

// Encode writes a keyring payload in canonical form.
func (k Keyring) Encode() ([]byte, error) {
	if err := k.check(); err != nil {
		return nil, err
	}
	var gen [4]byte
	binary.BigEndian.PutUint32(gen[:], k.Gen)
	fs := frame.Fields{
		{Tag: kTagOp, Value: []byte{k.Op}},
		{Tag: kTagKey, Value: append([]byte(nil), k.Key[:]...)},
		{Tag: kTagGen, Value: gen[:]},
	}
	if k.Name != "" {
		fs = append(fs, frame.Field{Tag: kTagName, Value: []byte(k.Name)})
	}
	if len(k.Reader) > 0 {
		fs = append(fs, frame.Field{Tag: kTagReader, Value: append([]byte(nil), k.Reader...)})
	}
	if len(k.Signer) > 0 {
		fs = append(fs, frame.Field{Tag: kTagSigner, Value: append([]byte(nil), k.Signer...)})
	}
	if k.Reads != nil {
		v, err := encodePrefixes(k.Reads)
		if err != nil {
			return nil, err
		}
		fs = append(fs, frame.Field{Tag: kTagReads, Value: v})
	}
	if len(k.Slot) > 0 {
		fs = append(fs, frame.Field{Tag: kTagSlot, Value: append([]byte(nil), k.Slot...)})
	}
	if len(k.Heir) > 0 {
		fs = append(fs, frame.Field{Tag: kTagHeir, Value: append([]byte(nil), k.Heir...)})
	}
	if !k.Target.IsZero() {
		fs = append(fs, frame.Field{Tag: kTagTarget, Value: append([]byte(nil), k.Target[:]...)})
	}
	if len(k.System) > 0 {
		fs = append(fs, frame.Field{Tag: kTagSystem, Value: append([]byte(nil), k.System...)})
	}
	return frame.EncodeFields(fs)
}

// DecodeKeyring reads a keyring payload and enforces the canonical form.
func DecodeKeyring(b []byte) (Keyring, error) {
	var k Keyring
	fs, err := frame.DecodeFields(b)
	if err != nil {
		return k, err
	}
	for _, f := range fs {
		switch f.Tag {
		case kTagOp:
			if len(f.Value) != 1 {
				return k, fmt.Errorf("%w: keyring op size", ErrShape)
			}
			k.Op = f.Value[0]
		case kTagKey:
			if len(f.Value) != 32 {
				return k, fmt.Errorf("%w: key id size", ErrShape)
			}
			copy(k.Key[:], f.Value)
		case kTagGen:
			if len(f.Value) != 4 {
				return k, fmt.Errorf("%w: generation size", ErrShape)
			}
			k.Gen = binary.BigEndian.Uint32(f.Value)
		case kTagName:
			k.Name = string(f.Value)
		case kTagReader:
			k.Reader = append([]byte(nil), f.Value...)
		case kTagSigner:
			k.Signer = ed25519.PublicKey(append([]byte(nil), f.Value...))
		case kTagReads:
			if k.Reads, err = decodePrefixes(f.Value, true); err != nil {
				return k, err
			}
			if k.Reads == nil {
				k.Reads = []string{}
			}
		case kTagSlot:
			k.Slot = append([]byte(nil), f.Value...)
		case kTagHeir:
			k.Heir = append([]byte(nil), f.Value...)
		case kTagTarget:
			if len(f.Value) != frame.IDSize {
				return k, fmt.Errorf("%w: keyring target size", ErrShape)
			}
			copy(k.Target[:], f.Value)
		case kTagSystem:
			k.System = append([]byte(nil), f.Value...)
		default:
			return k, fmt.Errorf("%w in keyring (tag=%d)", ErrUnknown, f.Tag)
		}
	}
	if !fs.Has(kTagOp) || !fs.Has(kTagKey) || !fs.Has(kTagGen) {
		return k, fmt.Errorf("%w: keyring payload lacks op, key or generation", ErrShape)
	}
	if err := k.check(); err != nil {
		return k, err
	}
	re, err := k.Encode()
	if err != nil {
		return k, err
	}
	if string(re) != string(b) {
		return k, fmt.Errorf("%w: non-canonical keyring payload", ErrShape)
	}
	return k, nil
}

// Seed payload fields (contract 4.7).
const (
	sTagOp     frame.Tag = 0x0001 // 0x01 give, 0x02 take
	sTagSeed   frame.Tag = 0x0002 // 32 random bytes, the seed's id
	sTagKey    frame.Tag = 0x0003 // 32 bytes, id of the keyring key made for the seed
	sTagScopes frame.Tag = 0x0004 // list of prefixes; absent means whole
	sTagSource frame.Tag = 0x0005 // seed id of the giving vessel; absent means the root vessel
	sTagGive   frame.Tag = 0x0006 // take only: id of the give
	sTagVessel frame.Tag = 0x0007 // take only: the new vessel's id
)

// Seed operations.
const (
	SeedGive byte = 0x01
	SeedTake byte = 0x02
)

// Seed is the payload of rokh.seed: a seed given by the root, or taken by the
// key the give names.
type Seed struct {
	Op     byte
	Seed   [32]byte
	Key    [32]byte
	Scopes []string // nil means whole
	Source [32]byte // zero means the root vessel
	Give   frame.ID // take only
	Vessel [32]byte // take only
}

func (s Seed) check() error {
	if s.Seed == [32]byte{} {
		return fmt.Errorf("%w: a seed has an id", ErrShape)
	}
	switch s.Op {
	case SeedGive:
		if !s.Give.IsZero() || s.Vessel != [32]byte{} {
			return fmt.Errorf("%w: a give names no give and no vessel", ErrShape)
		}
	case SeedTake:
		if s.Give.IsZero() || s.Vessel == [32]byte{} {
			return fmt.Errorf("%w: a take names its give and its vessel", ErrShape)
		}
	default:
		return fmt.Errorf("%w: seed op 0x%02x", ErrShape, s.Op)
	}
	return nil
}

// Encode writes a seed payload in canonical form.
func (s Seed) Encode() ([]byte, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	fs := frame.Fields{
		{Tag: sTagOp, Value: []byte{s.Op}},
		{Tag: sTagSeed, Value: append([]byte(nil), s.Seed[:]...)},
		{Tag: sTagKey, Value: append([]byte(nil), s.Key[:]...)},
	}
	if s.Scopes != nil {
		for _, p := range s.Scopes {
			if p == "" {
				return nil, fmt.Errorf("%w: an empty seed scope; absent means whole", ErrShape)
			}
		}
		v, err := encodePrefixes(s.Scopes)
		if err != nil {
			return nil, err
		}
		if len(v) == 0 {
			return nil, fmt.Errorf("%w: an empty scope list; absent means whole", ErrShape)
		}
		fs = append(fs, frame.Field{Tag: sTagScopes, Value: v})
	}
	if s.Source != [32]byte{} {
		fs = append(fs, frame.Field{Tag: sTagSource, Value: append([]byte(nil), s.Source[:]...)})
	}
	if !s.Give.IsZero() {
		fs = append(fs, frame.Field{Tag: sTagGive, Value: append([]byte(nil), s.Give[:]...)})
	}
	if s.Vessel != [32]byte{} {
		fs = append(fs, frame.Field{Tag: sTagVessel, Value: append([]byte(nil), s.Vessel[:]...)})
	}
	return frame.EncodeFields(fs)
}

// DecodeSeed reads a seed payload and enforces the canonical form.
func DecodeSeed(b []byte) (Seed, error) {
	var s Seed
	fs, err := frame.DecodeFields(b)
	if err != nil {
		return s, err
	}
	for _, f := range fs {
		switch f.Tag {
		case sTagOp:
			if len(f.Value) != 1 {
				return s, fmt.Errorf("%w: seed op size", ErrShape)
			}
			s.Op = f.Value[0]
		case sTagSeed, sTagKey, sTagSource, sTagVessel:
			if len(f.Value) != 32 {
				return s, fmt.Errorf("%w: seed field size (tag=%d)", ErrShape, f.Tag)
			}
			var v [32]byte
			copy(v[:], f.Value)
			switch f.Tag {
			case sTagSeed:
				s.Seed = v
			case sTagKey:
				s.Key = v
			case sTagSource:
				s.Source = v
			case sTagVessel:
				s.Vessel = v
			}
		case sTagScopes:
			if s.Scopes, err = decodePrefixes(f.Value, false); err != nil {
				return s, err
			}
		case sTagGive:
			if len(f.Value) != frame.IDSize {
				return s, fmt.Errorf("%w: give size", ErrShape)
			}
			copy(s.Give[:], f.Value)
		default:
			return s, fmt.Errorf("%w in seed (tag=%d)", ErrUnknown, f.Tag)
		}
	}
	if !fs.Has(sTagOp) || !fs.Has(sTagSeed) || !fs.Has(sTagKey) {
		return s, fmt.Errorf("%w: seed payload lacks op, seed or key", ErrShape)
	}
	re, err := s.Encode()
	if err != nil {
		return s, err
	}
	if string(re) != string(b) {
		return s, fmt.Errorf("%w: non-canonical seed payload", ErrShape)
	}
	return s, nil
}

// SeedCovers reports whether a seed's scopes cover an address. No scopes
// means the whole rokh.
func (s Seed) Covers(addr string) bool {
	if s.Scopes == nil {
		return true
	}
	for _, p := range s.Scopes {
		if ScopeCovers(p, addr) {
			return true
		}
	}
	return false
}
