// Package key is opening and covenant: the envelopes that seal every record
// to the readers of its address, the slot cells that turn a passphrase into a
// key, the seals that hand a key's private half to another key, and the fold
// of the keyring that says who reads where (contract 3.1, 3.2, 4.4, 4.6).
//
// It is a core package: standard library only, no clock, no file, no lock,
// and every source of randomness is an injected io.Reader (C1, C5).
package key

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"

	"rokh/event"
	"rokh/frame"
)

// Envelope versions and suites (contract 3.1).
const (
	Version      byte = 0x01
	SuiteReaders byte = 0x01
	SuiteShared  byte = 0x02

	KidSize    = 8
	MaxReaders = 32
	rcptSize   = KidSize + 12 + 48
)

// Record types, the `type` of the envelope's additional data (contract 2.5).
const (
	TypeEvent   byte = 0x01
	TypeContent byte = 0x02
	TypePointer byte = 0x03
	TypeObject  byte = 0x04
)

var (
	// ErrUnread is an envelope of a version or suite this reader does not
	// hold. It is unread, not invalid.
	ErrUnread = errors.New("key: an envelope of a generation this reader does not hold")
	// ErrNotForMe is an envelope with no entry this reader can open.
	ErrNotForMe = errors.New("key: no entry of this envelope is for this reader")
	// ErrForged is an envelope that does not open under the key it names.
	ErrForged = errors.New("key: the envelope does not open; it was altered or forged")
	// ErrNoOwner is an envelope that does not name the owner's reader (E3).
	ErrNoOwner = errors.New("key: the envelope does not name every live generation of the owner's reader")
	// ErrOwnerUnknown is a record whose point's owner generations this
	// session was not told: E3 is judged at the record's own point, and an
	// unknown point is refused, never guessed.
	ErrOwnerUnknown = errors.New("key: the owner generations at this record's point are not known here; it is not opened")
	// ErrPassphrase is a passphrase that opens no cell.
	ErrPassphrase = errors.New("key: the passphrase opens no slot of this vessel")
)

// Kid is the first eight bytes of SHA-256("rokh/kid/1" || reader_pub).
func Kid(pub []byte) [KidSize]byte {
	h := sha256.Sum256(append([]byte("rokh/kid/1"), pub...))
	var k [KidSize]byte
	copy(k[:], h[:KidSize])
	return k
}

// At is the chunk coordinates bound into a content or object envelope: chunk,
// chunks, total size and the chunk's length (contract 3.1, C10). An event and
// a pointer bind nothing: nil.
func At(chunk, chunks uint32, size uint64, length uint32) []byte {
	b := make([]byte, 20)
	binary.BigEndian.PutUint32(b[0:4], chunk)
	binary.BigEndian.PutUint32(b[4:8], chunks)
	binary.BigEndian.PutUint64(b[8:16], size)
	binary.BigEndian.PutUint32(b[16:20], length)
	return b
}

func aad(typ byte, id frame.ID, at []byte) []byte {
	out := append([]byte("rokh/env/1"), typ)
	out = append(out, id[:]...)
	return append(out, at...)
}

func gcm(k []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func read(rnd io.Reader, n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rnd, b); err != nil {
		return nil, fmt.Errorf("key: no randomness: %w", err)
	}
	return b, nil
}

// Reader is a key's reading half: an X25519 private key.
type Reader struct{ priv *ecdh.PrivateKey }

// NewReader makes a reading key from the injected randomness.
func NewReader(rnd io.Reader) (Reader, error) {
	b, err := read(rnd, 32)
	if err != nil {
		return Reader{}, err
	}
	return ReaderFrom(b)
}

// ReaderFrom rebuilds a reading key from its 32 private bytes.
func ReaderFrom(b []byte) (Reader, error) {
	p, err := ecdh.X25519().NewPrivateKey(b)
	if err != nil {
		return Reader{}, err
	}
	return Reader{priv: p}, nil
}

// Public is the reader's public half, the one an envelope is sealed to.
func (r Reader) Public() []byte { return r.priv.PublicKey().Bytes() }

// Bytes is the reader's private half.
func (r Reader) Bytes() []byte { return r.priv.Bytes() }

// Kid names this reader in an envelope.
func (r Reader) Kid() [KidSize]byte { return Kid(r.Public()) }

// SealReaders seals plain to every reader named (suite 0x01). The readers are
// the public halves; they are deduplicated and ordered, so the same inputs and
// the same random stream give the same bytes.
func SealReaders(typ byte, id frame.ID, at []byte, readers [][]byte, plain []byte, rnd io.Reader) ([]byte, error) {
	rs := uniq(readers)
	if len(rs) == 0 || len(rs) > MaxReaders {
		return nil, fmt.Errorf("key: an envelope names 1 to %d readers, not %d", MaxReaders, len(rs))
	}
	ck, err := read(rnd, 32)
	if err != nil {
		return nil, err
	}
	ephKey, err := read(rnd, 32)
	if err != nil {
		return nil, err
	}
	eph, err := ecdh.X25519().NewPrivateKey(ephKey)
	if err != nil {
		return nil, err
	}
	ephPub := eph.PublicKey().Bytes()
	out := []byte{Version, SuiteReaders, 0, 0}
	binary.BigEndian.PutUint16(out[2:4], uint16(len(rs)))
	out = append(out, ephPub...)
	wrapAAD := append(append([]byte(nil), id[:]...), at...)
	for _, pub := range rs {
		rp, err := ecdh.X25519().NewPublicKey(pub)
		if err != nil {
			return nil, fmt.Errorf("key: a reader is not an X25519 key: %w", err)
		}
		shared, err := eph.ECDH(rp)
		if err != nil {
			return nil, err
		}
		wk, err := hkdf.Key(sha256.New, shared, nil, "rokh/env/1/wrap"+string(ephPub)+string(pub), 32)
		if err != nil {
			return nil, err
		}
		g, err := gcm(wk)
		if err != nil {
			return nil, err
		}
		wn, err := read(rnd, 12)
		if err != nil {
			return nil, err
		}
		kid := Kid(pub)
		out = append(out, kid[:]...)
		out = append(out, wn...)
		out = g.Seal(out, wn, ck, wrapAAD)
	}
	nonce, err := read(rnd, 12)
	if err != nil {
		return nil, err
	}
	g, err := gcm(ck)
	if err != nil {
		return nil, err
	}
	out = append(out, nonce...)
	return g.Seal(out, nonce, plain, aad(typ, id, at)), nil
}

// Kids lists the readers an envelope names. It opens nothing.
func Kids(env []byte) ([][KidSize]byte, error) {
	if len(env) < 2 || env[0] != Version {
		return nil, ErrUnread
	}
	switch env[1] {
	case SuiteShared:
		if len(env) < 2+KidSize {
			return nil, ErrForged
		}
		var k [KidSize]byte
		copy(k[:], env[2:])
		return [][KidSize]byte{k}, nil
	case SuiteReaders:
	default:
		return nil, ErrUnread
	}
	if len(env) < 4+32 {
		return nil, ErrForged
	}
	n := int(binary.BigEndian.Uint16(env[2:4]))
	if n == 0 || n > MaxReaders || len(env) < 4+32+n*rcptSize+12+16 {
		return nil, ErrForged
	}
	out := make([][KidSize]byte, n)
	for i := 0; i < n; i++ {
		copy(out[i][:], env[4+32+i*rcptSize:])
	}
	return out, nil
}

// OpenReaders opens a suite-0x01 envelope with one reader's private half.
func OpenReaders(typ byte, id frame.ID, at []byte, env []byte, r Reader) ([]byte, error) {
	kids, err := Kids(env)
	if err != nil {
		return nil, err
	}
	if env[1] != SuiteReaders {
		return nil, ErrUnread
	}
	n := len(kids)
	ephPub := env[4 : 4+32]
	mine := r.Kid()
	eph, err := ecdh.X25519().NewPublicKey(ephPub)
	if err != nil {
		return nil, ErrForged
	}
	shared, err := r.priv.ECDH(eph)
	if err != nil {
		return nil, ErrForged
	}
	pub := r.Public()
	wk, err := hkdf.Key(sha256.New, shared, nil, "rokh/env/1/wrap"+string(ephPub)+string(pub), 32)
	if err != nil {
		return nil, err
	}
	wg, err := gcm(wk)
	if err != nil {
		return nil, err
	}
	wrapAAD := append(append([]byte(nil), id[:]...), at...)
	found := false
	var ck []byte
	for i := 0; i < n; i++ {
		if kids[i] != mine {
			continue
		}
		found = true
		off := 4 + 32 + i*rcptSize + KidSize
		wn := env[off : off+12]
		wrapped := env[off+12 : off+12+48]
		if c, err := wg.Open(nil, wn, wrapped, wrapAAD); err == nil {
			ck = c
			break
		}
	}
	if !found {
		return nil, ErrNotForMe
	}
	if ck == nil {
		return nil, ErrForged
	}
	body := env[4+32+n*rcptSize:]
	g, err := gcm(ck)
	if err != nil {
		return nil, err
	}
	plain, err := g.Open(nil, body[:12], body[12:], aad(typ, id, at))
	if err != nil {
		return nil, ErrForged
	}
	return plain, nil
}

// SealShared seals plain under a shared key K (suite 0x02).
func SealShared(typ byte, id frame.ID, at []byte, k []byte, plain []byte, rnd io.Reader) ([]byte, error) {
	salt, err := read(rnd, 32)
	if err != nil {
		return nil, err
	}
	sk, err := hkdf.Key(sha256.New, k, salt, "rokh/env/1/shared", 32)
	if err != nil {
		return nil, err
	}
	g, err := gcm(sk)
	if err != nil {
		return nil, err
	}
	kid := Kid(k)
	out := []byte{Version, SuiteShared}
	out = append(out, kid[:]...)
	out = append(out, salt...)
	return g.Seal(out, make([]byte, 12), plain, aad(typ, id, at)), nil
}

// OpenShared opens a suite-0x02 envelope under the shared key K.
func OpenShared(typ byte, id frame.ID, at []byte, env []byte, k []byte) ([]byte, error) {
	if len(env) < 2 || env[0] != Version || env[1] != SuiteShared {
		return nil, ErrUnread
	}
	if len(env) < 2+KidSize+32+16 {
		return nil, ErrForged
	}
	if kid := Kid(k); !bytes.Equal(env[2:2+KidSize], kid[:]) {
		return nil, ErrNotForMe
	}
	salt := env[2+KidSize : 2+KidSize+32]
	sk, err := hkdf.Key(sha256.New, k, salt, "rokh/env/1/shared", 32)
	if err != nil {
		return nil, err
	}
	g, err := gcm(sk)
	if err != nil {
		return nil, err
	}
	plain, err := g.Open(nil, make([]byte, 12), env[2+KidSize+32:], aad(typ, id, at))
	if err != nil {
		return nil, ErrForged
	}
	return plain, nil
}

// NamesAll reports whether a suite-0x01 envelope names every reader given:
// E3 asks this of the owner's live generations at commit, union and opening.
func NamesAll(env []byte, readers [][]byte) error {
	kids, err := Kids(env)
	if err != nil {
		return err
	}
	if env[1] != SuiteReaders {
		return nil
	}
	have := map[[KidSize]byte]bool{}
	for _, k := range kids {
		have[k] = true
	}
	for _, r := range readers {
		if !have[Kid(r)] {
			return ErrNoOwner
		}
	}
	return nil
}

// SealTo seals a 32-byte secret to one reader, for vkseal, heir and system
// (contract 4.2, 4.6): eph(32) || nonce(12) || ct(32) || tag(16), 92 bytes.
func SealTo(readerPub []byte, secret []byte, info string, rnd io.Reader) ([]byte, error) {
	if len(secret) != 32 {
		return nil, fmt.Errorf("key: a sealed secret is 32 bytes")
	}
	rp, err := ecdh.X25519().NewPublicKey(readerPub)
	if err != nil {
		return nil, err
	}
	ek, err := read(rnd, 32)
	if err != nil {
		return nil, err
	}
	eph, err := ecdh.X25519().NewPrivateKey(ek)
	if err != nil {
		return nil, err
	}
	shared, err := eph.ECDH(rp)
	if err != nil {
		return nil, err
	}
	ephPub := eph.PublicKey().Bytes()
	k, err := hkdf.Key(sha256.New, shared, nil, info+string(ephPub)+string(readerPub), 32)
	if err != nil {
		return nil, err
	}
	g, err := gcm(k)
	if err != nil {
		return nil, err
	}
	nonce, err := read(rnd, 12)
	if err != nil {
		return nil, err
	}
	out := append(append([]byte(nil), ephPub...), nonce...)
	return g.Seal(out, nonce, secret, []byte(info)), nil
}

// OpenFrom opens a 92-byte seal with the reader it was sealed to.
func OpenFrom(r Reader, sealed []byte, info string) ([]byte, error) {
	if len(sealed) != event.SealedKeySize {
		return nil, ErrForged
	}
	eph, err := ecdh.X25519().NewPublicKey(sealed[:32])
	if err != nil {
		return nil, ErrForged
	}
	shared, err := r.priv.ECDH(eph)
	if err != nil {
		return nil, ErrForged
	}
	k, err := hkdf.Key(sha256.New, shared, nil, info+string(sealed[:32])+string(r.Public()), 32)
	if err != nil {
		return nil, err
	}
	g, err := gcm(k)
	if err != nil {
		return nil, err
	}
	out, err := g.Open(nil, sealed[32:44], sealed[44:], []byte(info))
	if err != nil {
		return nil, ErrForged
	}
	return out, nil
}

// Seal infos.
const (
	InfoVK     = "rokh/1/vk"
	InfoHeir   = "rokh/1/heir"
	InfoSystem = "rokh/1/system"
)

// ---------- slot cells (contract 4.6) ----------

// CellSize, BlobSize: a cell is blob(160) || vkseal(92) || random(4).
const (
	CellSize = 256
	BlobSize = event.SlotSize
	pSize    = 132
)

// Secret is what a slot holds: the key's id and generation, flags, the
// signing seed (zeros when the key cannot sign, or the owner's root in cold
// custody) and the reader's private half.
type Secret struct {
	Key        [32]byte
	Gen        uint32
	Flags      uint32
	SignerSeed [32]byte
	Reader     [32]byte
}

// Signer is the Ed25519 key of this secret, or nil when it cannot sign.
func (s Secret) Signer() ed25519.PrivateKey {
	if s.SignerSeed == [32]byte{} {
		return nil
	}
	return ed25519.NewKeyFromSeed(s.SignerSeed[:])
}

// PassKey is kk := PBKDF2-HMAC-SHA256(passphrase, salt, iterations, 32).
func PassKey(passphrase string, salt []byte, iter int) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("key: an empty passphrase is not allowed")
	}
	if iter < 1 || iter > 10_000_000 {
		return nil, fmt.Errorf("key: iterations %d out of range", iter)
	}
	return pbkdf2.Key(sha256.New, passphrase, salt, iter, 32)
}

func slotAEAD(kk []byte) (cipher.AEAD, error) {
	k, err := hkdf.Key(sha256.New, kk, nil, "rokh/1/slot", 32)
	if err != nil {
		return nil, err
	}
	return gcm(k)
}

// Blob wraps a secret under a passphrase key: nonce(12) || ct || tag(16).
// It is also the `slot` field of a keyring add (4.2).
func Blob(kk []byte, s Secret, rnd io.Reader) ([]byte, error) {
	p := make([]byte, pSize)
	copy(p[0:32], s.Key[:])
	binary.BigEndian.PutUint32(p[32:36], s.Gen)
	binary.BigEndian.PutUint32(p[36:40], s.Flags)
	copy(p[40:72], s.SignerSeed[:])
	copy(p[72:104], s.Reader[:])
	g, err := slotAEAD(kk)
	if err != nil {
		return nil, err
	}
	nonce, err := read(rnd, 12)
	if err != nil {
		return nil, err
	}
	return g.Seal(append([]byte(nil), nonce...), nonce, p, []byte("rokh/1/slot")), nil
}

// OpenBlob unwraps a secret.
func OpenBlob(kk []byte, blob []byte) (Secret, error) {
	var s Secret
	if len(blob) != BlobSize {
		return s, ErrForged
	}
	g, err := slotAEAD(kk)
	if err != nil {
		return s, err
	}
	p, err := g.Open(nil, blob[:12], blob[12:], []byte("rokh/1/slot"))
	if err != nil {
		return s, ErrPassphrase
	}
	copy(s.Key[:], p[0:32])
	s.Gen = binary.BigEndian.Uint32(p[32:36])
	s.Flags = binary.BigEndian.Uint32(p[36:40])
	copy(s.SignerSeed[:], p[40:72])
	copy(s.Reader[:], p[72:104])
	return s, nil
}

// Cell makes a slot cell: the secret wrapped under the passphrase, VK sealed
// to the secret's reader, and four random bytes.
func Cell(kk []byte, s Secret, vk []byte, rnd io.Reader) ([]byte, error) {
	blob, err := Blob(kk, s, rnd)
	if err != nil {
		return nil, err
	}
	return CellFrom(blob, s, vk, rnd)
}

// CellFrom makes a cell around a blob already made: the blob a keyring add
// carries as its slot, so the cell of a live key is known by it.
func CellFrom(blob []byte, s Secret, vk []byte, rnd io.Reader) ([]byte, error) {
	if len(blob) != BlobSize {
		return nil, fmt.Errorf("key: a blob is %d bytes, not %d", len(blob), BlobSize)
	}
	blob = append([]byte(nil), blob...)
	r, err := ReaderFrom(s.Reader[:])
	if err != nil {
		return nil, err
	}
	vkseal, err := SealTo(r.Public(), vk, InfoVK, rnd)
	if err != nil {
		return nil, err
	}
	tail, err := read(rnd, 4)
	if err != nil {
		return nil, err
	}
	out := append(append(blob, vkseal...), tail...)
	if len(out) != CellSize {
		return nil, fmt.Errorf("key: a cell came out %d bytes", len(out))
	}
	return out, nil
}

// OpenCell opens one cell with a passphrase key: the secret and VK.
func OpenCell(kk []byte, cell []byte) (Secret, []byte, error) {
	if len(cell) != CellSize {
		return Secret{}, nil, ErrForged
	}
	s, err := OpenBlob(kk, cell[:BlobSize])
	if err != nil {
		return Secret{}, nil, err
	}
	r, err := ReaderFrom(s.Reader[:])
	if err != nil {
		return Secret{}, nil, err
	}
	vk, err := OpenFrom(r, cell[BlobSize:BlobSize+event.SealedKeySize], InfoVK)
	if err != nil {
		return Secret{}, nil, err
	}
	return s, vk, nil
}

// Unlock is the vessel's unlock function for one passphrase: it derives the
// passphrase key once and tries every cell. Its type matches
// vessel.Unlock (contract 2.10).
func Unlock(passphrase string) func(slots [][]byte, salt []byte, iter int) ([]byte, error) {
	return func(slots [][]byte, salt []byte, iter int) ([]byte, error) {
		_, vk, err := Try(passphrase, slots, salt, iter)
		return vk, err
	}
}

// Try opens the first cell a passphrase opens, and says which secret it held.
func Try(passphrase string, slots [][]byte, salt []byte, iter int) (Secret, []byte, error) {
	kk, err := PassKey(passphrase, salt, iter)
	if err != nil {
		return Secret{}, nil, err
	}
	for _, c := range slots {
		if s, vk, err := OpenCell(kk, c); err == nil {
			return s, vk, nil
		}
	}
	return Secret{}, nil, ErrPassphrase
}

// ---------- the keyring fold (contract 4.4) ----------

// Gen is one live generation of a key, with the event that added it.
type Gen struct {
	Event frame.ID
	event.Keyring
}

// Ring is the fold of the keyring at one point of the ledger: every live
// generation, from which readers(A, P) and the concurrent keys follow.
type Ring struct {
	Live []Gen
}

// Fold builds the ring from the live keyring adds at a point, as
// ledger.Keyring returns them, and a way to read an event's payload.
func Fold(adds []frame.ID, payload func(frame.ID) ([]byte, bool)) (Ring, error) {
	var r Ring
	for _, id := range adds {
		p, ok := payload(id)
		if !ok {
			return r, fmt.Errorf("key: the keyring add %s is not here", id.Short())
		}
		k, err := event.DecodeKeyring(p)
		if err != nil {
			return r, err
		}
		if k.Op != event.KeyringAdd {
			continue
		}
		r.Live = append(r.Live, Gen{Event: id, Keyring: k})
	}
	sort.Slice(r.Live, func(i, j int) bool {
		if r.Live[i].Key != r.Live[j].Key {
			return bytes.Compare(r.Live[i].Key[:], r.Live[j].Key[:]) < 0
		}
		if r.Live[i].Gen != r.Live[j].Gen {
			return r.Live[i].Gen < r.Live[j].Gen
		}
		return r.Live[i].Event.Compare(r.Live[j].Event) < 0
	})
	return r, nil
}

// Owner lists the reading halves of the owner's live generations.
func (r Ring) Owner() [][]byte {
	var out [][]byte
	for _, g := range r.Live {
		if g.IsOwner() {
			out = append(out, g.Reader)
		}
	}
	return out
}

// Readers is readers(A, P): the owner's generations and every live generation
// whose reads cover the address; the system reader too when the address is
// "rokh" or read-open (E4).
func (r Ring) Readers(addr string, system []byte, readOpen bool) [][]byte {
	out := r.Owner()
	for _, g := range r.Live {
		if !g.IsOwner() && g.Covers(addr) {
			out = append(out, g.Reader)
		}
	}
	if system != nil && (addr == event.AddressRoot || readOpen) {
		out = append(out, system)
	}
	return uniq(out)
}

// Concurrent lists the keys that have more than one live generation. Every
// surface marks them until the owner revokes all but one.
func (r Ring) Concurrent() [][32]byte {
	count := map[[32]byte]int{}
	for _, g := range r.Live {
		count[g.Key]++
	}
	var out [][32]byte
	for k, n := range count {
		if n > 1 {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i][:], out[j][:]) < 0 })
	return out
}

// Of returns the live generations of one key.
func (r Ring) Of(k [32]byte) []Gen {
	var out []Gen
	for _, g := range r.Live {
		if g.Key == k {
			out = append(out, g)
		}
	}
	return out
}

func uniq(rs [][]byte) [][]byte {
	cp := make([][]byte, 0, len(rs))
	for _, r := range rs {
		if len(r) != 0 {
			cp = append(cp, r)
		}
	}
	sort.Slice(cp, func(i, j int) bool { return bytes.Compare(cp[i], cp[j]) < 0 })
	out := cp[:0]
	for i, r := range cp {
		if i > 0 && bytes.Equal(cp[i-1], r) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// ---------- the Sealer the vessel uses ----------

// Session seals and opens records for one opening of a vessel: it seals to
// the readers the ring names for an address, always naming the owner, and it
// opens with its own readers.
type Session struct {
	Ring     Ring
	System   []byte // the system reader's public half
	ReadOpen func(addr string) bool
	Mine     []Reader // the readers this session holds, inherited generations included
	Rand     io.Reader
	// OwnerAt answers the owner generations that were live at a record's own
	// point: for an event, the fold at its parents; for content, at the event
	// that describes it. The carrier's caller supplies it from the ledger.
	// Without it the ring's present owners are required.
	OwnerAt func(id frame.ID) (owners [][]byte, known bool)
	// PTK is the vessel's shared key: pointers are sealed under suite 0x02
	// with it (contract E7), not to readers.
	PTK []byte
}

// Seal seals a record at an address (the vessel's Sealer interface). Content and
// objects bind their chunk coordinates through SealAt.
func (s *Session) Seal(kind byte, id frame.ID, address string, plain []byte) ([]byte, error) {
	return s.SealAt(kind, id, address, nil, plain)
}

// SealAt is Seal with the chunk coordinates bound in.
func (s *Session) SealAt(kind byte, id frame.ID, address string, at []byte, plain []byte) ([]byte, error) {
	if kind == TypePointer && len(s.PTK) > 0 {
		return SealShared(kind, id, at, s.PTK, plain, s.Rand)
	}
	open := s.ReadOpen != nil && s.ReadOpen(address)
	readers := s.Ring.Readers(address, s.System, open)
	if len(s.Ring.Owner()) == 0 {
		return nil, ErrNoOwner
	}
	return SealReaders(kind, id, at, readers, plain, s.Rand)
}

// Open opens a record with the first reader of this session that it names.
func (s *Session) Open(kind byte, id frame.ID, envelope []byte) ([]byte, error) {
	return s.OpenAt(kind, id, nil, envelope)
}

// ErrSharedKind refuses a shared-key envelope for a record that must be
// sealed to readers: an event or content (E7, E4).
var ErrSharedKind = errors.New("key: the shared key seals pointers and the home's own records, never an event or content")

// OpenAt is Open with the chunk coordinates bound in.
func (s *Session) OpenAt(kind byte, id frame.ID, at []byte, envelope []byte) ([]byte, error) {
	if len(envelope) >= 2 && envelope[0] == Version && envelope[1] == SuiteShared {
		// Every holder of the vessel's key opens suite 0x02, so it is for
		// pointers and the home's own records; an event or content under it
		// would skip E3 and the readers of its address.
		if kind != TypePointer && kind != TypeObject {
			return nil, ErrSharedKind
		}
		if len(s.PTK) == 0 {
			return nil, ErrNotForMe
		}
		return OpenShared(kind, id, at, envelope, s.PTK)
	}
	owners := s.Ring.Owner()
	pointGiven := false
	if s.OwnerAt != nil {
		o, known := s.OwnerAt(id)
		if !known {
			return nil, ErrOwnerUnknown
		}
		owners, pointGiven = o, true
	}
	if len(owners) == 0 {
		// No point of a rokh has no live owner reader: an empty list is a
		// fold nobody vouched for, not a waiver of E3.
		return nil, ErrOwnerUnknown
	}
	if err := NamesAll(envelope, owners); err != nil {
		if !pointGiven && errors.Is(err, ErrNoOwner) {
			// Judged by the present ring only: the envelope may be sound and
			// sealed at a point whose owner has since rotated. What is
			// missing is that point.
			return nil, fmt.Errorf("%w (no point was given; the present owner is not named: %v)", ErrOwnerUnknown, err)
		}
		return nil, err
	}
	last := error(ErrNotForMe)
	for _, r := range s.Mine {
		p, err := OpenReaders(kind, id, at, envelope, r)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, ErrNotForMe) {
			last = err
		}
	}
	return nil, last
}
