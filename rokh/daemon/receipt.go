package daemon

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/harness"
	"rokh/receipt"
)

// An engine holding delegated authority acts on the owner's behalf through
// this socket, and the socket is therefore where its acts have to become
// answerable. The receipt is what makes them so: an intent before the step, a
// result after it, both of them real signed events, the result naming the
// intent through the ledger's own parent link rather than through an
// agreement the two ends have to keep.
//
// The ritual was reachable from Go and from nowhere else, so the one caller
// that most needs to leave a receipt could not leave one. These three ops are
// the way in.
//
// None of this is Rokh. The receipt is the harnesses' custom on top of the
// ledger, and the ledger underneath carries these two events exactly as it
// carries any other: bytes, signature and authority, with no opinion about
// the payload.
//
//	— T10, T10.1, T10.5

// receiptRequest is what the three receipt ops read off the request line.
//
// It is read here rather than out of the daemon's own request struct because
// these fields are the custom's, not the core's: nothing below the receipt
// knows what an intent is, and the shape of one belongs beside the ops that
// speak it.
//
//	— T10.5
type receiptRequest struct {
	Address string    `json:"address,omitempty"`
	Key     string    `json:"key,omitempty"`
	Branch  string    `json:"branch,omitempty"`
	Doing   string    `json:"doing,omitempty"`
	Intent  string    `json:"intent,omitempty"`
	Outcome string    `json:"outcome,omitempty"`
	Saying  string    `json:"saying,omitempty"`
	Once    string    `json:"once,omitempty"`
	Witness witnessIn `json:"witness,omitempty"`
}

// witnessIn is the five witnesses a request carries.
//
// The sixth is not one of them, and its absence is not an omission. An intent
// cannot name its own receipt: its name is the hash of bytes that would have
// to contain it. A result owes all six, and the ritual fills that one from the
// name the ledger gave the intent — which removes the one way the two halves
// could disagree about which receipt they are.
//
//	— T10.2, T3.2
type witnessIn struct {
	Origin    string `json:"origin,omitempty"`
	Authority string `json:"authority,omitempty"`
	Audience  string `json:"audience,omitempty"`
	State     string `json:"state,omitempty"`
	WayBack   string `json:"wayBack,omitempty"`
}

func (w witnessIn) of() receipt.Witness {
	return receipt.Witness{
		Origin: w.Origin, Authority: w.Authority, Audience: w.Audience,
		State: w.State, WayBack: w.WayBack,
	}
}

// keeper is the daemon's carrier and ledger behind receipt.Keeper.
//
// The ritual holds neither key nor carrier; it decides what the two halves say
// and hands them over. This holds both, which is why Record returns a name
// instead of taking one: the name is the hash of the bytes as finally signed,
// so nothing before the signing could have known it, and a name settled
// earlier would be a name for bytes nobody wrote.
//
//	— T10.1, T3.2
type keeper struct {
	s         *Server
	priv      ed25519.PrivateKey
	authority *frame.ID
	branch    string
	// claim is the attempt this half is recorded under, when the caller named
	// one; typed is the answer the commit point gave when it did not record.
	claim []byte
	typed map[string]any
}

var _ receipt.Keeper = (*keeper)(nil)

// Record signs one half of a receipt and puts it in the ledger, on the carrier
// and under a reference, in that order.
//
// The branch head comes first among the parents, then whatever the event names
// for itself — and not twice, since a result whose intent is also the head
// names one parent, not the same one under two headings.
//
//	— T10.1, T4.2
func (k *keeper) Record(e event.Event) (frame.ID, error) {
	var zero frame.ID
	parents, err := k.s.branchHead(k.branch)
	if err != nil {
		k.typed = notRecorded("carrier_unreadable", err)
		return zero, err
	}
	for _, p := range e.Parents {
		seen := false
		for _, q := range parents {
			if q == p {
				seen = true
				break
			}
		}
		if !seen {
			parents = append(parents, p)
		}
	}
	e.Parents = parents
	anchor := k.s.car.Anchor()
	e.Carrier = &anchor
	e.Authority = k.authority
	// The door's stamp, and the caller's name for this half — bound to this
	// ledger, this key and the exact request — inside the bytes that are
	// signed.
	//   — T8.5, T3.6
	e.Attest = k.s.stamp(k.claim)
	signed, err := event.SignFresh(e, k.priv)
	if err != nil {
		k.typed = notRecorded("bad_request", err)
		return zero, err
	}
	// The reference is the commit point, and moving it is not tidying up.
	// Opening a carrier walks back from the heads, so bytes no reference
	// reaches are not an event on the next open — they are what a recording
	// that stopped left behind. Both halves go that way if this is skipped,
	// and a receipt nobody can read afterwards is not a receipt.
	//   — T8.5, T4.4
	if _, bad := k.s.commit(signed.Raw, func(rec *carrier.Recording, id frame.ID) error { return rec.SetRef(k.branch, id) }); bad != nil {
		k.typed = bad
		msg, _ := bad["error"].(string)
		return zero, errors.New(msg)
	}
	return signed.ID, nil
}

// Each walks the accepted events in the ledger's own deterministic order.
// Pending and rejected ones are not there to walk: the first has ancestry that
// has not arrived, and the second never was.
//
//	— T3.3
func (k *keeper) Each(visit func(frame.ID, event.Event) bool) error {
	for _, id := range k.s.led.Order() {
		s, found := k.s.led.Get(id)
		if !found {
			continue
		}
		if !visit(id, s.Event) {
			return nil
		}
	}
	return nil
}

// mayRecord is the guard the two writing halves share with append and write. A
// receipt is a pair of signed events, so a read-only daemon refuses it for the
// reason it refuses a write, and a daemon with signing off refuses it because
// making one means reaching into the keyring.
func (s *Server) mayRecord() map[string]any {
	if s.opts.ReadOnly {
		return notRecorded("read_only", errors.New("read-only"))
	}
	if !s.opts.AllowSign {
		return notRecorded("signing_disabled", errors.New("signing disabled; a receipt is two signed events"))
	}
	return nil
}

// speaksHere puts a receipt's own verb through the covenant of whatever
// harness owns this address, exactly as write does. The core weighs bytes,
// signature and authority; it does not know what a verb means and does not
// guess, and the receipt verbs get no exemption from that.
//
//	— T11.10, T13.5
func (s *Server) speaksHere(addr, verb string) (map[string]any, bool) {
	probe := event.Event{Address: addr, Verb: verb}
	c, found := s.bound.For(probe)
	if !found {
		return nil, true
	}
	switch c.Read(probe) {
	case harness.Shelf:
		// The harness's own root, and one of the ritual's two words: this is
		// the shelf its receipts sit on, and the covenant has no say here.
		//   — T11.11
		return nil, true
	case harness.Refused:
		// A receipt under the root is not an unknown verb; it is in the wrong
		// place, and the refusal says where the right one is.
		//   — T11.11
		if !c.AtRoot(probe) && (verb == receipt.VerbIntent || verb == receipt.VerbOutcome) {
			return notRecorded("harness_refused", fmt.Errorf("a receipt for %s is written at %q, its root, and nowhere under it; %q is where its verbs act",
				c.Namespace, c.Namespace, addr)), false
		}
		return notRecorded("harness_refused", fmt.Errorf("%q is not a verb %s %s can do", verb,
			c.Namespace, c.Version)), false
	case harness.Ignored:
		// Ignoring is an answer, and the answer is that nothing was written.
		// write may say so with an ok and no id, because its caller has
		// nothing further to do; a receipt's caller has to carry the intent's
		// name into the result, and reading ok as "recorded" would mean
		// performing the effect with no receipt behind it. So the response
		// says in a word which of the two happened.
		//   — T10, T11.10
		return ok(map[string]any{"ignored": true, "recorded": false, "record": NotRecorded,
			"namespace": c.Namespace, "version": c.Version}), false
	}
	return nil, true
}

// readReceipt decodes the receipt half of a request line. Handle has already decoded
// the core half of the same bytes, so what can still fail here is a field of
// the wrong shape, and a request that is not the shape it claims is refused
// rather than read past.
func readReceipt(line []byte) (receiptRequest, map[string]any) {
	var req receiptRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return req, refusal("bad_request", fmt.Errorf("bad request: %w", err))
	}
	if strings.TrimSpace(req.Address) == "" {
		return req, refusal("bad_request", errors.New("a receipt is written at an address; name one"))
	}
	return req, nil
}

// rite opens a ritual at the address the request names, on the branch it names
// and with the key it names. The defaults are write's: the main branch and the
// carrier's root key.
func (s *Server) rite(req receiptRequest) (*receipt.Ritual, *keeper, map[string]any) {
	priv, authority, err := s.key(req.Key)
	if err != nil {
		return nil, nil, fail(err)
	}
	branch := req.Branch
	if branch == "" {
		branch = "main"
	}
	k := &keeper{s: s, priv: priv, authority: authority, branch: branch}
	return receipt.New(req.Address, k), k, nil
}

// intent records the first half, before the step: what is about to be done,
// and the five witnesses an intent can show. The name it returns is the
// ledger's own name for the recorded event, and the result will answer that.
//
//	— T10.1, T10.2
func (s *Server) intent(line []byte) map[string]any {
	if r := s.mayRecord(); r != nil {
		return r
	}
	top := topOf(line)
	req, bad := readReceipt(line)
	if bad != nil {
		return bad
	}
	if r, may := s.speaksHere(req.Address, receipt.VerbIntent); !may {
		return r
	}
	rit, k, bad := s.rite(req)
	if bad != nil {
		return bad
	}
	answer := func(id frame.ID) map[string]any {
		addr := rit.Address()
		if e, found := s.led.Get(id); found {
			addr = e.Event.Address
		}
		return ok(map[string]any{"id": id.String(), "address": addr, "branch": k.branch})
	}
	claim, done := s.attemptFor(top.Attempt, k.priv.Public().(ed25519.PublicKey), "intent", map[string]any{
		"address": req.Address, "branch": k.branch, "doing": req.Doing,
		"witness": witnessFields(req.Witness),
	}, answer)
	if done != nil {
		return done
	}
	if r := s.precondition(top.ExpectHeads); r != nil {
		return r
	}
	k.claim = claim
	id, err := rit.Open(receipt.Intent{Doing: req.Doing, Witness: req.Witness.of()})
	if k.typed != nil {
		return k.typed
	}
	if err != nil {
		return notRecorded("receipt_refused", err)
	}
	r := answer(id)
	r["record"] = Recorded
	if top.Attempt != "" {
		r["attempt"] = top.Attempt
		r["already"] = false
	}
	return r
}

// topOf reads the attempt and the precondition off a receipt's request line.
// Handle has already decoded the same bytes, so a line that reaches here
// decodes.
func topOf(line []byte) request {
	var top request
	_ = json.Unmarshal(line, &top)
	return top
}

func witnessFields(w witnessIn) map[string]any {
	return map[string]any{"origin": w.Origin, "authority": w.Authority,
		"audience": w.Audience, "state": w.State, "wayBack": w.WayBack}
}

// outcome records the second half, after the step. The intent must already be
// in the ledger: a result whose parent is a name nobody recorded is a claim
// about an ending with no question in front of it, and the parent link exists
// to prevent exactly that. Every ending comes through here, the failure and
// the one that was never learned along with the success.
//
//	— T10.1, T10.3, T10.4
func (s *Server) outcome(line []byte) map[string]any {
	if r := s.mayRecord(); r != nil {
		return r
	}
	top := topOf(line)
	req, bad := readReceipt(line)
	if bad != nil {
		return bad
	}
	if r, may := s.speaksHere(req.Address, receipt.VerbOutcome); !may {
		return r
	}
	intent, err := frame.ParseID(req.Intent)
	if err != nil {
		return notRecorded("bad_request", fmt.Errorf("intent: %w", err))
	}
	rit, k, bad := s.rite(req)
	if bad != nil {
		return bad
	}
	answer := func(id frame.ID) map[string]any {
		// The address the result was actually written at, which is the intent's
		// own and not always the one asked for: a ritual reaches the intents below
		// it, and the result goes where its intent stands.
		//   — T10.6
		addr := rit.Address()
		if e, found := s.led.Get(id); found {
			addr = e.Event.Address
		}
		return ok(map[string]any{"id": id.String(), "intent": intent.String(),
			"outcome": req.Outcome, "address": addr, "branch": k.branch})
	}
	claim, done := s.attemptFor(top.Attempt, k.priv.Public().(ed25519.PublicKey), "outcome", map[string]any{
		"address": req.Address, "branch": k.branch, "intent": intent.String(),
		"outcome": req.Outcome, "saying": req.Saying, "once": req.Once,
		"witness": witnessFields(req.Witness),
	}, answer)
	if done != nil {
		return done
	}
	if r := s.precondition(top.ExpectHeads); r != nil {
		return r
	}
	k.claim = claim
	id, err := rit.Close(intent, receipt.Result{
		Outcome: receipt.Outcome(req.Outcome),
		Saying:  req.Saying,
		Witness: req.Witness.of(),
		Once:    req.Once,
	})
	if k.typed != nil {
		return k.typed
	}
	if err != nil {
		return notRecorded("receipt_refused", err)
	}
	r := answer(id)
	r["record"] = Recorded
	if top.Attempt != "" {
		r["attempt"] = top.Attempt
		r["already"] = false
	}
	return r
}

// receipts is the intents at an address that no result answers — the steps
// that were begun and never closed either way.
//
// It reads, and that is all it does. Nothing here closes anyone's books: no
// timeout turns an open receipt into a failure, and reading the list a second
// time leaves it exactly as the first reading found it. An unanswered intent
// stays unanswered until someone writes the result, including the honest one
// that says the ending was never learned.
//
// It signs nothing and stores nothing, so it stands with status and log rather
// than with the two halves above: a daemon opened read-only still answers it,
// which is what a harness closing its books on a cold carrier needs.
//
//	— T10.1, T10.4, T4.1, N-Axiom2
func (s *Server) receipts(line []byte) map[string]any {
	req, bad := readReceipt(line)
	if bad != nil {
		return bad
	}
	// Reading needs no key and moves no reference, so this keeper carries
	// neither.
	rit := receipt.New(req.Address, &keeper{s: s})
	unanswered, err := rit.Unanswered()
	if err != nil {
		return fail(err)
	}
	out := []map[string]any{}
	for _, id := range unanswered {
		row := map[string]any{"id": id.String()}
		if e, found := s.led.Get(id); found {
			row["address"] = e.Event.Address
			in, err := receipt.Read[receipt.Intent](e.Event.Payload)
			switch {
			case err != nil:
				// An event can declare itself an intent in the one field the
				// aperture peeks at and still not be the one encoding of one —
				// anything that can sign a payload can write that. The reading
				// failure was being dropped, and the receipt was listed as a
				// bare name with no doing and no witnesses, which reads like an
				// intent that simply said nothing. It says why instead.
				//   — N4.1, T10.6
				row["unreadable"] = err.Error()
			default:
				row["doing"] = in.Doing
				row["witness"] = map[string]any{
					"origin":    in.Witness.Origin,
					"authority": in.Witness.Authority,
					"audience":  in.Witness.Audience,
					"state":     in.Witness.State,
					"wayBack":   in.Witness.WayBack,
				}
			}
		}
		out = append(out, row)
	}
	return ok(map[string]any{"address": req.Address, "open": out})
}
