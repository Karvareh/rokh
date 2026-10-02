package home

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"rokh-home/authority"
	"rokh-home/sheet"
	"rokh/content"
	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
)

// ConsumerView is a program's registration as the owner sees it.
type ConsumerView struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	Grants    []authority.Grant `json:"grants"`
	Version   uint64            `json:"version"`
	Revoked   bool              `json:"revoked"`
	KeyPublic string            `json:"key_public,omitempty"`
	LedgerKey []string          `json:"ledger_keys,omitempty"`
	Places    Places            `json:"places"`
}

func (h *Home) consumerView(c *authority.Consumer) ConsumerView {
	v := ConsumerView{ID: c.ID, Name: c.Name, Kind: c.Kind, Grants: append([]authority.Grant(nil), c.Grants...),
		Version: c.Version, Revoked: c.Revoked, KeyPublic: c.KeyPublic, Places: h.places[c.ID]}
	for name, k := range h.keys.Keys {
		if k.Consumer == c.ID {
			v.LedgerKey = append(v.LedgerKey, name+" → "+k.Grant)
		}
	}
	sort.Strings(v.LedgerKey)
	return v
}

// AddConsumer registers a program and issues its credential, which is shown
// this once and kept only as a verifier.
func (h *Home) AddConsumer(a Actor, name, kind string, places Places) (ConsumerView, []byte, error) {
	if err := ownerOnly(a, "consumer.add"); err != nil {
		return ConsumerView{}, nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	reg := copyRegistry(h.reg)
	c, cred, err := reg.Add(name, kind, h.regKey)
	if err != nil {
		return ConsumerView{}, nil, err
	}
	nextPlaces := make(map[string]Places, len(h.places)+1)
	for id, p := range h.places {
		nextPlaces[id] = p
	}
	nextPlaces[c.ID] = places
	if err := h.save(ptrPlaces, nextPlaces); err != nil {
		return ConsumerView{}, nil, err
	}
	if err := h.persistRegistry(reg); err != nil {
		return ConsumerView{}, nil, err
	}
	h.places = nextPlaces
	return h.consumerView(c), cred, nil
}

// Consumers lists every registered program.
func (h *Home) Consumers(a Actor) ([]ConsumerView, error) {
	if err := ownerOnly(a, "consumer.list"); err != nil {
		return nil, err
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []ConsumerView
	for _, c := range h.reg.Consumers {
		out = append(out, h.consumerView(c))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Grant gives a program one action over one scope. Recording needs a key the
// ledger accepts, so the first record grant makes the program's key and the
// owner's ledger grant for it; a ledger grant does the same for its namespace.
func (h *Home) Grant(a Actor, consumer string, action authority.Action, scope string) (authority.Grant, error) {
	if err := ownerOnly(a, "grant"); err != nil {
		return authority.Grant{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	reg := copyRegistry(h.reg)
	c, ok := reg.Get(consumer)
	if !ok {
		return authority.Grant{}, errors.New("home: no such program")
	}
	if h.replica.Kind == MirrorReplica && !mirrorAction(action) {
		return authority.Grant{}, errors.New("home: a reading mirror cannot grant a writing or signing action")
	}
	// Validate the requested scope and standing before preparing any signing
	// authority. The copied registry is published only after durable success.
	g, err := reg.Grant(consumer, action, scope)
	if err != nil {
		return g, err
	}
	if action == authority.Record {
		if err := h.ensureLedgerKey(c, "", ItemsAddress, []string{content.VerbPut}); err != nil {
			return authority.Grant{}, err
		}
	}
	if action == authority.Ledger {
		if _, err := authority.CleanPath(scope); err != nil || scope == authority.WholeHome || strings.HasPrefix(scope, "home") || strings.HasPrefix(scope, event.AddressRoot) {
			return authority.Grant{}, fmt.Errorf("home: %q is not a namespace a program may hold", scope)
		}
		if err := h.ensureLedgerKey(c, scope, scope, nil); err != nil {
			return authority.Grant{}, err
		}
		// The program's own key reads its namespaces and its shelf (T2):
		// what is recorded there is sealed to it, and it is answered from
		// what its own reader opens.
		if err := h.ensureProgramReaderLocked(reg, c); err != nil {
			return authority.Grant{}, err
		}
	}
	return g, h.persistRegistry(reg)
}

func (h *Home) ensureLedgerKey(c *authority.Consumer, namespace, scope string, verbs []string) error {
	if err := h.Health(); err != nil {
		return err
	}
	name := keyName(c.ID, namespace)
	setConsumerKey := func(k heldKey) {
		c.KeyPublic = hex.EncodeToString(ed25519.PrivateKey(k.Priv).Public().(ed25519.PublicKey))
		if namespace == "" {
			c.KeyName, c.RokhGrant = name, k.Grant
		}
	}
	k, exists := h.keys.Keys[name]
	if exists && len(k.Priv) != ed25519.PrivateKeySize {
		return errors.New("home: stored signing authority is corrupt")
	}
	if exists && k.Grant != "" && h.grantStanding(k.Grant) {
		setConsumerKey(k)
		return nil
	}
	if !exists || k.Grant != "" || k.Attempt == "" {
		var priv ed25519.PrivateKey
		for _, old := range h.keys.Keys {
			if old.Consumer == c.ID {
				if len(old.Priv) != ed25519.PrivateKeySize {
					return errors.New("home: stored signing authority is corrupt")
				}
				priv = append(ed25519.PrivateKey(nil), old.Priv...)
				break
			}
		}
		if priv == nil {
			var err error
			if _, priv, err = ed25519.GenerateKey(rand.Reader); err != nil {
				return err
			}
		}
		k = heldKey{Priv: priv, Consumer: c.ID, Scope: scope, Attempt: "home:grant:" + newID()}
		// Save the secret and exact retry identity BEFORE the public grant.
		// A pending key has no grant ID, so the program door cannot use it.
		if err := h.persistHeldKey(name, k); err != nil {
			return err
		}
	}
	pub := ed25519.PrivateKey(k.Priv).Public().(ed25519.PublicKey)
	payload, err := event.Grant{Subject: pub, Scope: scope, Verbs: verbs}.Encode()
	if err != nil {
		return err
	}
	r := ask(h.ownerDoor, map[string]any{"op": "write", "address": event.AddressRoot, "verb": event.VerbGrant,
		"payload": b64(payload), "attempt": k.Attempt})
	if r["record"] != daemon.Recorded {
		return fmt.Errorf("home: the ledger grant for %s was not recorded: %v", c.Name, r["error"])
	}
	k.Grant = fmt.Sprint(r["id"])
	if err := h.persistHeldKey(name, k); err != nil {
		return err
	}
	setConsumerKey(k)
	return nil
}

func (h *Home) grantStanding(id string) bool {
	st := ask(h.ownerDoor, map[string]any{"op": "status"})
	grants, _ := st["grants"].([]string)
	for _, g := range grants {
		if g == id {
			return true
		}
	}
	return false
}

// Ungrant withdraws one grant. The session a program holds goes stale at once;
// what it did under the grant stays done.
func (h *Home) Ungrant(a Actor, consumer, grantID string) error {
	if err := ownerOnly(a, "ungrant"); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	reg := copyRegistry(h.reg)
	if err := reg.Ungrant(consumer, grantID); err != nil {
		return err
	}
	if err := h.persistRegistry(reg); err != nil {
		return err
	}
	// A program that reads through a key of its own reads what its grants
	// say from now on: a namespace withdrawn is not sealed to it again.
	h.keys.mu.RLock()
	_, reads := h.keys.Readers[consumer]
	h.keys.mu.RUnlock()
	if c, ok := reg.Get(consumer); ok && reads && !c.Revoked {
		return h.ensureProgramReaderLocked(reg, c)
	}
	return nil
}

// RevokeConsumer withdraws a program's standing: its credential and sessions
// stop working now, and every ledger grant made for its key is revoked in the
// ledger. Revocation closes the future; the events it recorded stay accepted.
func (h *Home) RevokeConsumer(a Actor, consumer string) error {
	if err := ownerOnly(a, "revoke"); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	reg := copyRegistry(h.reg)
	if err := reg.Revoke(consumer); err != nil {
		return err
	}
	if err := h.persistRegistry(reg); err != nil {
		return err
	}
	for name, k := range h.keys.Keys {
		if k.Consumer != consumer || !h.grantStanding(k.Grant) {
			continue
		}
		target, err := frame.ParseID(k.Grant)
		if err != nil {
			return err
		}
		payload, err := event.Revoke{Target: target}.Encode()
		if err != nil {
			return err
		}
		r := ask(h.ownerDoor, map[string]any{"op": "write", "address": event.AddressRoot, "verb": event.VerbRevoke,
			"payload": b64(payload), "attempt": "home:revoke:" + k.Grant[:40]})
		if r["record"] != daemon.Recorded {
			return fmt.Errorf("home: the ledger revocation of %s was not recorded: %v", name, r["error"])
		}
	}
	// Its own key is taken back too: nothing is sealed to it any more (K4).
	return h.retireProgramReaderLocked(consumer)
}

// DeclareInput is the owner's declaration of authorship for one version.
type DeclareInput struct {
	Version    int             `json:"version"`
	About      string          `json:"about"`
	Author     string          `json:"author"`
	Statement  string          `json:"statement"`
	Authorship string          `json:"authorship"`
	Segments   []sheet.Segment `json:"segments,omitempty"`
}

// DeclareAuthor records the owner's word about who wrote a version, and makes
// the next revision of its sheet cite that recorded word. Only the owner
// declares; a registrar's signature never does.
func (h *Home) DeclareAuthor(a Actor, path string, in DeclareInput) (string, string, error) {
	if err := ownerOnly(a, "declare"); err != nil {
		return "", "", err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.replica.Kind == MirrorReplica {
		return "", "", errors.New("home: a reading mirror cannot declare authorship")
	}
	item, v, err := h.versionOf(path, in.Version)
	if err != nil {
		return "", "", err
	}
	prev, prevID, err := h.sheetOf(v)
	if err != nil {
		return "", "", err
	}
	// Validate the complete proposed sheet before committing the decision.
	// This placeholder is used only for structural validation, never stored.
	pending := strings.Repeat("0", 64)
	proposed := h.declaredSheet(prev, prevID, in, pending)
	validate := func(id string) (sheet.EventFacts, bool) {
		if id == pending {
			return sheet.EventFacts{Accepted: true, ByOwner: true, Address: sheet.DeclarationAddress(item.ID), Verb: sheet.DeclarationVerb}, true
		}
		return h.ledgerCheck(id)
	}
	if err := proposed.Check(validate); err != nil {
		return "", "", err
	}
	record := declarationRecord{Format: declarationFormat, Item: item.ID, Version: v.N,
		Manifest: v.SHA256, PreviousSheet: prevID, Input: in}
	recordID, recordBytes, err := h.putJSON("declaration", record)
	if err != nil {
		return "", "", err
	}
	hash, err := frame.ParseID(recordID)
	if err != nil {
		return "", "", err
	}
	payload, err := (content.Descriptor{Hash: hash, Size: uint64(len(recordBytes)), Type: declarationRecordType}).Encode()
	if err != nil {
		return "", "", err
	}
	attempt := fmt.Sprintf("home:author:%.12s:%d:%.32s", item.ID, v.N, recordID)
	r := ask(h.ownerDoor, map[string]any{"op": "write", "address": sheet.DeclarationAddress(item.ID),
		"verb": sheet.DeclarationVerb, "payload": b64(payload), "attempt": attempt})
	if r["record"] == daemon.Unknown {
		r = ask(h.ownerDoor, map[string]any{"op": "attempt", "attempt": attempt})
	}
	if r["record"] != daemon.Recorded {
		return "", "", fmt.Errorf("home: declaration state %v: %v", r["record"], r["error"])
	}
	evt := fmt.Sprint(r["id"])
	next := h.declaredSheet(prev, prevID, in, evt)
	if err := next.Check(h.ledgerCheck); err != nil {
		return "", evt, err
	}
	sheetID, _, err := h.putJSON(kindSheet, next)
	if err != nil {
		return "", evt, err
	}
	item.Versions[v.N-1].Sheets = append(item.Versions[v.N-1].Sheets, sheetID)
	return sheetID, evt, h.saveCatalog()
}

// Disclosure is what would leave the home toward a peer, shown before it does.
type Disclosure struct {
	Hash       string `json:"hash"`
	Item       string `json:"item"`
	Path       string `json:"path"`
	Version    int    `json:"version"`
	VersionSHA string `json:"version_sha256"`
	BodySize   int64  `json:"body_size"`
	BodySHA256 string `json:"body_sha256"`
	Start      int64  `json:"start"`
	End        int64  `json:"end"`
	Excerpt    []byte `json:"excerpt"`
	ExcerptSHA string `json:"excerpt_sha256"`
	Recipient  string `json:"recipient"`
	Network    string `json:"network"`
	By         string `json:"by"`
	State      string `json:"state"`
	// What stays on the author's own chain and what goes out to peers.
	Private string `json:"private"`
	Public  string `json:"public"`
}

func (d Disclosure) name() string {
	c := d
	c.Hash, c.State = "", ""
	b, _ := json.Marshal(c)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// DisclosePreview prepares a disclosure of a byte range of a version toward a
// recipient in a network. It needs both share and bytes on the path. Nothing
// leaves until the owner confirms this exact preview.
func (h *Home) DisclosePreview(a Actor, path string, n int, start, end int64, recipient, network string) (Disclosure, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := h.require(a, authority.Share, path); err != nil {
		return Disclosure{}, err
	}
	if _, err := h.require(a, authority.Bytes, path); err != nil {
		return Disclosure{}, err
	}
	item, v, err := h.versionOf(path, n)
	if err != nil {
		return Disclosure{}, err
	}
	body, err := h.versionBytes(v, "")
	if err != nil {
		return Disclosure{}, err
	}
	if start < 0 || end > int64(len(body)) || start >= end || end-start > 1<<16 {
		return Disclosure{}, errors.New("home: the excerpt must be a non-empty range of at most 64 KiB inside the version")
	}
	bsum := sha256.Sum256(body)
	esum := sha256.Sum256(body[start:end])
	d := Disclosure{Item: item.ID, Path: path, Version: v.N, VersionSHA: v.SHA256, BodySize: int64(len(body)),
		BodySHA256: hex.EncodeToString(bsum[:]), Start: start, End: end, Excerpt: append([]byte(nil), body[start:end]...),
		ExcerptSHA: hex.EncodeToString(esum[:]), Recipient: recipient, Network: network, By: actorID(a),
		State:   "pending-owner",
		Private: "the whole body, as a private entry on the author's own source chain; its bytes are not published, the action that created it is",
		Public:  "the excerpt bytes, the manuscript's title, hashes and links, and the actions that create them, published to the network's peers"}
	d.Hash = d.name()
	b, _ := json.Marshal(d)
	return d, h.st.SetPointer(ptrDisclosure+d.Hash, b)
}

// ConfirmDisclosure is the owner confirming one exact preview.
func (h *Home) ConfirmDisclosure(a Actor, hash string) (Disclosure, error) {
	if err := ownerOnly(a, "disclose.confirm"); err != nil {
		return Disclosure{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	d, err := h.disclosure(hash)
	if err != nil {
		return d, err
	}
	if d.name() != hash {
		return d, errors.New("home: the stored preview does not match its name")
	}
	d.State = "confirmed"
	b, _ := json.Marshal(d)
	return d, h.st.SetPointer(ptrDisclosure+hash, b)
}

func (h *Home) disclosure(hash string) (Disclosure, error) {
	var d Disclosure
	b, ok, err := h.st.Pointer(ptrDisclosure + hash)
	if err != nil {
		return d, err
	}
	if !ok {
		return d, ErrNotFound
	}
	return d, json.Unmarshal(b, &d)
}

// ConfirmedDisclosure gives the program that prepared a confirmed disclosure
// what it may now send: the excerpt, and the body for its own private entry.
// Authority is decided again now — both of the rights the preview asked for,
// because this is the call that hands the body over. The owner's confirmation
// is a second condition, not a standing grant: a program whose bytes was
// withdrawn between the preview and now collects nothing.
func (h *Home) ConfirmedDisclosure(a Actor, hash string) (Disclosure, []byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, err := h.disclosure(hash)
	if err != nil || d.By != actorID(a) {
		return Disclosure{}, nil, ErrNotFound
	}
	if _, err := h.require(a, authority.Share, d.Path); err != nil {
		return Disclosure{}, nil, err
	}
	if _, err := h.require(a, authority.Bytes, d.Path); err != nil {
		return Disclosure{}, nil, err
	}
	if d.State != "confirmed" {
		return d, nil, errors.New("home: the owner has not confirmed this disclosure")
	}
	_, v, err := h.versionOf(d.Path, d.Version)
	if err != nil {
		return d, nil, err
	}
	body, err := h.versionBytes(v, "")
	if err != nil {
		return d, nil, err
	}
	if s := sha256.Sum256(body); hex.EncodeToString(s[:]) != d.BodySHA256 {
		return d, nil, errors.New("home: the version no longer matches the preview")
	}
	return d, body, nil
}
