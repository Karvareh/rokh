package daemon

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"rokh/booth"
	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/keyview"
)

// A booth answers each bound session from the view that session may hold,
// and from nothing wider (contract B4, B5, 3.3; T2):
//
//   - the owner, on a door the owner opened: the door's own view;
//   - on a door opened with a key's passphrase, that key's view and nothing
//     more, to every session: the owner and that key itself are answered
//     from the door's own view, which that key's reader opened, and it is
//     judged "lineage";
//   - any other key that gave its reader at prove: its own view, read with
//     that reader and the system reader only, on an opening of the vessel of
//     its own; on a key's door only what the door's view holds whole too;
//   - a key that gave no reader: heads only, lineage and no body;
//   - a guest: the read-open addresses, from what the system reader opens.
//
// Nothing a door opened with the owner's reader reaches another key's
// session, whole or cut. A question for an address or a verb outside the
// view is answered as a question for an address that holds nothing.

// viewKind is which view a session is answered from.
type viewKind int

const (
	viewDoor  viewKind = iota // the door's own view
	viewOwn                   // the session's own view, read with the reader it gave
	viewHeads                 // heads only: a key that gave no reader
	viewGuest                 // the read-open addresses
)

// viewValue is where a booth session keeps its own view.
const viewValue = "rokh/daemon/view"

// nowhere is an address no event has: a question narrowed to it is answered
// as a question for an address that holds nothing.
const nowhere = "\x00"

// restricted says whether this door's own view is a key's: then every
// session is served at most that key's view, judged lineage (B4, 3.3).
func (s *Server) restricted() bool { return s.opts.Layer != nil && !s.opts.Layer.Owner() }

// doorKey says whether who is the key this door was opened with.
func (s *Server) doorKey(who booth.Identity) bool {
	if !s.restricted() || who.Owner || who.Guest {
		return false
	}
	id, gen := s.opts.Layer.Key()
	if who.Key != id || who.Gen != gen {
		return false
	}
	rs := s.opts.Layer.Readers()
	return len(rs) > 0 && bytes.Equal(rs[0].Public(), who.Reader)
}

// kindOf is the view a bound session is answered from.
func (s *Server) kindOf(sess *booth.Session, who booth.Identity) viewKind {
	switch {
	case who.Guest:
		return viewGuest
	case who.Owner:
		return viewDoor
	case who.Credential != "" || who.Program != "":
		return viewHeads // this door issues no credential; another booth's is not a key here
	case s.doorKey(who):
		return viewDoor
	}
	if _, ok := sess.Reader(); ok {
		return viewOwn
	}
	return viewHeads
}

// sessionView is one session's own view: its own opening of the vessel, its
// ledger as its own reader opens it, and a door over them that only reads.
type sessionView struct {
	key    [32]byte
	gen    uint32
	reader []byte
	srv    *Server
}

// ownView is the door over a session's own view, made at its first question
// and kept for as long as the session stays bound to the same key and reader.
// The session's reader is held there, in its key's session, and nowhere
// else; the booth forgets both when the session ends (Ender).
func (s *Server) ownView(sess *booth.Session, who booth.Identity) (*Server, error) {
	r, ok := sess.Reader()
	if !ok {
		return nil, errors.New("this session gave no reader")
	}
	if v, ok := sess.Values[viewValue].(*sessionView); ok && v.key == who.Key && v.gen == who.Gen && bytes.Equal(v.reader, r.Public()) {
		return v.srv, nil
	}
	delete(sess.Values, viewValue)
	v2, _, err := s.car.Vessel().Reopen()
	if err != nil {
		return nil, fmt.Errorf("the vessel could not be opened again for this session's own view: %w", err)
	}
	c2 := carrier.Wrap(v2, nil)
	sec := key.Secret{Key: who.Key, Gen: who.Gen}
	copy(sec.Reader[:], r.Bytes())
	// The door vouches for the owner's first generation it holds: on a
	// carrier whose keyring names no owner generation, it stands for the
	// owner at every point of the session's view.
	var vouched *key.Gen
	if s.opts.Layer != nil {
		first := s.opts.Layer.First()
		vouched = &first
	}
	_, layer, err := keyview.KeySessionWith(c2, sec, vouched)
	if err != nil {
		return nil, fmt.Errorf("this key's own view does not open here: %w", err)
	}
	id, gen, pub := who.Key, who.Gen, r.Public()
	srv := New(c2, layer.Ledger(), Options{ReadOnly: true, Release: s.opts.Release, Door: s.opts.Door,
		ReadOn: layer.ReadOn,
		Live:   func() bool { return keyview.LiveIn(layer.Ledger(), id, gen, pub) }})
	sess.Values[viewValue] = &sessionView{key: id, gen: gen, reader: pub, srv: srv}
	return srv, nil
}

// systemView is the door over what the system reader opens, on an opening
// of the vessel of its own: the system layer and the read-open addresses
// whole, the rest heads. Heads-only sessions and guests are answered from it.
func (s *Server) systemView() (*Server, error) {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	if s.sysView != nil {
		return s.sysView, nil
	}
	if s.opts.Layer == nil || s.opts.Layer.System() == nil {
		return nil, errNoSystemView
	}
	v2, _, err := s.car.Vessel().Reopen()
	if err != nil {
		return nil, fmt.Errorf("the vessel could not be opened again for the system layer: %w", err)
	}
	c2 := carrier.Wrap(v2, nil)
	layer, err := keyview.SystemView(c2, *s.opts.Layer.System(), s.opts.Layer.First())
	if err != nil {
		return nil, fmt.Errorf("the system layer does not open here: %w", err)
	}
	s.sysView = New(c2, layer.Ledger(), Options{ReadOnly: true, Release: s.opts.Release, Door: s.opts.Door, ReadOn: layer.ReadOn})
	return s.sysView, nil
}

// errNoSystemView is a door that holds no system reader: its carrier's
// keyring names no owner generation (a carrier made before the surface
// recorded one), or the door was given no key layer.
var errNoSystemView = errors.New("this door holds no system reader, so it has no view for a session that gave no reader")

// wholeInDoor says whether this door's own view holds an event whole.
func (s *Server) wholeInDoor(id string) bool {
	fid, err := frame.ParseID(id)
	if err != nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.led.Get(fid)
	return ok && !e.HeadOnly
}

// strip leaves an event's row its head alone: id, author, grant, parents and
// the system mark.
func strip(r map[string]any) {
	for _, k := range []string{"address", "verb", "payload", "attest", "door", "key"} {
		delete(r, k)
	}
	r["head_only"] = true
}

// narrow keeps in an answer what keep allows and shows every other event by
// its head alone. In the answer to a question by address or verb (filtered),
// an event keep does not allow is not there at all: the question is answered
// as a question for an address that holds nothing.
func narrow(ans map[string]any, filtered bool, keep func(id string, system bool, addr string) bool) {
	if rows, ok := ans["events"].([]map[string]any); ok {
		out := rows[:0]
		for _, r := range rows {
			id, _ := r["id"].(string)
			addr, _ := r["address"].(string)
			system, _ := r["system"].(bool)
			if head, _ := r["head_only"].(bool); !head && keep(id, system, addr) {
				out = append(out, r)
				continue
			}
			if filtered {
				continue
			}
			strip(r)
			out = append(out, r)
		}
		ans["events"] = out
		if _, had := ans["last"]; had || filtered {
			if len(out) > 0 {
				ans["last"] = out[len(out)-1]["id"]
			} else {
				delete(ans, "last")
			}
		}
	}
	if raw, ok := ans["raw"].(string); ok {
		b, err := hex.DecodeString(raw)
		e, perr := event.Parse(b)
		switch {
		case err != nil || perr != nil:
			delete(ans, "raw")
			ans["head_only"] = true
		case e.HeadOnly:
			ans["head_only"] = true
		case !keep(e.ID.String(), e.System, e.Event.Address):
			ans["raw"] = hex.EncodeToString(e.Head)
			ans["head_only"] = true
		}
	}
}

// lineageOnly marks an answer from a restricted view: every event it names
// is judged "lineage", never "full" (3.3).
func lineageOnly(ans map[string]any) {
	if rows, ok := ans["events"].([]map[string]any); ok {
		for _, r := range rows {
			if _, judged := r["judged"]; judged {
				r["judged"] = "lineage"
			}
		}
	}
	if _, judged := ans["judged"]; judged {
		ans["judged"] = "lineage"
	}
}

// boothHandler answers a bound session and forgets its view when it ends.
type boothHandler struct{ s *Server }

func (h boothHandler) Serve(sess *booth.Session, req map[string]any) map[string]any {
	return h.s.BoothAnswer(sess, req)
}

// End forgets what the door kept for a session: its own view, which holds
// the reader it gave.
func (h boothHandler) End(sess *booth.Session) { delete(sess.Values, viewValue) }
