package tui

// Sample is a ledger as the shell would hand it over, made up for showing
// the screen: a person's notes, one sentence waiting, a key that may write
// one place and a reader one place has been opened to, a grant taken back,
// and the three layers. No such ledger exists anywhere. sample.json is the
// same snapshot as a file, and a test keeps the two equal.
func Sample() Snapshot {
	return Snapshot{
		Vault: "/path/to/rokh", Ledger: "home", Anchor: "a0b6443e", Head: "8c91d2af", Heads: 1, Custody: "warm",
		Room: "64 MB", Free: "58 MB", Growth: "fixed", Verified: true, Accepted: 3, System: 4,
		Events: []EventItem{
			{ID: "8c91d2af", Verdict: "accepted", Verb: "note", Address: "home/journal/today", Payload: "Walked to the river before work; the water was high.", Parent: "72b4e191", Signer: "root", Authority: "the owner's own right", Door: "rokh-shell"},
			{ID: "72b4e191", Verdict: "accepted", Verb: "rokh.revoke", Address: "rokh", Payload: "grant 0be21d6c taken back, from here on", Parent: "5c1f09aa", Signer: "root", Authority: "the owner's own right", Door: "rokh-shell", System: true},
			{ID: "5c1f09aa", Verdict: "accepted", Verb: "note", Address: "work/plans", Payload: "Draft the budget by Friday.", Parent: "31ab770c", Signer: "assistant", Authority: "grant 5f20c331", Door: "rokh-home/program"},
			{ID: "31ab770c", Verdict: "accepted", Verb: "rokh.grant", Address: "rokh", Payload: "writing entrusted to 3fa9c1d2… at work/plans", Parent: "9e04d2b7", Signer: "root", Authority: "the owner's own right", Door: "rokh", System: true},
			{ID: "9e04d2b7", Verdict: "accepted", Verb: "note", Address: "home/journal", Payload: "The first line belongs to this address.", Parent: "5d21c0e9", Signer: "root", Authority: "the owner's own right", Door: "rokh-shell"},
			{ID: "5d21c0e9", Verdict: "accepted", Verb: "rokh.keyring", Address: "rokh", Payload: "the owner's key added, generation 1", Parent: "a0b6443e", Signer: "root", Authority: "the owner's own right", Door: "rokh", System: true},
			{ID: "a0b6443e", Verdict: "accepted", Verb: "rokh.genesis", Address: "rokh", Payload: "made: my notes", Signer: "root", Authority: "the owner's own right", Door: "rokh", System: true},
		},
		Drafts: []Draft{
			{Index: 1, Count: 1, Sentence: "write at home/journal/today: Call the plumber about the kitchen tap.",
				Address: "home/journal/today", Verb: "note", PayloadBytes: 42, Signer: "root", Authority: "the owner's own right", Parent: "8c91d2af"},
		},
		Layers: Layers{Star: Star{Anchor: "a0b6443e", System: 4, Keys: 1},
			Planets: []Planet{
				{Name: "home", Events: 2, Moons: []Moon{{Name: "journal", Events: 2, Deeper: []string{"today"}}}},
				{Name: "work", Events: 1, Moons: []Moon{{Name: "plans", Events: 1}}},
			}},
		Writing:    []AuthorityItem{{ID: "5f20c331", Subject: "3fa9c1d2…", Scope: "work/plans", Detail: "may note"}},
		Disclosure: []AuthorityItem{{ID: "c91e407a", Subject: "77be04ac…", Scope: "home/journal", Detail: "may receive"}},
		TakenBack:  []AuthorityItem{{ID: "0be21d6c", Subject: "72b4e191", Detail: "taken back by"}},
	}
}
