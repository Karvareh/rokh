package native

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// PublishInput is exactly the confirmed disclosure, and nothing else.
//
// The gate builds it from the preview the owner confirmed; a program names a
// disclosure by its hash and supplies none of these fields. Version is the
// hash of Body and ExcerptSHA256 the hash of Excerpt, which the zome's own
// validation recomputes — so nothing but that preview can be published under
// this call.
type PublishInput struct {
	Profile       string `json:"profile"`
	ItemID        string `json:"item_id"`
	Title         string `json:"title"`
	Version       string `json:"version"`
	Body          []byte `json:"-"`
	Excerpt       []byte `json:"-"`
	ExcerptSHA256 string `json:"excerpt_sha256"`
	Attempt       string `json:"attempt"`
	RokhIntent    string `json:"rokh_intent"`
}

// PublishResult is a typed ending, never a guess. Record is one of
// "recorded", "not-recorded" or "unknown"; "unknown" means the answer was lost
// and the chain has to be asked.
type PublishResult struct {
	Record     string `json:"record"`
	Existing   bool   `json:"existing"`
	Manuscript string `json:"manuscript,omitempty"`
	BodyAction string `json:"body_action,omitempty"`
	Excerpt    string `json:"excerpt,omitempty"`
	Attempt    string `json:"attempt"`
	Error      string `json:"error,omitempty"`
	// Code is the conductor's own code for a refusal, kept so an operator reads
	// a name rather than a sentence.
	Code string         `json:"code,omitempty"`
	Raw  map[string]any `json:"raw,omitempty"`
}

// Publish puts one confirmed disclosure on this identity's source chain: the
// whole body as a private entry, the excerpt and the announcement in public.
//
// Everything sensitive travels down the bridge's pipe. A lost answer is
// reported as "unknown", and the caller asks the chain with Attempt rather
// than sending the call again blindly.
func (m *Manager) Publish(in PublishInput) (PublishResult, error) {
	res := PublishResult{Record: "not-recorded", Attempt: in.Attempt}
	if in.Attempt == "" {
		return res, errors.New("native: a publication is made under a named attempt")
	}
	payload := map[string]any{
		"item_id": in.ItemID, "version": in.Version, "title": in.Title,
		"body": byteList(in.Body), "excerpt": byteList(in.Excerpt),
		"attempt": in.Attempt, "excerpt_sha256": in.ExcerptSHA256,
	}
	if in.RokhIntent != "" {
		payload["rokh_intent"] = in.RokhIntent
	}
	out, err := m.Zome(in.Profile, m.bin.Manifest.Functions.Publish, payload, 180*time.Second)
	if err != nil {
		res.Error = bounded(err.Error())
		var door *doorError
		if errors.As(err, &door) {
			res.Code = door.Code
		}
		res.Record = recordOf(err)
		return res, nil
	}
	m2, ok := out.(map[string]any)
	if !ok {
		res.Record = "unknown"
		res.Error = "the conductor's answer was not a record"
		return res, nil
	}
	res.Existing, _ = m2["existing"].(bool)
	res.Manuscript, res.BodyAction, res.Excerpt = holoB64(m2["manuscript"]), holoB64(m2["body_action"]), holoB64(m2["excerpt"])
	res.Raw = map[string]any{"existing": res.Existing}
	// An answer shaped like a success that names no action is not one. The
	// three names are what a later reader has to hold on to.
	if res.Manuscript == "" || res.BodyAction == "" || res.Excerpt == "" {
		res.Record = "unknown"
		res.Error = "the conductor's answer named no manuscript, body or excerpt"
		return res, nil
	}
	res.Record = "recorded"
	return res, nil
}

// bounded keeps a refusal's text to a size two places can carry.
//
// This text goes two ways that neither the bridge nor the conductor knows
// about: back to the program as part of the answer, and into the home's ledger
// as the receipt's saying. A receipt's payload is capped at four kilobytes, so
// an unbounded error makes the outcome itself unrecordable and leaves the
// intent open; and whatever the text happens to quote is handed to a program
// that was never shown anything but the preview. The code beside it is what an
// operator reads; this is the rest, cut.
const maxErrorText = 512

func bounded(s string) string {
	if len(s) <= maxErrorText {
		return s
	}
	cut := maxErrorText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "… (cut)"
}

// recordOf says what is known about the chain after a call that failed, and
// never more than that.
//
// Only a call that never left this process is reported as "not recorded".
// Once the request has crossed the pipe, a refusal, a lost answer and a commit
// whose answer could not be read all look the same from here — and two of the
// zome's own refusals mean the opposite of "nothing happened": attempt_conflict
// and attempt_incomplete both say the attempt is already on the chain. The gate
// writes this word into the home's ledger as the receipt's outcome, so a guess
// here becomes a false ending there. The ending is settled by asking, with
// FindAttempt, not by reading an error string.
//
// If the bridge's codes ever name refusals that are certainly before the
// commit, they belong in this switch by name; nothing in this package can tell
// them apart today.
func recordOf(err error) string {
	if errors.Is(err, ErrNotLive) || errors.Is(err, ErrNotSent) || errors.Is(err, ErrClosing) {
		return "not-recorded"
	}
	return "unknown"
}

func byteList(b []byte) []int {
	out := make([]int, len(b))
	for i, x := range b {
		out[i] = int(x)
	}
	return out
}

// readOnly are the zome functions the owner may ask for through the gate.
// Everything that writes to the chain goes through Publish, behind a
// disclosure the owner confirmed.
// The reading functions are named by the engine manifest (Funcs).

// Observe asks one of the reading zome functions. It writes nothing.
func (m *Manager) Observe(profile, fn string, payload any) (any, error) {
	if !listed(m.bin.Manifest.Functions.Read, fn) {
		return nil, fmt.Errorf("native: %q is not a reading function", fn)
	}
	return m.Zome(profile, fn, payload, 120*time.Second)
}

// capabilityOps are the two zome functions that change what a peer may ask this
// agent for. They are the owner's, like everything else that writes: a
// capability the conductor holds is not a grant over home content, and a grant
// over the conductor is not a program's to make or to withdraw.
// The capability functions are named by the engine manifest (Funcs).

// Capability grants or withdraws a peer's access to this agent's granted
// function. The secret never leaves the two conductors: the zome hands it to
// the assignee over the network and keeps it out of every answer.
func (m *Manager) Capability(profile, fn string, payload any) (any, error) {
	if !listed(m.bin.Manifest.Functions.Capability, fn) {
		return nil, fmt.Errorf("native: %q is not a capability function", fn)
	}
	return m.Zome(profile, fn, payload, 120*time.Second)
}

// peerOps are the zome functions a reviewing home performs on its own chain
// about content a peer disclosed to it.
//
// They are named one by one, and they are the owner's alone. Three of them
// write: the standing word about which agent may hand this one a claim, the
// review itself, and — on the far side — the claim a deliberate lend plants on
// an agent that already said it would take one. None of them is a door onto the
// zome: `accept_excerpt_grant` is deliberately not here, because it is the
// unrestricted door a peer calls, not an act this home performs on itself.
// The peer functions are named by the engine manifest (Funcs).

// Peer performs one of those acts. Whether this home should perform it is the
// owner's decision, made before the call; what the conductor will allow is the
// conductor's, made inside it.
func (m *Manager) Peer(profile, fn string, payload any) (any, error) {
	if !listed(m.bin.Manifest.Functions.Peer, fn) {
		return nil, fmt.Errorf("native: %q is not one of this home's own peer acts", fn)
	}
	return m.Zome(profile, fn, payload, 180*time.Second)
}

// adminOps are the conductor admin requests the owner may ask for through the
// gate. A program reaches none of them: the Admin API is the broker's, and a
// capability it holds there is not a grant over anything in the home.
// The admin requests are named by the engine manifest (Funcs).

// Admin runs one allowed admin request against a live conductor.
func (m *Manager) Admin(profile string, args []string) (string, error) {
	if len(args) == 0 || !listed(m.bin.Manifest.Functions.Admin, args[0]) {
		return "", fmt.Errorf("native: %q is not an admin request the gate passes on", firstOrEmpty(args))
	}
	m.mu.Lock()
	s := m.live[profile]
	m.mu.Unlock()
	if s == nil {
		return "", ErrNotLive
	}
	return m.admin(s.dir, s.port, s.origin, args...)
}

func firstOrEmpty(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// FindAttempt is the read-only question a lost answer is settled with.
func (m *Manager) FindAttempt(profile, attempt string) (string, any, error) {
	out, err := m.Observe(profile, m.bin.Manifest.Functions.FindAttempt, map[string]any{"attempt": attempt})
	if err != nil {
		return "unknown", nil, err
	}
	if out == nil {
		return "not-recorded", nil, nil
	}
	return "recorded", out, nil
}
