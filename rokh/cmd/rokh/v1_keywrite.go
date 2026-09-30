package main

// A key writes with its own passphrase (contract 3.3, 4.3 K2, section 5).
//
// A key's passphrase opens the key's own cell and nothing else (4.6): its
// signing seed, its reader, the vessel's key. The command line's session is
// then bound to that one key (B3), and write records under the session key
// (section 5): signed by that key's signer, under a live grant made to that
// signer (K2), never by the root, which only the owner's cell holds. The
// record is sealed at its own point, the head of the branch it is written
// on: to the owner generations live there (E3) and to every live generation
// whose reads cover its address there, the system reader too where the
// address is rokh or read-open there (E4).
//
// What cannot be written so is refused before anything is signed, and
// nothing is recorded: a key without a signer, an address or a verb outside
// the key's own grants, the root or another key named by --key, a point
// where the key is not live or no owner generation is. An open address
// (4.5) is not written through here: only the key's own grants are taken.
//
// An attempt is found again by reading the record that carries it (section
// 6), and a key reads only its view (E4, K2). A key that signed records it
// cannot read cannot rule out that one of them carries an attempt, so such an
// attempt, not found in what the key reads, is refused rather than recorded a
// second time, and rokh attempt answers unknown for it rather than not
// recorded.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"rokh/booth"
	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
)

// refusalOf is a recording's typed answer when nothing reached the carrier.
func refusalOf(code string, err error) map[string]any {
	return map[string]any{"ok": false, "code": code, "error": err.Error(), "record": daemon.NotRecorded}
}

// ownKey is the key a key's own passphrase opened, live in this session's
// keyring, for --key NAME. The session holds that one key (B3): no name, its
// name or its id names it. The root is refused, and so is every other key:
// this passphrase holds neither.
func ownKey(s *session, name string) (liveKey, map[string]any) {
	mine, err := key.ReaderFrom(s.sec.Reader[:])
	if err != nil {
		return liveKey{}, refusalOf("key_unknown", err)
	}
	var own liveKey
	found := false
	for _, k := range liveKeys(s) {
		if k.k.Key == s.sec.Key && k.k.Gen == s.sec.Gen && bytes.Equal(k.k.Reader, mine.Public()) {
			own, found = k, true
		}
	}
	if !found {
		return liveKey{}, refusalOf(booth.CodeKeyRevoked, errors.New("the key this passphrase opens is not live in the keyring; nothing was recorded"))
	}
	switch {
	case name == keyRootName:
		return liveKey{}, refusalOf("root_refused", fmt.Errorf("this passphrase opens key %q, not the owner's cell: the root signs only with the owner's passphrase; nothing was recorded", own.k.Name))
	case name == "" || name == own.k.Name || name == hex.EncodeToString(own.k.Key[:]):
		return own, nil
	}
	return liveKey{}, refusalOf("key_unknown", fmt.Errorf("this passphrase opens key %q and signs as that key only, not as %q; nothing was recorded", own.k.Name, name))
}

// ownSigner is ownKey for a key that must sign: a generation without a
// signer cannot write (K2).
func ownSigner(s *session, name string) (liveKey, map[string]any) {
	own, bad := ownKey(s, name)
	if bad != nil {
		return liveKey{}, bad
	}
	if own.k.Signer == nil {
		return liveKey{}, refusalOf(booth.CodeViewDenied, fmt.Errorf("key %q cannot write: its generation has no signer (contract K2); nothing was recorded", own.k.Name))
	}
	return own, nil
}

// oneKey is the door's key source for a key's own write: that key's signer,
// under the one grant chosen for the record.
type oneKey struct {
	name  string
	alias string
	priv  ed25519.PrivateKey
	grant frame.ID
}

// unreadOwn counts the accepted events a signer signed that this session
// holds as heads only: what a key wrote where it does not read (K2). Their
// bodies, and the attempts signed into them, are not this key's to open.
func (s *session) unreadOwn(pub ed25519.PublicKey) int {
	n := 0
	for _, id := range s.led.Order() {
		if e, _ := s.led.Get(id); e.HeadOnly && bytes.Equal(e.Event.Author, pub) {
			n++
		}
	}
	return n
}

// errAttemptUnjudged is an attempt a key's door cannot look for: the key
// signed events it cannot read, and any of them may carry the attempt.
var errAttemptUnjudged = errors.New("an earlier recording under this attempt cannot be ruled out")

// keyAttempt holds a key's write to the attempt rule (contract section 6:
// the same attempt again returns the first result). The door finds an
// attempt in the records the key reads. A key that has signed events it
// cannot read cannot rule out that one of them carries this attempt, so an
// attempt the door does not find there is not recorded again: it is refused
// and nothing is recorded. Without such events, or when the door finds the
// attempt, the door answers as it always does.
func (s *session) keyAttempt(srv *daemon.Server, ks oneKey, attempt string) map[string]any {
	pub := ks.priv.Public().(ed25519.PublicKey)
	hidden := s.unreadOwn(pub)
	if attempt == "" || hidden == 0 {
		return nil
	}
	line, err := json.Marshal(map[string]any{"op": "attempt", "attempt": attempt, "author": hex.EncodeToString(pub)})
	if err != nil {
		return refusalOf("bad_request", err)
	}
	r := srv.Handle(line)
	if ok, _ := r["ok"].(bool); !ok {
		r["record"] = daemon.NotRecorded
		return r
	}
	if r["record"] == daemon.Recorded {
		return nil
	}
	return refusalOf("attempt_unsupported", fmt.Errorf("key %q signed %d events it cannot read, so %v under %q; nothing was recorded. Write without --attempt, or ask rokh attempt with the owner's passphrase",
		ks.name, hidden, errAttemptUnjudged, attempt))
}

func (o oneKey) Key(name string) (ed25519.PrivateKey, *frame.ID, bool) {
	if name != o.alias {
		return nil, nil, false
	}
	g := o.grant
	return append(ed25519.PrivateKey(nil), o.priv...), &g, true
}

func (o oneKey) Names() []string { return []string{o.alias} }

// keySigner prepares a key's own write of address and verb on branch: the
// key's signer, the grant it writes under at the branch's head, and the
// carrier sealing at that point. It answers a typed refusal when the write
// cannot be the key's, and then nothing is signed or recorded.
func (s *session) keySigner(name, address, verb, branch string) (oneKey, map[string]any) {
	own, bad := ownSigner(s, name)
	if bad != nil {
		return oneKey{}, bad
	}
	priv := s.sec.Signer()
	if priv == nil || !bytes.Equal(priv.Public().(ed25519.PublicKey), own.k.Signer) {
		return oneKey{}, refusalOf(booth.CodeViewDenied, fmt.Errorf("key %q: its cell holds no signing key for the signer its keyring generation names; nothing was recorded", own.k.Name))
	}
	// The record's point: the branch's head, which the writer's turn this
	// session holds keeps still until the commit.
	parents, err := s.branchHead(branch)
	if err != nil {
		return oneKey{}, refusalOf("carrier_unreadable", err)
	}
	// K2: writing is a live rokh.grant to the key's signer at that point,
	// covering the address and the verb (a reserved verb never is).
	var grant frame.ID
	for _, id := range s.led.ActiveGrants(parents...) {
		e, ok := s.led.Get(id)
		if !ok || e.HeadOnly {
			continue
		}
		g, err := event.DecodeGrant(e.Event.Payload)
		if err != nil || g.Open || !bytes.Equal(g.Subject, own.k.Signer) || !g.Allows(address, verb) {
			continue
		}
		grant = id
		break
	}
	if grant.IsZero() {
		return oneKey{}, refusalOf(booth.CodeViewDenied, fmt.Errorf("%q with verb %q is outside every live grant of key %q on branch %s; nothing was recorded", address, verb, own.k.Name, branch))
	}
	sess, err := s.layer.sessionAt(s.led, s.sec, s.car.Vessel().SharedKey(), parents)
	if err != nil {
		code := booth.CodeAncestryUnproven
		if errors.Is(err, key.ErrKeyNotLive) {
			code = booth.CodeKeyRevoked
		}
		return oneKey{}, refusalOf(code, fmt.Errorf("key %q has no session at the head of branch %s: %w; nothing was recorded", own.k.Name, branch, err))
	}
	s.car.SetSealer(sess)
	return oneKey{name: own.k.Name, alias: hex.EncodeToString(own.k.Key[:]), priv: priv, grant: grant}, nil
}
