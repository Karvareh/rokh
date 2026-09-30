package home

import (
	"errors"
	"fmt"
	"strings"

	"rokh-home/authority"
)

// ledgerOps are the daemon ops a program may speak through its ledger grant.
// Writing outside the receipt ritual (write, append) and reading one event by
// name (get) are not among them; log and wait are, but only aimed at a place
// the program holds — its own namespace, or the shelf of tasks the owner
// keeps for it — so a program reads through its own aperture and nothing
// else (T10.6).
var ledgerOps = map[string]bool{"status": true, "capabilities": true, "bound": true, "bind": true,
	"intent": true, "outcome": true, "receipts": true, "attempt": true, "log": true, "wait": true}

// readingLedgerOps sign and store nothing and may be answered while the home
// goes on; wait in particular holds its answer for as long as the caller asked.
var readingLedgerOps = map[string]bool{"status": true, "capabilities": true, "bound": true,
	"receipts": true, "attempt": true, "log": true, "wait": true}

// LedgerOp answers a daemon request from a program holding ledger grants. The
// program speaks the daemon's own protocol, but the home decides first: the
// address it names must lie inside a namespace it holds, the key it signs with
// is the one held for it in that namespace — whatever key the request named —
// and what comes back about the ledger is cut down to what is the program's.
func (h *Home) LedgerOp(a Actor, req map[string]any) map[string]any {
	op, _ := req["op"].(string)
	deny := func(reason string, err error) map[string]any {
		r := map[string]any{"ok": false, "code": "denied", "reason": reason, "error": err.Error()}
		switch op {
		case "intent", "outcome":
			r["record"] = "not-recorded"
		}
		return r
	}
	if a.Owner {
		return h.OwnerLedger(req)
	}
	if !ledgerOps[op] {
		return deny("op_not_granted", fmt.Errorf("home: %q is not an op a program speaks here", op))
	}
	// The decision is made under the home's lock; the door is asked without
	// it for a request that only reads, so a program waiting on its shelf
	// keeps nobody else from recording.
	if readingLedgerOps[op] {
		h.mu.RLock()
	} else {
		h.mu.Lock()
	}
	unlocked := false
	unlock := func() {
		if unlocked {
			return
		}
		unlocked = true
		if readingLedgerOps[op] {
			h.mu.RUnlock()
		} else {
			h.mu.Unlock()
		}
	}
	defer unlock()
	if err := h.Health(); err != nil {
		return map[string]any{"ok": false, "code": "durability_unknown", "error": err.Error(), "reopen_required": true}
	}
	c, ok := h.reg.Get(a.Consumer)
	if !ok {
		return deny(authority.UnknownConsumer, errors.New("home: unknown program"))
	}
	namespaces := h.reg.Scopes(a.Consumer, authority.Ledger)
	covering := func(address string) (string, bool) {
		for _, ns := range namespaces {
			if authority.Covers(ns, address) {
				return ns, true
			}
		}
		return "", false
	}
	fwd := map[string]any{}
	for k, v := range req {
		fwd[k] = v
	}
	switch op {
	case "intent", "outcome", "receipts", "bind":
		address, _ := req["address"].(string)
		if op == "bind" {
			address, _ = req["namespace"].(string)
		}
		d := h.reg.Decide(a.Consumer, a.Session, authority.Ledger, address)
		if !d.Allowed {
			return deny(d.Reason, fmt.Errorf("home: ledger is not allowed at %q (%s)", address, d.Reason))
		}
		ns, _ := covering(address)
		fwd["key"] = keyName(c.ID, ns)
	case "log", "wait":
		// Aimed at one place the program holds: its namespace and what lies
		// under it, or its own shelf of tasks. Nowhere else, and never the
		// whole ledger.
		address, _ := req["address"].(string)
		if address == "" {
			return deny("address_required", errors.New("home: a program reads the ledger at an address it holds; name one"))
		}
		switch ns, held := covering(address); {
		case authority.Covers(TasksAddress+"/"+c.ID, address):
			// Its own shelf. Being handed work is the owner's act; reading
			// what was handed to you needs no grant beyond standing.
			if c.Revoked {
				return deny(authority.ConsumerRevoked, errors.New("home: this program's standing was withdrawn"))
			}
			if a.Session != c.Version {
				return deny(authority.StaleSession, errors.New("home: the program's authority changed; say hello again"))
			}
		case held:
			if d := h.reg.Decide(a.Consumer, a.Session, authority.Ledger, ns); !d.Allowed {
				return deny(d.Reason, fmt.Errorf("home: ledger is not allowed at %q (%s)", address, d.Reason))
			}
		default:
			return deny(authority.NoGrant, fmt.Errorf("home: %q is not a place this program holds", address))
		}
	case "attempt":
		d := h.reg.Decide(a.Consumer, a.Session, authority.Ledger, firstOr(namespaces, "none"))
		if !d.Allowed {
			return deny(d.Reason, errors.New("home: no ledger grant"))
		}
		fwd["key"] = keyName(c.ID, namespaces[0])
		delete(fwd, "author")
	default:
		if len(namespaces) == 0 {
			return deny(authority.NoGrant, errors.New("home: no ledger grant"))
		}
		if d := h.reg.Decide(a.Consumer, a.Session, authority.Ledger, namespaces[0]); !d.Allowed {
			return deny(d.Reason, errors.New("home: ledger is not allowed"))
		}
	}
	if readingLedgerOps[op] {
		unlock()
	}
	// What the program is told of the ledger is its own view, opened with its
	// own key's reader (T2, contract B4): never the owner's view cut to its
	// namespace. The door's own register of harnesses (bound) and what the
	// door is (capabilities) are the program's door's; recording goes through
	// that door, which seals every record at its own point.
	door := h.progDoor
	switch op {
	case "status", "log", "wait", "receipts", "attempt":
		view, err := h.programView(c.ID)
		if err != nil {
			return map[string]any{"ok": false, "code": "view_denied", "error": err.Error()}
		}
		door = view
	}
	r := ask(door, fwd)
	switch op {
	case "status":
		mine := map[string]bool{}
		for _, k := range h.keys.Keys {
			if k.Consumer == c.ID {
				mine[k.Grant] = true
			}
		}
		grants := []string{}
		if all, ok := r["grants"].([]string); ok {
			for _, g := range all {
				if mine[g] {
					grants = append(grants, g)
				}
			}
		}
		r["grants"] = grants
		delete(r, "branches")
	case "capabilities":
		if signers, ok := r["signers"].([]map[string]any); ok {
			var own []map[string]any
			for _, s := range signers {
				if name, _ := s["name"].(string); strings.HasPrefix(name, "c-"+c.ID[:16]) {
					own = append(own, s)
				}
			}
			r["signers"] = own
		}
	case "bound":
		if hs, ok := r["harnesses"].([]map[string]any); ok {
			var own []map[string]any
			for _, b := range hs {
				if ns, _ := b["namespace"].(string); ns != "" {
					if _, ok := covering(ns); ok {
						own = append(own, b)
					}
				}
			}
			if own == nil {
				own = []map[string]any{}
			}
			r["harnesses"] = own
		}
	}
	return r
}

func firstOr(list []string, otherwise string) string {
	if len(list) == 0 {
		return otherwise
	}
	return list[0]
}
