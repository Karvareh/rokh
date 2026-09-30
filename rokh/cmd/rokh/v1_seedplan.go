package main

// The key layer's part of `rokh seed` (contract 4.7, S1 and S2), and of what
// a side of `rokh reconcile` does after the union: the cells it installs
// (4.8, R2; learnCells) and the heads and concurrent keys it shows (R7;
// boundaryOf).
//
// The owner gives: the passphrase must open the owner's cell, and the root's
// signing key must be in it (S1). The give is three kinds of system event,
// chained and recorded in one commit on the source: the keyring add of the
// seed's key (a reader and a signer, reading nothing, with the system reader
// sealed to it), one rokh.grant to its signer per scope, and the rokh.seed
// give. Each is judged by the source's ledger before it is handed on, so an
// event the ledger would refuse is never recorded. The take is signed by the
// seed's key once the new vessel's id is known, under the first of those
// grants, with the give in its causal past: on the give itself, or, for a
// give used again after the source moved on, on the source's branch as it is
// now (takePoint).
//
// The seed's key reads nothing. Nobody but the owner, who reads everything,
// holds it; and a key that reads is named in every envelope sealed where it
// reads, and an envelope names at most 32 readers (key.MaxReaders), so each
// whole seed would bring every later envelope one reader nearer that limit,
// until nothing could be sealed any more, not even a revoke.
//
// The seed's key is derived from the root's signing seed, the anchor and the
// seed id (HKDF-SHA256): only the holder of the root derives it, and the same
// seed id gives the same key. Its add carries no slot and no cell of it is
// installed anywhere, so no passphrase opens it; it acts only through the
// owner, who derives it.
//
// Which seed it is: the one DST already holds, whose give the source must
// hold, unless DST holds no event yet and the seed was planned on this source
// or by the attempt named (a command cut after it made the folder and before
// its give, which is given now); else the one an attempt's name derives; else
// a new one planned on this source (plannedSeed). A give the source holds
// already is used again with its own keys and nothing of it is signed again
// (goal 7.41): the take is signed with the key derived again, under the grant
// the give put in its past. A DST that holds the seed keeps its own key; its
// cells too, unless its give is made now, when they are made again for that
// give (remadeSeedKeys).
//
// The new vessel's key is 32 new random bytes. Its first cell is the owner's:
// the owner's own secret under the same passphrase (the salt and the cost are
// the rokh's, inherited), so the owner's passphrase opens the seed and shows
// all of it (axiom 19). Then, by a ruling of the design (axiom 19,
// "on seeds it is the same"; contract 4.6), the cell of every key generation
// the seed takes: live at the give, its add carrying a slot, and for a slice
// touching one of its scopes (seedKeyCells). A key's passphrase then opens
// the seed and shows the same view there. The vessel's sealer seals as the
// source's keyring stands at the give: to the owner's generations, to every
// key whose reads cover the address, and to the system reader at the system
// address (E3, E4).

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/ledger"
	"rokh/lineage"
	"rokh/vessel"
)

func init() {
	v1SeedPlan = seedPlan
	v1Cells = learnCells
	v1Boundary = boundaryOf
}

// seedPlan is v1SeedPlan.
func seedPlan(src *carrier.Carrier, pass string, ask SeedAsk) (lineage.SeedPlan, SeedKeys, error) {
	v := src.Vessel()
	info := v.Info()
	sec, _, err := key.Try(pass, v.Slots(), info.Salt, info.Iter)
	if err != nil {
		return lineage.SeedPlan{}, SeedKeys{}, err
	}
	if sec.Key != ([32]byte{}) {
		return lineage.SeedPlan{}, SeedKeys{}, fmt.Errorf("%w: only the owner gives a seed (S1); this passphrase opens a key's cell", lineage.ErrSeedRefused)
	}
	root := sec.Signer()
	if root == nil {
		return lineage.SeedPlan{}, SeedKeys{}, fmt.Errorf("%w: the root's signing key is in cold custody, and only the root gives a seed (S1)", lineage.ErrSeedRefused)
	}
	owner, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		return lineage.SeedPlan{}, SeedKeys{}, err
	}
	if held, err := holdsEvents(src); err != nil {
		return lineage.SeedPlan{}, SeedKeys{}, err
	} else if !held {
		return lineage.SeedPlan{}, SeedKeys{}, fmt.Errorf("%w: the source %w, and gives no seed until it holds its lineage", lineage.ErrSeedRefused, errNoEventYet)
	}
	l, err := sourceLedger(src)
	if err != nil {
		return lineage.SeedPlan{}, SeedKeys{}, err
	}
	if !bytes.Equal(l.Root(), root.Public().(ed25519.PublicKey)) {
		return lineage.SeedPlan{}, SeedKeys{}, fmt.Errorf("%w: the root in this passphrase's cell is not this rokh's root", lineage.ErrSeedRefused)
	}
	if err := scopesInside(ask.Scopes, info.Scopes); err != nil {
		return lineage.SeedPlan{}, SeedKeys{}, err
	}
	gives, doubled := givesOf(l)
	seed, err := chooseSeed(root, src.Anchor(), info.Vessel, ask, gives, doubled)
	if err != nil {
		return lineage.SeedPlan{}, SeedKeys{}, err
	}
	sk, err := deriveSeedKey(root, src.Anchor(), seed)
	if err != nil {
		return lineage.SeedPlan{}, SeedKeys{}, err
	}
	sys := systemReaderOf(l, owner)
	var g giving
	var notes []string
	gid, given := gives[seed]
	if given {
		g, err = reuseGiving(l, gid, sk, ask.Scopes)
	} else {
		var parents []frame.ID
		parents, notes, err = giveParents(l, src)
		if err != nil {
			return lineage.SeedPlan{}, SeedKeys{}, err
		}
		g, err = newGiving(l, root, sk, sys, event.Seed{Op: event.SeedGive, Seed: seed, Key: sk.id, Scopes: ask.Scopes, Source: info.Seed}, parents)
	}
	if err != nil {
		return lineage.SeedPlan{}, SeedKeys{}, err
	}
	// The points the seed's key cells are judged at: a give used again, and
	// for a give made now, the give and every head it leaves apart (so a key
	// live anywhere on the source takes its cell, as it does on the source).
	at := []frame.ID{g.give}
	if !given {
		at = l.Heads()
	}
	// Where the take is signed: the give made now, or, for a give used again,
	// the source's branch as it is now (takePoint).
	point := []frame.ID{g.give}
	if given {
		point = takePoint(l, src, g.give, ask.Scopes)
	}
	var keys SeedKeys
	if h := ask.Held; h == nil {
		keys, err = newSeedKeys(pass, info, sec, owner, sys, l, at, point, ask.Scopes)
		if err == nil && ask.Attempt == "" {
			// A planned seed's folder is made with the vessel id its seed
			// names (seedVessel).
			keys.Vessel, err = seedVessel(root, src.Anchor(), seed)
			keys.Earlier = earlierGive(l, info.Seed, ask.Scopes, gives)
		}
	} else {
		// The folder names this seed: its give is made now, or it is used
		// again and the folder goes on from where its command stopped. Either
		// way its cells are judged again now.
		keys, err = heldSeedKeys(pass, info, sec, owner, sys, l, at, point, ask.Scopes, h)
		if err == nil && ask.Attempt == "" {
			keys.Refused, err = foreignFolder(root, src.Anchor(), h)
		}
	}
	if err != nil {
		return lineage.SeedPlan{}, SeedKeys{}, err
	}
	keys.Notes = append(append(keys.Notes, doubledNotes(doubled)...), notes...)
	return lineage.SeedPlan{Seed: seed, Scopes: ask.Scopes, Give: g.events, Take: takeOf(l, sk, g, point), Branch: defaultBranch}, keys, nil
}

// takePoint is the point a take of a give used again is signed at (S1: the
// give in its causal past). The source may have moved on since its give was
// recorded (a write, a key added or taken back) and the copy brings all of
// it; a take on the give alone would leave the seed's own branch behind what
// it holds, and what the owner then writes on the seed would not have in its
// past what the source recorded since, a revoke among it (K4). So the take
// goes on from the source's branch head: its parents are the newest events
// of that head's past that the copy brings (for a slice, its scopes and
// their necessary lineage, as lineage.offer reads them). The seed's branch
// then goes on from the source's as the source's branch did, and heads the
// source holds apart stay apart (they are named beside it). When the branch
// head does not have the give in its past, or no such point exists, the take
// is signed on the give alone, as for a give made now.
func takePoint(l *ledger.Ledger, src *carrier.Carrier, give frame.ID, scopes []string) []frame.ID {
	alone := []frame.ID{give}
	tip, ok, err := src.Ref(defaultBranch)
	if err != nil || !ok || l.State(tip) != ledger.Accepted {
		return alone
	}
	past := map[frame.ID]bool{}
	for _, id := range l.CausalPast(tip) {
		past[id] = true
	}
	if !past[give] {
		return alone
	}
	// What the copy brings: every accepted event it carries whole (in scope,
	// or a system event; everything for a whole seed), with its ancestors.
	var whole []frame.ID
	for _, id := range l.Order() {
		e, ok := l.Get(id)
		if !ok || e.HeadOnly {
			continue
		}
		if len(scopes) == 0 || e.System || lineage.Within(e.Event.Address, scopes) {
			whole = append(whole, id)
		}
	}
	brought := map[frame.ID]bool{}
	for _, id := range l.CausalPast(whole...) {
		if past[id] {
			brought[id] = true
		}
	}
	// The newest of them: none of the others names it as a parent.
	parent := map[frame.ID]bool{}
	for id := range brought {
		e, _ := l.Get(id)
		for _, p := range e.Event.Parents {
			parent[p] = true
		}
	}
	var point []frame.ID
	for id := range brought {
		if !parent[id] {
			point = append(point, id)
		}
	}
	if len(point) == 0 || len(point) > event.MaxParents {
		return alone
	}
	sort.Slice(point, func(i, j int) bool { return point[i].Compare(point[j]) < 0 })
	return point
}

// chooseSeed says which seed this is, given the source vessel's own id. A DST
// that holds a seed names it, and the source must hold its give, unless DST
// holds no event yet and its seed was planned on this source (plannedHere),
// or is the seed of the attempt named: then the command that made the folder
// was cut before its give, and the give is made now, for that seed. An
// attempt names the seed its name derives on this source. Otherwise the seed
// is new, planned on this source. A seed the source holds two or more gives
// of is never gone on with (doubled): which give a take names is not for
// this command to choose.
func chooseSeed(root ed25519.PrivateKey, anchor, source frame.ID, ask SeedAsk, gives map[frame.ID]frame.ID, doubled map[frame.ID][]frame.ID) (frame.ID, error) {
	var named frame.ID
	if ask.Attempt != "" {
		var err error
		if named, err = attemptSeed(root, anchor, source, ask.Attempt); err != nil {
			return frame.Zero, err
		}
	}
	if h := ask.Held; h != nil {
		if ask.Attempt != "" && named != h.Seed {
			hint := ""
			if _, given := gives[h.Seed]; given {
				hint = "; the same command without --attempt goes on with the seed the folder holds"
			}
			return frame.Zero, fmt.Errorf("%w: the folder holds seed %s, not the seed of attempt %q on this source%s", lineage.ErrSeedRefused, h.Seed.Short(), ask.Attempt, hint)
		}
		if ids, two := doubled[h.Seed]; two {
			return frame.Zero, fmt.Errorf("%w: the source holds %d gives of seed %s (%s); that seed is never gone on with, and a new folder takes a new seed", lineage.ErrSeedRefused, len(ids), h.Seed.Short(), shortIDs(ids))
		}
		if _, given := gives[h.Seed]; !given {
			switch {
			case !h.Empty:
				return frame.Zero, fmt.Errorf("%w: the folder holds seed %s, whose give this source does not hold; rokh reconcile brings two seeds together", lineage.ErrSeedRefused, h.Seed.Short())
			case ask.Attempt == "" && !plannedHere(root, anchor, source, h.Seed):
				return frame.Zero, fmt.Errorf("%w: the folder names seed %s and holds no event, and that seed was not planned on this source: its give is not made here (the source it was planned on, or the same --attempt, goes on with it)", lineage.ErrSeedRefused, h.Seed.Short())
			}
		}
		if !sameScopes(h.Scopes, ask.Scopes) {
			return frame.Zero, fmt.Errorf("%w: the folder holds a seed of %s, and this asks for %s", lineage.ErrSeedRefused, scopesText(h.Scopes), scopesText(ask.Scopes))
		}
		return h.Seed, nil
	}
	if ask.Attempt != "" {
		if ids, two := doubled[named]; two {
			return frame.Zero, fmt.Errorf("%w: the source holds %d gives of the seed of attempt %q, %s (%s); that seed is never gone on with: another attempt's name gives a new seed", lineage.ErrSeedRefused, len(ids), ask.Attempt, named.Short(), shortIDs(ids))
		}
		return named, nil
	}
	return plannedSeed(root, anchor, source)
}

// plannedSeed is a new seed's id when no attempt names it: 16 random bytes
// and a tag of 16 that binds them to this root, this anchor and the source
// vessel the seed is given from. The folder is made, and names its seed,
// before the give is recorded (goal 7.41); a command cut between the two
// finds the seed in the folder when it runs again, and the tag says that it
// was planned on this source, so its give is made here and on no other source
// of the rokh. Nobody without the root tells the id from 32 random bytes.
func plannedSeed(root ed25519.PrivateKey, anchor, source frame.ID) (frame.ID, error) {
	var id frame.ID
	if _, err := rand.Read(id[:16]); err != nil {
		return frame.Zero, err
	}
	tag, err := plannedTag(root, anchor, source, id[:16])
	copy(id[16:], tag)
	return id, err
}

// plannedHere says whether a seed id was planned on this source by this root
// (plannedSeed).
func plannedHere(root ed25519.PrivateKey, anchor, source, seed frame.ID) bool {
	tag, err := plannedTag(root, anchor, source, seed[:16])
	return err == nil && hmac.Equal(tag, seed[16:])
}

func plannedTag(root ed25519.PrivateKey, anchor, source frame.ID, nonce []byte) ([]byte, error) {
	return hkdf.Key(sha256.New, root.Seed(), anchor[:], "rokh/seed/1/planned/"+string(source[:])+string(nonce), 16)
}

// earlierGive is the latest give from this source vessel (its give names the
// source's own seed id, or none for the root), of these scopes, whose seed's
// key is live and whose take this source's ledger does not hold. A folder
// lost after its give leaves exactly such a give; a seed that was taken and
// never met this source again looks the same from here, so the answer that
// names it says only what is known.
func earlierGive(l *ledger.Ledger, source frame.ID, scopes []string, gives map[frame.ID]frame.ID) EarlierGive {
	taken := map[frame.ID]bool{}
	for _, id := range l.Order() {
		e, ok := l.Get(id)
		if !ok || e.HeadOnly || e.Event.Verb != event.VerbSeed {
			continue
		}
		if s, err := event.DecodeSeed(e.Event.Payload); err == nil && s.Op == event.SeedTake {
			taken[frame.ID(s.Seed)] = true
		}
	}
	liveKeys := map[[32]byte]bool{}
	adds, _ := l.Keyring()
	for _, id := range adds {
		if e, ok := l.Get(id); ok && !e.HeadOnly {
			if k, err := event.DecodeKeyring(e.Event.Payload); err == nil {
				liveKeys[k.Key] = true
			}
		}
	}
	var out EarlierGive
	at := -1
	for seed, gid := range gives {
		e, ok := l.Get(gid)
		if !ok || e.HeadOnly || taken[seed] {
			continue
		}
		s, err := event.DecodeSeed(e.Event.Payload)
		if err != nil || frame.ID(s.Source) != source || !sameScopes(s.Scopes, scopes) || !liveKeys[s.Key] {
			continue
		}
		if p, ok := l.Position(gid); ok && p > at {
			at, out = p, EarlierGive{Seed: seed, Give: gid}
		}
	}
	return out
}

// seedVessel is the vessel id of a planned seed's folder: derived from the
// root's signing seed, the anchor and the seed id (HKDF-SHA256), so that a
// folder is bound to the seed it was made for. The seed's marker in a folder
// is written by whoever holds its vessel key, a key's holder among them; the
// vessel id is not (the vessel keeps it whatever a commit's root edit says),
// and nobody without the root derives it, or tells it from 32 random bytes.
func seedVessel(root ed25519.PrivateKey, anchor, seed frame.ID) (frame.ID, error) {
	b, err := hkdf.Key(sha256.New, root.Seed(), anchor[:], "rokh/seed/1/vessel/"+string(seed[:]), 32)
	var id frame.ID
	copy(id[:], b)
	return id, err
}

// foreignFolder says why a folder DST holds, which names a seed, is not gone
// on with when no --attempt names that seed, or nil. A planned seed's folder
// is made with the vessel id its seed names (seedVessel); a folder whose
// vessel is another names the seed by a marker written after it was made
// (the id of a seed given, and taken elsewhere, among them), and taking the
// seed there would give it a second vessel. An attempt's seed is the owner's
// own name for the act, and a lost folder of it is made anew by design: the
// rule is not asked of it. The refusal stands only while the folder has not
// taken the seed (goOnHeld): a folder that took it records nothing (S5).
func foreignFolder(root ed25519.PrivateKey, anchor frame.ID, h *SeedHeld) (refusal error, err error) {
	want, err := seedVessel(root, anchor, h.Seed)
	if err != nil {
		return nil, err
	}
	if h.Vessel != want {
		return fmt.Errorf("%w: the folder names seed %s, and its vessel is not the one made for that seed (a folder names its seed when it is made; this one's marker was written since, or the folder is an --attempt's, which that --attempt goes on with)", lineage.ErrSeedRefused, h.Seed.Short()), nil
	}
	return nil, nil
}

// attemptSeed is the seed id an attempt names on one source vessel (contract
// section 6): the same root, anchor, source vessel and name give the same id,
// and nobody without the root tells it from 32 random bytes. The source is
// bound in: one name used on two vessels of the rokh names two seeds, so
// no seed id is ever given twice by two sources that both hold the name.
func attemptSeed(root ed25519.PrivateKey, anchor, source frame.ID, attempt string) (frame.ID, error) {
	b, err := hkdf.Key(sha256.New, root.Seed(), anchor[:], "rokh/seed/1/attempt-of/"+string(source[:])+attempt, 32)
	var id frame.ID
	copy(id[:], b)
	return id, err
}

// givesOf maps each seed id the source holds one give of to that give. A
// seed id the source holds two or more gives of (two vessels that gave one
// attempt's seed before attempts were bound to their source, or a forgery)
// is not in it: it is in doubled, with its gives. Nothing removes an event,
// so such a seed stays doubled; it is shown, it is never gone on with, and
// every other seed is given as ever.
func givesOf(l *ledger.Ledger) (map[frame.ID]frame.ID, map[frame.ID][]frame.ID) {
	all := map[frame.ID][]frame.ID{}
	_, seeds := l.Keyring()
	for _, id := range seeds {
		e, ok := l.Get(id)
		if !ok || e.HeadOnly {
			continue
		}
		s, err := event.DecodeSeed(e.Event.Payload)
		if err != nil || s.Op != event.SeedGive {
			continue
		}
		all[s.Seed] = append(all[s.Seed], id)
	}
	gives, doubled := map[frame.ID]frame.ID{}, map[frame.ID][]frame.ID{}
	for seed, ids := range all {
		if len(ids) == 1 {
			gives[seed] = ids[0]
			continue
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i].Compare(ids[j]) < 0 })
		doubled[seed] = ids
	}
	return gives, doubled
}

// doubledNotes are the lines that show, beside the answer, every seed the
// source holds more than one give of.
func doubledNotes(doubled map[frame.ID][]frame.ID) []string {
	seeds := make([]frame.ID, 0, len(doubled))
	for s := range doubled {
		seeds = append(seeds, s)
	}
	sort.Slice(seeds, func(i, j int) bool { return seeds[i].Compare(seeds[j]) < 0 })
	var out []string
	for _, s := range seeds {
		out = append(out, fmt.Sprintf("the source holds %d gives of seed %s (%s); that seed is never gone on with", len(doubled[s]), s.Short(), shortIDs(doubled[s])))
	}
	return out
}

// shortIDs is a list of event ids as a person reads them.
func shortIDs(ids []frame.ID) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.Short()
	}
	return strings.Join(out, ", ")
}

// scopesInside refuses, before anything is recorded, a seed whose scopes do
// not lie inside the source's own vessel scopes (S1). The whole rokh lies
// inside no slice.
func scopesInside(asked, holds []string) error {
	if len(holds) == 0 {
		return nil
	}
	if len(asked) == 0 {
		return fmt.Errorf("%w: the source holds only %s, and a seed of it names scopes inside them", lineage.ErrSeedRefused, scopesText(holds))
	}
	for _, s := range asked {
		if !lineage.Within(s, holds) {
			return fmt.Errorf("%w: scope %q is not inside the source's own scopes (%s)", lineage.ErrSeedRefused, s, scopesText(holds))
		}
	}
	return nil
}

// sameScopes compares two scope lists of one spelling; nil and empty are
// both the whole rokh.
func sameScopes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func scopesText(s []string) string {
	if len(s) == 0 {
		return "the whole rokh"
	}
	return strings.Join(s, ",")
}

// seedKey is the seed's own key: its id in the keyring, its reader and its
// signer.
type seedKey struct {
	id     [32]byte
	reader key.Reader
	signer ed25519.PrivateKey
}

// deriveSeedKey derives the seed's key from the root's signing seed, the
// anchor and the seed id (HKDF-SHA256). Only the holder of the root derives
// it, and the same seed id always gives the same key.
func deriveSeedKey(root ed25519.PrivateKey, anchor, seed frame.ID) (seedKey, error) {
	b, err := hkdf.Key(sha256.New, root.Seed(), anchor[:], "rokh/seed/1/key"+string(seed[:]), 96)
	if err != nil {
		return seedKey{}, err
	}
	var k seedKey
	copy(k.id[:], b[:32])
	if k.id == ([32]byte{}) {
		return seedKey{}, errors.New("the seed's key came out as the owner's id")
	}
	if k.reader, err = key.ReaderFrom(b[32:64]); err != nil {
		return seedKey{}, err
	}
	k.signer = ed25519.NewKeyFromSeed(b[64:96])
	return k, nil
}

// seedKeyName is the name the seed's key is listed under: one address
// component, from the seed's short id.
func seedKeyName(seed frame.ID) string { return "seed-" + seed.Short() }

// giving is a give ready for the source: its events in recording order, the
// give's id, the grant the take is written under, and the give's payload.
// blocked, when set, refuses the take.
type giving struct {
	events  []lineage.Event
	give    frame.ID
	grant   frame.ID
	seed    event.Seed
	blocked error
}

// giveParents is where a give made now is signed: on the head of the
// source's main branch, as every write on that branch is, and on nothing
// else (F13). Nothing merges unless the owner commands it: heads the owner
// left apart stay apart, and the notes say so beside the answer. A source
// whose main branch names no accepted event gives on all its heads, as
// before, within the limit of an event's parents.
func giveParents(l *ledger.Ledger, src *carrier.Carrier) ([]frame.ID, []string, error) {
	heads := l.Heads()
	tip, ok, err := src.Ref(defaultBranch)
	if err != nil {
		return nil, nil, err
	}
	if !ok || l.State(tip) != ledger.Accepted {
		if len(heads) > event.MaxParents {
			return nil, nil, fmt.Errorf("the source has %d heads, its %s branch names none of them, and an event names at most %d parents; merge them first (rokh reconcile --merge)", len(heads), defaultBranch, event.MaxParents)
		}
		return heads, nil, nil
	}
	var notes []string
	if len(heads) > 1 {
		apart := 0
		for _, h := range heads {
			if h != tip {
				apart++
			}
		}
		notes = append(notes, fmt.Sprintf("the source holds %d heads; the give names the head of its %s branch, %s, and leaves %d apart, as the owner left them, until the owner merges them (rokh reconcile --merge)", len(heads), defaultBranch, tip.Short(), apart))
	}
	return []frame.ID{tip}, notes, nil
}

// newGiving signs the give (S2) on the parents given (giveParents): the
// keyring add of the seed's key, one grant to its signer per scope ("" is
// the whole rokh), and the give, chained so that each has the one before it
// in its causal past. The source's ledger judges each before it is handed
// on.
func newGiving(l *ledger.Ledger, root ed25519.PrivateKey, sk seedKey, sys *key.Reader, sd event.Seed, parents []frame.ID) (giving, error) {
	heads := parents
	if len(heads) > event.MaxParents {
		return giving{}, fmt.Errorf("the give would name %d parents and an event names at most %d; merge them first (rokh reconcile --merge)", len(heads), event.MaxParents)
	}
	// The seed's key reads nothing (Reads absent): it writes the take under
	// its grants and is named in no envelope.
	k := event.Keyring{Op: event.KeyringAdd, Key: sk.id, Gen: 1, Name: seedKeyName(sd.Seed),
		Reader: sk.reader.Public(), Signer: sk.signer.Public().(ed25519.PublicKey)}
	if sys != nil {
		sealed, err := key.SealTo(k.Reader, sys.Bytes(), key.InfoSystem, rand.Reader)
		if err != nil {
			return giving{}, err
		}
		k.System = sealed
	}
	payload, err := k.Encode()
	if err != nil {
		return giving{}, err
	}
	add, err := signJudged(l, root, nil, heads, event.VerbKeyring, payload)
	if err != nil {
		return giving{}, err
	}
	signed := []event.Signed{add}
	prev := add.ID
	grants := sd.Scopes
	if len(grants) == 0 {
		grants = []string{""} // the whole rokh
	}
	for _, scope := range grants {
		p, err := event.Grant{Subject: k.Signer, Scope: scope}.Encode()
		if err != nil {
			return giving{}, err
		}
		g, err := signJudged(l, root, nil, []frame.ID{prev}, event.VerbGrant, p)
		if err != nil {
			return giving{}, err
		}
		signed = append(signed, g)
		prev = g.ID
	}
	p, err := sd.Encode()
	if err != nil {
		return giving{}, err
	}
	give, err := signJudged(l, root, nil, []frame.ID{prev}, event.VerbSeed, p)
	if err != nil {
		return giving{}, err
	}
	signed = append(signed, give)
	out := giving{give: give.ID, grant: signed[1].ID, seed: sd}
	for _, e := range signed {
		out.events = append(out.events, lineageEvent(e))
	}
	return out, nil
}

// reuseGiving is a give the source holds already for this seed id, used again
// with its own keys (goal 7.41): nothing of it is signed again. Its key must
// be the one the root derives for the seed, its scopes the ones asked, and
// the add of its key and a grant to its signer must be in its causal past.
// The take is refused when the owner has since taken back the seed's key or
// every grant to it: a take signed now would rest on what the owner withdrew.
func reuseGiving(l *ledger.Ledger, gid frame.ID, sk seedKey, scopes []string) (giving, error) {
	ge, ok := l.Get(gid)
	if !ok || ge.HeadOnly {
		return giving{}, fmt.Errorf("%w: the give %s is not whole in the source", lineage.ErrSeedRefused, gid.Short())
	}
	sd, err := event.DecodeSeed(ge.Event.Payload)
	if err != nil {
		return giving{}, err
	}
	if sd.Key != sk.id {
		return giving{}, fmt.Errorf("%w: the give %s names a key the root does not derive for its seed", lineage.ErrSeedRefused, gid.Short())
	}
	if !sameScopes(sd.Scopes, scopes) {
		return giving{}, fmt.Errorf("%w: seed %s was given for %s, and this asks for %s", lineage.ErrSeedRefused, frame.ID(sd.Seed).Short(), scopesText(sd.Scopes), scopesText(scopes))
	}
	signer := sk.signer.Public().(ed25519.PublicKey)
	var add *event.Signed
	var grants []event.Signed
	for _, id := range l.CausalPast(gid) {
		e, _ := l.Get(id)
		if e.HeadOnly {
			continue
		}
		switch e.Event.Verb {
		case event.VerbKeyring:
			k, err := event.DecodeKeyring(e.Event.Payload)
			if err == nil && k.Op == event.KeyringAdd && k.Key == sk.id && bytes.Equal(k.Signer, signer) && bytes.Equal(k.Reader, sk.reader.Public()) {
				found := e
				add = &found
			}
		case event.VerbGrant:
			g, err := event.DecodeGrant(e.Event.Payload)
			if err == nil && !g.Open && bytes.Equal(g.Subject, signer) {
				grants = append(grants, e)
			}
		}
	}
	if add == nil || len(grants) == 0 {
		return giving{}, fmt.Errorf("%w: the give %s has no add of its key or no grant to it in its past", lineage.ErrSeedRefused, gid.Short())
	}
	out := giving{give: gid, seed: sd}
	out.events = append(out.events, lineageEvent(*add))
	for _, g := range grants {
		out.events = append(out.events, lineageEvent(g))
	}
	out.events = append(out.events, lineageEvent(ge))
	// Live now: the key's add not taken back, and a grant to it not revoked.
	adds, _ := l.Keyring()
	live := false
	for _, id := range adds {
		live = live || id == add.ID
	}
	active := map[frame.ID]bool{}
	for _, id := range l.ActiveGrants() {
		active[id] = true
	}
	for _, g := range grants {
		if active[g.ID] {
			out.grant = g.ID
			break
		}
	}
	switch {
	case !live:
		out.blocked = fmt.Errorf("%w: the seed's key was taken back after its give; no take is signed", lineage.ErrSeedRefused)
	case out.grant.IsZero():
		out.blocked = fmt.Errorf("%w: every grant to the seed's key was taken back after its give; no take is signed", lineage.ErrSeedRefused)
	}
	return out, nil
}

// takeOf signs the take once the new vessel's id is known (S1): by the
// seed's key, under its grant, on point (the give, or for a give used again
// the source's branch as it is now, takePoint), so the give is in its causal
// past. The source's ledger judges it first.
func takeOf(l *ledger.Ledger, sk seedKey, g giving, point []frame.ID) func(frame.ID) (lineage.Event, error) {
	return func(vesselID frame.ID) (lineage.Event, error) {
		if g.blocked != nil {
			return lineage.Event{}, g.blocked
		}
		sd := g.seed
		sd.Op, sd.Give, sd.Vessel = event.SeedTake, g.give, vesselID
		p, err := sd.Encode()
		if err != nil {
			return lineage.Event{}, err
		}
		grant := g.grant
		e, err := signJudged(l, sk.signer, &grant, point, event.VerbSeed, p)
		if err != nil {
			return lineage.Event{}, err
		}
		return lineageEvent(e), nil
	}
}

// signJudged signs one system event and has the source's ledger judge it:
// an event the ledger would not accept is never handed on to be recorded.
func signJudged(l *ledger.Ledger, priv ed25519.PrivateKey, authority *frame.ID, parents []frame.ID, verb string, payload []byte) (event.Signed, error) {
	anchor := l.Genesis()
	e, err := event.SignFresh(event.Event{Carrier: &anchor, Authority: authority, Parents: parents,
		Address: event.AddressRoot, Verb: verb, Payload: payload, Attest: stamp()}, priv)
	if err != nil {
		return event.Signed{}, err
	}
	st, err := l.Add(e.Raw)
	if err != nil {
		return event.Signed{}, err
	}
	if st != ledger.Accepted {
		why, _ := l.Why(e.ID)
		return event.Signed{}, fmt.Errorf("the %s %s would be %s here (%s); nothing was recorded", verb, e.ID.Short(), st, why)
	}
	return e, nil
}

func lineageEvent(e event.Signed) lineage.Event {
	return lineage.Event{ID: e.ID, Head: e.Head, Body: e.Body, Address: e.Event.Address}
}

// sourceLedger reads the source's ledger through the session it was opened
// with: the owner's, which opens every body.
func sourceLedger(c *carrier.Carrier) (*ledger.Ledger, error) {
	gen, err := c.Get(c.Anchor())
	if err != nil {
		return nil, fmt.Errorf("genesis unreadable: %w", err)
	}
	heads, err := c.Heads()
	if err != nil {
		return nil, err
	}
	return ledger.Load(gen, c.Get, heads)
}

// systemReaderOf opens the system reader from the owner's live keyring
// generation, where its private half is sealed to the owner's reader
// (0x000B). A ledger whose owner generation holds none gives none.
func systemReaderOf(l *ledger.Ledger, owner key.Reader) *key.Reader {
	adds, _ := l.Keyring()
	for _, id := range adds {
		e, ok := l.Get(id)
		if !ok || e.HeadOnly {
			continue
		}
		k, err := event.DecodeKeyring(e.Event.Payload)
		if err != nil || !k.IsOwner() || len(k.System) == 0 {
			continue
		}
		priv, err := key.OpenFrom(owner, k.System, key.InfoSystem)
		if err != nil {
			continue
		}
		if r, err := key.ReaderFrom(priv); err == nil {
			return &r
		}
	}
	return nil
}

// newSeedKeys makes the new vessel's key, its cells (the owner's, and those of
// the key generations the seed takes, judged at the points at) and its
// sealer, which seals at the take's point.
func newSeedKeys(pass string, info vessel.Info, sec key.Secret, owner key.Reader, sys *key.Reader, l *ledger.Ledger, at, point []frame.ID, scopes []string) (SeedKeys, error) {
	vk := make([]byte, 32)
	if _, err := rand.Read(vk); err != nil {
		return SeedKeys{}, err
	}
	kk, err := key.PassKey(pass, info.Salt, info.Iter)
	if err != nil {
		return SeedKeys{}, err
	}
	cell, err := key.Cell(kk, sec, vk, rand.Reader)
	if err != nil {
		return SeedKeys{}, err
	}
	cells, err := seedKeyCells(l, scopes, vk, at, l.Heads())
	if err != nil {
		return SeedKeys{}, err
	}
	if 1+len(cells) > vessel.SlotCells {
		return SeedKeys{}, fmt.Errorf("%w: the seed takes the owner's cell and %d keys' cells, and a vessel holds %d; nothing was recorded", lineage.ErrSeedRefused, len(cells), vessel.SlotCells)
	}
	sealer, err := seedSealer(l, point, sec, owner, sys, vk)
	if err != nil {
		return SeedKeys{}, err
	}
	return SeedKeys{VK: vk, Slots: append([][]byte{cell}, cells...), Sealer: sealer}, nil
}

// heldSeedKeys are the keys of a folder DST holds and did not take, which the
// same command goes on with: a folder cut after it was made and before its
// give (its give is made now), or cut after its give (the give is used
// again). Its own vessel key stays; its cells are judged again now, by the
// source's keyring as it is, so that a key taken back since the folder
// was made opens nothing the seed goes on to hold:
//   - the cell of a generation the ledger holds and that is no longer live
//     is gone (random filling takes its place);
//   - the cell of a generation the seed takes (seedKeyCells: live at the give
//     and now, and for a slice touching its scopes) that the folder lacks is
//     placed where nobody's cell is (key.Install);
//   - every other cell, the owner's among them, is left as it is.
//
// When anything changes, or when a head file of the folder still holds a
// cell of a generation no longer live (a command cut while it wrote them),
// Recell is set: goOnHeld writes the cells in every head file before
// anything else is recorded. The owner's passphrase must open an owner's cell
// of the folder.
func heldSeedKeys(pass string, info vessel.Info, sec key.Secret, owner key.Reader, sys *key.Reader, l *ledger.Ledger, at, point []frame.ID, scopes []string, h *SeedHeld) (SeedKeys, error) {
	kk, err := key.PassKey(pass, info.Salt, info.Iter)
	if err != nil {
		return SeedKeys{}, err
	}
	ownerCell := false
	for _, c := range h.Slots {
		if s, _, err := key.OpenCell(kk, c); err == nil && s.Key == ([32]byte{}) {
			ownerCell = true
			break
		}
	}
	if !ownerCell {
		return SeedKeys{}, fmt.Errorf("%w: the folder holds no owner's cell for this passphrase", lineage.ErrSeedRefused)
	}
	heads := l.Heads()
	cells, err := seedKeyCells(l, scopes, h.VK, at, heads)
	if err != nil {
		return SeedKeys{}, err
	}
	if 1+len(cells) > vessel.SlotCells {
		return SeedKeys{}, fmt.Errorf("%w: the seed takes the owner's cell and %d keys' cells, and a vessel holds %d; nothing was recorded", lineage.ErrSeedRefused, len(cells), vessel.SlotCells)
	}
	sealer, err := seedSealer(l, point, sec, owner, sys, h.VK)
	if err != nil {
		return SeedKeys{}, err
	}
	// The slot blobs of the generations the ledger holds: live now, or gone.
	liveNow := map[frame.ID]bool{}
	addsNow, _ := l.Keyring(heads...)
	for _, id := range addsNow {
		liveNow[id] = true
	}
	live, gone := map[string]bool{}, map[string]bool{}
	for _, id := range l.Order() {
		e, ok := l.Get(id)
		if !ok || e.HeadOnly || e.Event.Verb != event.VerbKeyring {
			continue
		}
		k, err := event.DecodeKeyring(e.Event.Payload)
		if err != nil || k.Op != event.KeyringAdd || len(k.Slot) != key.BlobSize {
			continue
		}
		if liveNow[id] {
			live[string(k.Slot)] = true
		} else {
			gone[string(k.Slot)] = true
		}
	}
	for b := range live {
		delete(gone, b)
	}
	slots := make([][]byte, len(h.Slots))
	changed := false
	for i, c := range h.Slots {
		slots[i] = append([]byte(nil), c...)
		if len(c) == key.CellSize && gone[string(c[:key.BlobSize])] {
			if _, err := rand.Read(slots[i]); err != nil {
				return SeedKeys{}, err
			}
			changed = true
		}
	}
	slots, n, err := key.Install(slots, owner, live, cells)
	if err != nil {
		return SeedKeys{}, fmt.Errorf("%w: %v; nothing was recorded", lineage.ErrSeedRefused, err)
	}
	stale := false
	for _, raw := range h.Heads {
		for b := range gone {
			stale = stale || bytes.Contains(raw, []byte(b))
		}
	}
	return SeedKeys{VK: h.VK, Slots: slots, Sealer: sealer, Recell: changed || n > 0 || stale}, nil
}

// seedKeyCells are the cells of the key generations a vessel takes, by the
// host's decision of 2026-09-29, and by nothing wider: a generation whose
// keyring add the ledger holds whole and accepted, so that it travels with
// the seed; live at the give (at: for a give made now, the give and every
// head it leaves apart) and not taken back since (now), for a give used
// again may be older than the source's present heads; and taken by a
// vessel of these scopes (key.Takes): its add carries a slot and, for a
// slice, its reads, or the scope of a grant to its signer that is live at
// both points, touch one of the slice's scopes. Never the owner's (it has its
// own cell), never a revoked generation, never a key without a slot, such as
// a seed's own key. Each cell is the slot blob the add carries with the
// vessel key sealed to the generation's reader: nothing of a key's secret is
// asked for or derived. A reconcile asks the same at a side's present heads
// (at and now alike, learnCells).
func seedKeyCells(l *ledger.Ledger, scopes []string, vk []byte, at, now []frame.ID) ([][]byte, error) {
	liveNow := map[frame.ID]bool{}
	addsNow, _ := l.Keyring(now...)
	for _, id := range addsNow {
		liveNow[id] = true
	}
	grantedNow := map[frame.ID]bool{}
	for _, id := range l.ActiveGrants(now...) {
		grantedNow[id] = true
	}
	grants := map[string][]string{} // a signer's live grant scopes
	for _, id := range l.ActiveGrants(at...) {
		e, ok := l.Get(id)
		if !ok || e.HeadOnly || !grantedNow[id] {
			continue
		}
		if g, err := event.DecodeGrant(e.Event.Payload); err == nil && !g.Open {
			grants[string(g.Subject)] = append(grants[string(g.Subject)], g.Scope)
		}
	}
	adds, _ := l.Keyring(at...)
	var cells [][]byte
	for _, id := range adds {
		if !liveNow[id] {
			continue
		}
		e, ok := l.Get(id)
		if !ok || e.HeadOnly || l.State(id) != ledger.Accepted {
			continue
		}
		k, err := event.DecodeKeyring(e.Event.Payload)
		if err != nil || !key.Takes(k, grants[string(k.Signer)], scopes) {
			continue
		}
		cell, err := key.CellFor(k.Slot, k.Reader, vk, rand.Reader)
		if err != nil {
			return nil, err
		}
		cells = append(cells, cell)
	}
	return cells, nil
}

// learnCells is v1Cells, the second half of R2 on one side of a reconcile, by the
// host's decision of 2026-09-29: after the union the side installs the cell
// of every live key generation its ledger holds and its vessel lacks, by the
// rule a new seed follows (seedKeyCells) at the side's present heads and
// inside its own scopes. It records no event: the cells go into the vessel's
// slot cells in one commit, where nobody's cell is (key.Install), and a cell
// the vessel holds is left as it is, so a second reconcile writes nothing. A
// key revoked on either side is not live after the union and takes no cell.
// Only the owner's passphrase tells the owner's cell from a free one, so
// under a key's passphrase no cell is installed (when in doubt, no cell).
// When the free cells do not hold them all, none is installed.
func learnCells(c *carrier.Carrier, own vessel.Owner, pass string) (cellsLearned, error) {
	v := c.Vessel()
	info := v.Info()
	sec, _, err := key.Try(pass, v.Slots(), info.Salt, info.Iter)
	if err != nil {
		return cellsLearned{}, err
	}
	if sec.Key != ([32]byte{}) {
		return cellsLearned{byKey: true}, nil
	}
	owner, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		return cellsLearned{}, err
	}
	// The union changed what the vessel holds: its ledger is read again,
	// through a session dressed from it.
	sess, _, layer, err := keyLayerOf(c, pass)
	if err != nil {
		return cellsLearned{}, err
	}
	if layer.ledgerRead() == nil {
		return cellsLearned{}, fmt.Errorf("the vessel %w", errNoEventYet)
	}
	c.SetSealer(sess)
	l := layer.ledgerRead()
	heads := l.Heads()
	cells, err := seedKeyCells(l, info.Scopes, v.VK(), heads, heads)
	if err != nil {
		return cellsLearned{}, err
	}
	held := map[string]bool{}
	adds, _ := l.Keyring(heads...)
	for _, id := range adds {
		if e, ok := l.Get(id); ok && !e.HeadOnly {
			if k, err := event.DecodeKeyring(e.Event.Payload); err == nil && len(k.Slot) == key.BlobSize {
				held[string(k.Slot)] = true
			}
		}
	}
	slots, n, err := key.Install(v.Slots(), owner, held, cells)
	if err != nil || n == 0 {
		return cellsLearned{}, err
	}
	r, err := c.Begin(own)
	if err != nil {
		return cellsLearned{outcome: vessel.NotRecorded}, err
	}
	r.Tx().SetSlots(slots)
	out, err := r.Commit()
	if out != vessel.Recorded {
		return cellsLearned{outcome: out}, fmt.Errorf("the commit of %d key cells was %s: %v", n, out, err)
	}
	return cellsLearned{installed: n, outcome: out}, nil
}

// boundaryOf is v1Boundary: a side's heads and its concurrent keys after the
// union, as the side's own session reads its ledger (R7, contract 4.4). The
// keyring is the system layer every key reads, so the owner's session and a
// key's show the same. A concurrent key is one with more than one live
// generation; each is named by its generation and the event that added it.
func boundaryOf(c *carrier.Carrier, pass string) (sideBoundary, error) {
	sess, _, layer, err := keyLayerOf(c, pass)
	if err != nil {
		return sideBoundary{}, err
	}
	if layer.ledgerRead() == nil {
		return sideBoundary{}, fmt.Errorf("the vessel %w", errNoEventYet)
	}
	c.SetSealer(sess)
	l := layer.ledgerRead()
	heads := l.Heads()
	adds, _ := l.Keyring(heads...)
	ring, err := key.Fold(adds, func(id frame.ID) ([]byte, bool) {
		e, ok := l.Get(id)
		if !ok || e.HeadOnly {
			return nil, false
		}
		return e.Event.Payload, true
	})
	if err != nil {
		return sideBoundary{}, err
	}
	out := sideBoundary{heads: heads}
	for _, id := range ring.Concurrent() {
		gens := ring.Of(id)
		named := make([]string, len(gens))
		for i, g := range gens {
			named[i] = fmt.Sprintf("gen %d (event %s)", g.Gen, g.Event.Short())
		}
		out.concurrent = append(out.concurrent, gens[0].Name+" "+strings.Join(named, ", "))
	}
	return out, nil
}

// seedSealer is the new vessel's sealer: the keyring's fold at the take's
// point (the give, or the source's branch for a give used again), the
// owner's reader to open with, the system reader, and the vessel's own
// pointer key. What it seals is the take, at that point.
func seedSealer(l *ledger.Ledger, at []frame.ID, sec key.Secret, owner key.Reader, sys *key.Reader, vk []byte) (*key.Session, error) {
	adds, _ := l.Keyring(at...)
	ring, err := key.Fold(adds, func(id frame.ID) ([]byte, bool) {
		e, ok := l.Get(id)
		if !ok || e.HeadOnly {
			return nil, false
		}
		return e.Event.Payload, true
	})
	if err != nil {
		return nil, err
	}
	if len(ring.Owner()) == 0 {
		if l.OwnerEver() {
			return nil, errors.New("every owner generation is taken back at the take's point; nothing is sealed without the owner (E3)")
		}
		// A rokh whose keyring never held an owner generation: the owner is
		// the cell's generation, as the key layer's own dressing has it.
		ring.Live = append([]key.Gen{{Keyring: event.Keyring{Op: event.KeyringAdd, Gen: sec.Gen, Name: "owner",
			Reader: owner.Public(), Reads: []string{""}}}}, ring.Live...)
	}
	ptk, err := vessel.SharedKey(vk)
	if err != nil {
		return nil, err
	}
	// Read-open at the give's point is the key layer's one rule (readOpenAt,
	// v1_keylayer.go).
	s := &key.Session{Ring: ring, Mine: []key.Reader{owner}, Rand: rand.Reader, PTK: ptk,
		ReadOpen: func(addr string) bool { return readOpenAt(l, addr, at...) }}
	if sys != nil {
		s.System = sys.Public()
	}
	return s, nil
}
