package ledger

import (
	"testing"

	"rokh/event"
	"rokh/frame"
)

// The owner's fold at a point is three states and an unknown (R4): no
// owner generation ever added before it (the genesis and the owner's own
// first add), generations live there, and a fold emptied by revocation, known
// and ever added; a point with a parent this ledger has not accepted is
// unknown. The fold at an event's own point is the fold at its parents, and
// a point about to be signed on some parents is asked the same way.
func TestTheOwnerFoldAtAPointTellsItsStatesApart(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	fold := func(what string, parents []frame.ID, readers int, ever, known bool) {
		t.Helper()
		r, e, k := l.OwnerFoldAt(parents...)
		if len(r) != readers || e != ever || k != known {
			t.Fatalf("%s: %d readers, ever %v, known %v; want %d, %v, %v", what, len(r), e, k, readers, ever, known)
		}
	}
	fold("the genesis's own point", nil, 0, false, true)
	fold("a point on the genesis", []frame.ID{w.gen.ID}, 0, false, true)

	owner := addFor([32]byte{}, 1, "owner", w.rootPub, []string{""})
	add := w.keyring(w.rootPriv, nil, []frame.ID{w.gen.ID}, owner)
	must(t, l, add, Accepted, "the owner's first generation")
	fold("a point on the owner's first add", []frame.ID{add.ID}, 1, true, true)
	if r, known := l.OwnerReadersAt(add.ID); !known || len(r) != 0 {
		t.Fatalf("the owner's own first add sits where no owner was added yet: %d %v", len(r), known)
	}
	if ever, known := l.OwnerEverAt(add.ID); !known || ever {
		t.Fatalf("the owner's own first add: ever %v known %v", ever, known)
	}

	rv := w.keyring(w.rootPriv, nil, []frame.ID{add.ID}, event.Keyring{Op: event.KeyringRevoke, Gen: 1, Target: add.ID})
	must(t, l, rv, Accepted, "the root takes the owner's only generation back")
	fold("a point on the revoke: emptied by revocation", []frame.ID{rv.ID}, 0, true, true)
	after := w.write(w.rootPriv, nil, []frame.ID{rv.ID}, "journal/after", "note", []byte("after"))
	must(t, l, after, Accepted, "an event after the revoke")
	if r, known := l.OwnerReadersAt(after.ID); !known || len(r) != 0 {
		t.Fatalf("after the revoke: %d readers, known %v", len(r), known)
	}
	if ever, known := l.OwnerEverAt(after.ID); !known || !ever {
		t.Fatalf("after the revoke the fold is emptied, not never added: ever %v known %v", ever, known)
	}

	// A second branch holds the owner's next generation; the join of the two
	// branches holds it live, and the revoked one is not live in it.
	next := addFor([32]byte{}, 2, "owner", w.rootPub, []string{""})
	g2 := w.keyring(w.rootPriv, nil, []frame.ID{add.ID}, next)
	must(t, l, g2, Accepted, "the owner's next generation, on another branch")
	fold("a point on the next generation's branch", []frame.ID{g2.ID}, 2, true, true)
	fold("a point joining both branches", []frame.ID{rv.ID, g2.ID}, 1, true, true)

	// Unknown: a parent never seen, and a parent held but pending.
	nobody := frame.Hash([]byte("a point nobody knows"))
	fold("a parent never seen", []frame.ID{nobody}, 0, false, false)
	fold("one known parent and one never seen", []frame.ID{g2.ID, nobody}, 0, false, false)
	orphan := w.write(w.rootPriv, nil, []frame.ID{nobody}, "journal/orphan", "note", []byte("orphan"))
	must(t, l, orphan, Pending, "an event whose parent is not here")
	fold("a pending parent", []frame.ID{orphan.ID}, 0, false, false)
	if _, known := l.OwnerReadersAt(orphan.ID); known {
		t.Fatal("a pending event's point was answered")
	}
	if _, known := l.OwnerEverAt(nobody); known {
		t.Fatal("an unknown event's point was answered")
	}
}
