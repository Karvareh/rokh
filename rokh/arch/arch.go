// Package arch holds no code. It exists so the layering of docs/07 section 0
// can be enforced by a test rather than by memory.
//
//	pure     canon, seal                     no rokh import at all: bytes and ciphers
//	core     frame, event, ledger, carrier   judges bytes; knows nothing above
//	narrow   announce                        news and knocks
//	work     working, generation, oracle     the boundary before recording
//	bundle   covenant, content, bundle       reads the ledger; writes nothing
//	custom   receipt, harness, bond, answer, selective
//	socket   daemon                          the only door, and it is local
//	courier  cmd/rokh-courier                keyless, untrusted
//	adapter  cmd/rokh-forms                  owns an application's content root
//
// Getting this wrong is how a courier quietly becomes an authority, so it is
// checked.
package arch

// Position is where Rokh stands, written down so a test can hold it there.
//
// Rokh is upstream — upstream of meaning and lineage, not of cryptography and
// the network. It is not a network path, not a content store and not an
// identity system: it takes the free wheels that already turn into service
// and rebuilds none of them. What it says is which lineage a person acted in,
// and what addressed verb they performed.
//
// Where acceptance comes from a program and binds every agent of that program,
// this is not us. In Rokh the structural law is fixed, the source of authority
// is the causal past of the individual's own ledger, and the meaning of a verb
// comes from the harness.
//
// And the place of each wheel is clear because the map of realms is already
// drawn. Rokh is none of those realms: its body runs in one of them, and what
// is seen in all eight is the trace of meaning and lineage.
//
//	— T11, T11.1, T11.2, T11.3, N9.3, N9.4
const Position = "upstream of meaning and lineage; of nothing else"

// Custom is what the "custom" layer means, written down for the same reason.
//
// The receipt is the clearest case. It is not part of Rokh: Rokh knows events
// and nothing else, and it has no opinion about whether a step was begun,
// finished or lost. The intent-and-result pair is a custom the harnesses keep
// on top of the core — useful enough that it ships here, and still not core.
//
// The line is enforced downward, not upward. Nothing in the core may import
// the custom layer, because the moment the ledger knows what a receipt is, it
// has an opinion about work, and the next thing it does is act on one.
//
//	— T10, T10.5, T11.3
const Custom = "a custom the harnesses keep on Rokh, never a law of it"

// Complete is Rokh without a network, written down so a test can hold it there.
//
// Rokh is whole with no network. If it is never connected to another machine,
// nothing is missing from it: the ledger is written, judged, read and verified
// on one machine by one person, and that is the whole of it. Carrying it
// elsewhere is a convenience the core does not know about — which is why no
// core layer imports a transport, and why the courier is keyless and untrusted.
//
// The reason is the same one that runs through the rest of this: a system that
// needs the network to be complete has to be reachable, and what is reachable
// can be reached by someone else.
//
//	— T1.4, N-Axiom5, T8, T11.2
const Complete = "whole with no network; connection adds reach, never completeness"
