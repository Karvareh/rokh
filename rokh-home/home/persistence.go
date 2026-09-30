package home

import "rokh-home/authority"

// Keep the keyring's address stable: the program daemon holds that interface.
// Publish the replacement map only after its encrypted pointer is stored.
func (h *Home) persistHeldKey(name string, key heldKey) error {
	return h.persistKeyring(func(next *keyring) { next.Keys[name] = key })
}

// Mutable operational state becomes visible only after its sealed pointer is
// saved. Failed writes must not grant rights or replace acknowledged drafts.
func copyRegistry(r *authority.Registry) *authority.Registry {
	next := *r
	next.Consumers = make(map[string]*authority.Consumer, len(r.Consumers))
	for id, c := range r.Consumers {
		v := *c
		v.Grants = append([]authority.Grant(nil), c.Grants...)
		next.Consumers[id] = &v
	}
	return &next
}

func (h *Home) persistRegistry(next *authority.Registry) error {
	if err := h.save(ptrRegistry, next); err != nil {
		return err
	}
	h.reg = next
	return nil
}

func (h *Home) persistDraft(d *Draft) error {
	next := *h.cat
	next.Drafts = make(map[string]*Draft, len(h.cat.Drafts)+1)
	for id, old := range h.cat.Drafts {
		next.Drafts[id] = old
	}
	next.Drafts[d.ID] = d
	if err := h.save(ptrCatalog, &next); err != nil {
		return err
	}
	h.cat = &next
	return nil
}
