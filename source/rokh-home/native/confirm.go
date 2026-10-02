package native

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	ConfirmFormat = "rokh.native-confirmation/1"
	ptrConfirm    = "native/confirm/"
)

// ErrNoConfirmation is a publication asked for with no owner's word behind it.
var ErrNoConfirmation = errors.New("native: the owner has not confirmed this disclosure for any identity")

// Confirmation is the owner's word about one disclosure and one identity,
// together.
//
// Confirming what leaves the home and confirming who signs it are one decision,
// not two: a program that could name the profile itself could take a preview
// the owner approved and publish it under a different identity of the same
// owner. So the binding names the profile and, with it, the agent key and DNA
// that profile actually had when the owner looked — and the act checks them
// again at the moment it runs.
//
// It also names the program the preview belongs to. A second program cannot
// spend another's confirmation, even for the same bytes.
type Confirmation struct {
	Format       string `json:"format"`
	Hash         string `json:"hash"`
	Profile      string `json:"profile"`
	AgentKey     string `json:"agent_key"`
	DNAHash      string `json:"dna_hash"`
	AppID        string `json:"app_id"`
	Consumer     string `json:"consumer"`
	Path         string `json:"path"`
	Version      int    `json:"version"`
	ConfirmedUTC string `json:"confirmed_utc"`
}

// Confirm seals the owner's binding of one disclosure to one identity. The
// identity is read from the profile now, not taken from the caller.
func (m *Manager) Confirm(hash, profile, consumer, path string, version int) (*Confirmation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.seal.Health(); err != nil {
		return nil, err
	}
	if hash == "" || profile == "" {
		return nil, errors.New("native: a confirmation names a disclosure and a profile")
	}
	p, err := m.loadProfile(profile)
	if err != nil {
		return nil, err
	}
	if p.AgentKey == "" || p.DNAHash == "" {
		return nil, fmt.Errorf("native: profile %s has no identity yet; start it once before confirming for it", profile)
	}
	c := &Confirmation{Format: ConfirmFormat, Hash: hash, Profile: p.ID, AgentKey: p.AgentKey,
		DNAHash: p.DNAHash, AppID: p.AppID, Consumer: consumer, Path: path, Version: version,
		ConfirmedUTC: nowUTC()}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if err := m.seal.SetPointer(ptrConfirm+hash, b); err != nil {
		return nil, err
	}
	return c, nil
}

// Confirmation reads the owner's binding for a disclosure.
func (m *Manager) Confirmation(hash string) (*Confirmation, error) {
	b, ok, err := m.seal.Pointer(ptrConfirm + hash)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNoConfirmation
	}
	var c Confirmation
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.Format != ConfirmFormat {
		return nil, fmt.Errorf("native: unsupported confirmation format %q", c.Format)
	}
	return &c, nil
}

// CheckIdentity says whether the profile still is what the owner confirmed.
// It is asked again at the moment of the act, not only when the word was given.
func (m *Manager) CheckIdentity(c *Confirmation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.loadProfile(c.Profile)
	if err != nil {
		return err
	}
	if p.AgentKey != c.AgentKey || p.DNAHash != c.DNAHash || p.AppID != c.AppID {
		return fmt.Errorf("native: the owner confirmed %s for agent %s on %s, and that profile now holds agent %s on %s",
			c.Profile, c.AgentKey, c.DNAHash, p.AgentKey, p.DNAHash)
	}
	return nil
}

// AttemptFor is the name one program's attempt has, and no other program can
// name it: the caller's own identity is part of the hash. A program may vary
// the suffix to make a second attempt of its own, and can still never ask about
// somebody else's.
func AttemptFor(hash, consumer, suffix string) string {
	h := sha256.New()
	h.Write([]byte("rokh.native-attempt/1\x00"))
	h.Write([]byte(hash))
	h.Write([]byte{0})
	h.Write([]byte(consumer))
	h.Write([]byte{0})
	h.Write([]byte(suffix))
	return "native:" + hex.EncodeToString(h.Sum(nil))[:32]
}
