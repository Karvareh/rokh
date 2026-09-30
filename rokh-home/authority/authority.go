// Package authority is who may do what in a home, and why: the registry of
// the programs a home serves, the grants each holds, and the decision a gate
// asks for before every sensitive read and every effect.
//
// Eight actions, each its own right; holding one never implies another:
//
//	read    names, versions, provenance — what exists here and whose it is
//	search  lexical search over a scope
//	bytes   the content itself
//	draft   open and edit drafts
//	record  record a draft or an import as a new version: a signed event
//	export  hand files out of the home to a destination
//	share   prepare a disclosure to a peer; the owner confirms it
//	ledger  receipts at the program's own namespace: bind, intent, outcome
//
// A decision answers who asked, for which action, on what, under which grant
// and at which version of that program's authority — and when it refuses, it
// says why without saying whether the thing asked for exists.
//
// A program's identity is established by the gate — the connection it was
// handed, or the credential it presents — never by a name it claims, a
// process number, or a namespace it declared.
package authority

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Format names the registry's encoding.
const Format = "rokh-home.registry/1"

// Action is one right.
type Action string

const (
	Read   Action = "read"
	Search Action = "search"
	Bytes  Action = "bytes"
	Draft  Action = "draft"
	Record Action = "record"
	Export Action = "export"
	Share  Action = "share"
	Ledger Action = "ledger"
)

// Actions is every action, in one order.
var Actions = []Action{Read, Search, Bytes, Draft, Record, Export, Share, Ledger}

// Valid says whether the action is one of the eight.
func (a Action) Valid() bool {
	for _, x := range Actions {
		if a == x {
			return true
		}
	}
	return false
}

// WholeHome is the scope that covers every path.
const WholeHome = "/"

// Why a decision came out as it did.
const (
	Allowed         = "allowed"
	NoGrant         = "no_grant"
	ConsumerRevoked = "consumer_revoked"
	StaleSession    = "stale_session"
	UnknownConsumer = "unknown_consumer"
	BadPath         = "bad_path"
	BadAction       = "bad_action"
)

// Grant is one right over one scope.
type Grant struct {
	ID     string `json:"id"`
	Action Action `json:"action"`
	Scope  string `json:"scope"`
}

// Consumer is a program the home serves.
type Consumer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Verifier is HMAC-SHA256 of the program's credential under the registry
	// key; the credential itself is shown once, when it is issued.
	Verifier string  `json:"verifier"`
	Grants   []Grant `json:"grants"`
	// Version is the version of this program's authority. Every change to its
	// grants or its standing moves it, and a session opened under an older
	// version is refused until the program says hello again.
	Version uint64 `json:"version"`
	Revoked bool   `json:"revoked"`
	// The key this program records with, and the ledger grant it signs under.
	KeyName   string `json:"key_name,omitempty"`
	KeyPublic string `json:"key_public,omitempty"`
	RokhGrant string `json:"rokh_grant,omitempty"`
}

// Registry is every program a home serves.
type Registry struct {
	Format    string               `json:"format"`
	Consumers map[string]*Consumer `json:"consumers"`
}

// Decision is the answer to "may this program do this, here, now?"
type Decision struct {
	Allowed          bool   `json:"allowed"`
	Consumer         string `json:"consumer"`
	Name             string `json:"name,omitempty"`
	Action           Action `json:"action"`
	Path             string `json:"path"`
	Grant            string `json:"grant,omitempty"`
	AuthorityVersion uint64 `json:"authority_version"`
	Reason           string `json:"reason"`
}

// New is an empty registry.
func New() *Registry {
	return &Registry{Format: Format, Consumers: map[string]*Consumer{}}
}

// NewID is a random identifier.
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// NewCredential is a fresh credential: 32 random bytes.
func NewCredential() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// Verifier is what the registry keeps of a credential.
func Verifier(key, credential []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("rokh-home/1/credential\x00"))
	m.Write(credential)
	return hex.EncodeToString(m.Sum(nil))
}

// CleanPath checks a path in the home's view: "/"-separated, no empty, "." or
// ".." segment, valid UTF-8, no NUL. WholeHome is itself a path.
func CleanPath(p string) (string, error) {
	if p == WholeHome {
		return p, nil
	}
	if p == "" || len(p) > 4096 || !utf8.ValidString(p) || strings.ContainsRune(p, 0) {
		return "", errors.New("authority: not a path")
	}
	if strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return "", errors.New("authority: a path has no leading or trailing slash")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("authority: bad path segment %q", seg)
		}
	}
	return p, nil
}

// Covers says whether a scope covers a path. A scope covers itself and what is
// beneath it, segment by segment: "a/b" covers "a/b" and "a/b/c", and not
// "a/bc". WholeHome covers everything and nothing else covers WholeHome.
func Covers(scope, path string) bool {
	if scope == WholeHome {
		return true
	}
	if path == WholeHome {
		return false
	}
	return path == scope || strings.HasPrefix(path, scope+"/")
}

// Add registers a program and issues its credential, which is returned once.
func (r *Registry) Add(name, kind string, key []byte) (*Consumer, []byte, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 {
		return nil, nil, errors.New("authority: a program needs a name of 1 to 128 bytes")
	}
	for _, c := range r.Consumers {
		if c.Name == name && !c.Revoked {
			return nil, nil, fmt.Errorf("authority: a program named %q is already registered", name)
		}
	}
	cred := NewCredential()
	c := &Consumer{ID: NewID(), Name: name, Kind: kind, Verifier: Verifier(key, cred), Version: 1}
	r.Consumers[c.ID] = c
	return c, cred, nil
}

// Get finds a program by identifier.
func (r *Registry) Get(id string) (*Consumer, bool) {
	c, ok := r.Consumers[id]
	return c, ok
}

// Grant gives a program one action over one scope.
func (r *Registry) Grant(id string, action Action, scope string) (Grant, error) {
	c, ok := r.Consumers[id]
	if !ok {
		return Grant{}, errors.New("authority: no such program")
	}
	if c.Revoked {
		return Grant{}, errors.New("authority: that program's standing was withdrawn")
	}
	if !action.Valid() {
		return Grant{}, fmt.Errorf("authority: %q is not an action", action)
	}
	clean, err := CleanPath(scope)
	if err != nil {
		return Grant{}, err
	}
	for _, g := range c.Grants {
		if g.Action == action && g.Scope == clean {
			return g, nil
		}
	}
	g := Grant{ID: NewID(), Action: action, Scope: clean}
	c.Grants = append(c.Grants, g)
	sort.Slice(c.Grants, func(i, j int) bool {
		if c.Grants[i].Action != c.Grants[j].Action {
			return c.Grants[i].Action < c.Grants[j].Action
		}
		return c.Grants[i].Scope < c.Grants[j].Scope
	})
	c.Version++
	return g, nil
}

// Ungrant withdraws one grant. What was done under it stays done.
func (r *Registry) Ungrant(id, grantID string) error {
	c, ok := r.Consumers[id]
	if !ok {
		return errors.New("authority: no such program")
	}
	for i, g := range c.Grants {
		if g.ID == grantID {
			c.Grants = append(c.Grants[:i], c.Grants[i+1:]...)
			c.Version++
			return nil
		}
	}
	return errors.New("authority: no such grant")
}

// Revoke withdraws a program's standing altogether. Its credential stops
// working, every session it holds is refused, and what it recorded before
// stays recorded.
func (r *Registry) Revoke(id string) error {
	c, ok := r.Consumers[id]
	if !ok {
		return errors.New("authority: no such program")
	}
	c.Revoked = true
	c.Version++
	return nil
}

// Authenticate finds the program a credential belongs to. Every verifier is
// compared, so how long it takes says nothing about which one matched.
func (r *Registry) Authenticate(key, credential []byte) (*Consumer, bool) {
	want := Verifier(key, credential)
	var found *Consumer
	ids := make([]string, 0, len(r.Consumers))
	for id := range r.Consumers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		c := r.Consumers[id]
		if hmac.Equal([]byte(c.Verifier), []byte(want)) && found == nil {
			found = c
		}
	}
	if found == nil || found.Revoked {
		return nil, false
	}
	return found, true
}

// Decide answers whether a program, in a session opened at a version of its
// authority, may take an action on a path.
func (r *Registry) Decide(id string, session uint64, action Action, path string) Decision {
	d := Decision{Consumer: id, Action: action, Path: path}
	c, ok := r.Consumers[id]
	if !ok {
		d.Reason = UnknownConsumer
		return d
	}
	d.Name, d.AuthorityVersion = c.Name, c.Version
	switch {
	case c.Revoked:
		d.Reason = ConsumerRevoked
		return d
	case session != c.Version:
		d.Reason = StaleSession
		return d
	case !action.Valid():
		d.Reason = BadAction
		return d
	}
	clean, err := CleanPath(path)
	if err != nil {
		d.Reason = BadPath
		return d
	}
	d.Path = clean
	for _, g := range c.Grants {
		if g.Action == action && Covers(g.Scope, clean) {
			d.Allowed, d.Grant, d.Reason = true, g.ID, Allowed
			return d
		}
	}
	d.Reason = NoGrant
	return d
}

// Scopes lists the scopes a program holds an action over, for filtering a
// listing before it is built.
func (r *Registry) Scopes(id string, action Action) []string {
	c, ok := r.Consumers[id]
	if !ok || c.Revoked {
		return nil
	}
	var out []string
	for _, g := range c.Grants {
		if g.Action == action {
			out = append(out, g.Scope)
		}
	}
	return out
}

// Check validates a registry read back from storage.
func (r *Registry) Check() error {
	if r.Format != Format {
		return fmt.Errorf("authority: unknown registry format %q", r.Format)
	}
	for id, c := range r.Consumers {
		if c.ID != id || c.Name == "" || len(c.Verifier) != 64 {
			return fmt.Errorf("authority: program %s is malformed", id)
		}
		for _, g := range c.Grants {
			if !g.Action.Valid() {
				return fmt.Errorf("authority: program %s holds an unknown action", id)
			}
			if _, err := CleanPath(g.Scope); err != nil {
				return fmt.Errorf("authority: program %s holds a bad scope: %v", id, err)
			}
		}
	}
	return nil
}
