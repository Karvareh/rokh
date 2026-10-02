# Peering: disclosure, couriers, content

> This document is the contract for everything outside the core that lets two
> ledgers meet. Nothing here changes the settled core semantics of docs/01-03.
> Where a new rule touches the core, it is named as such and marked as needing
> a separate ruling.

## 0. Layers, and what each one may do

Every rule below belongs to exactly one layer. Getting this wrong is how a
courier quietly becomes an authority.

| layer | package | may | may never |
|---|---|---|---|
| **core** | `frame` `event` `ledger` `carrier` | judge bytes, decide validity, hold keys | know what a peer, a covenant, a want or a file is |
| **narrow** | `announce` | carry news and a public key | carry an event, a payload, or human text |
| **bundle** | `covenant` `content` | read the ledger, decide what *may* cross, seal for a recipient | write an event, decide validity |
| **courier** | `cmd/rokh-courier` | move opaque bytes between two ledgers | sign, hold a key, grant authority, decide validity |
| **adapter** | `cmd/rokh-forms`, others | own an application's content root | change the carrier inventory |

Two sentences that the rest of this document only elaborates:

> **The receiving ledger alone judges bytes.** A courier that lies, drops,
> reorders or replays changes nothing about what is accepted.
>
> **Disclosure is decided by the sender, at bundling time.** A covenant tells
> the sender's bundle layer what it *may* hand a courier. It never makes the
> courier trustworthy, and it never starts a transfer.

## 1. The narrow path is settled

Radio carries **news** and **public-key introduction**. Nothing else. No event,
no payload, no human text ever travels over radio.

### 1.1 `announce` - unchanged

Nineteen bytes bare, 51 maximum, one SF12 frame. Ledger anchor and head as
8-byte prefixes, optional count and address hint. Unsigned: it asserts no right
and moves no data, so a forged one costs at most a wasted fetch.

### 1.2 `hello` - the one addition

```
byte  0      magic 'K'
byte  1      version 0x01
bytes 2..33  one Ed25519 public key, 32 bytes
```

Thirty-four bytes. It fits an SF12 frame with room to spare.

A distinct magic byte, not a new version of `announce`, so neither decoder can
ever accept the other's frame.

**A hello is a knock.** It says only "this key exists and is reachable by
whatever path this frame arrived on". It:

- grants no right;
- starts no transfer;
- is not a share decision;
- is not evidence of anything, being unsigned like `announce`.

A knock may arrive by any path, radio included. What a node does with it is a
human decision, later, elsewhere.

### 1.3 Still asleep

The **transport frame cap** and the **short-id length** (8 bytes or 16). Both
wait on measurement of real hardware. Nothing in this document depends on
either.

**No radio code exists and none is being written.** `announce` and `hello` are
frame codecs. Rokh has no radio driver, no serial port, and no scheduler.

## 2. The tunnel is not Rokh

The encrypted IP path between peers is supplied by the **existing Yggdrasil
layer**, which derives a peer's IPv6 address from its public key. That layer is
not part of Rokh and is not described here.

Three boundaries, stated so they cannot erode:

- **Rokh has no network listener.** The only socket it opens is the local Unix
  socket of `rokh daemon`, mode 0600. There is no TCP code path anywhere and
  none is being added.
- **LoRa is not a Yggdrasil transport.** They are two separate paths. Radio
  carries news and knocks; Yggdrasil carries bundles over wide links.
- **A knock is discovery, not a decision.** Learning a key creates no covenant,
  no peer relationship and no flow.

## 3. Disclosure is not authorship

This is the sharpest boundary in the document.

`rokh.grant` is **authority to write**. It answers "may this key append an event
about this address?" It must never, by extension or convenience, come to answer
"may this key receive that event?"

So disclosure gets its own grammar, its own name and its own evaluator.

### 3.1 Why it is not a core reserved verb

Adding `rokh.share` to the core would extend the settled reserved-verb set of
docs/01, which needs its own ruling. Absent that ruling, the covenant is built
from **ordinary, non-reserved verbs** and evaluated **outside** the core. The
core stays exactly as docs/01-03 describe it.

The consequence is a good one: `covenant` is a bundle-layer package that reads
the ledger. The core never learns what a peer is.

### 3.2 Shape

A **share covenant** is an ordinary event:

```
address  peer
verb     peer.share        (to withdraw: peer.unshare)
payload  canonical TLV, same codec as grants
```

`peer.share` payload:

| tag | name | value |
|---|---|---|
| `0x0001` | `subject` | 32 bytes, the recipient's Ed25519 public key |
| `0x0002` | `scope` | UTF-8 address prefix. Absent means the whole ledger |
| `0x0003` | `sealTo` | 32 bytes, the recipient's X25519 key, optional. See §6 |

`peer.unshare` payload:

| tag | name | value |
|---|---|---|
| `0x0001` | `target` | 32 bytes, the id of the `peer.share` event being withdrawn |

Scope uses the same component-boundary rule as grants: scope `home` covers
`home/journal` and does **not** cover `homestead`.

A covenant is **outbound and one-directional**. It says "these events of mine
may be disclosed to P". It says nothing about what P may send here: inbound
bytes are judged by this ledger on their own merits, exactly as always.

### 3.3 Who may write one

**Only the ledger root, for now.**

The address `peer` is an ordinary address, so a write-delegate holding a broad
scope could otherwise author a covenant and disclose the ledger to itself. The
evaluator therefore ignores any covenant whose author is not the genesis author.

This is enforced in code and tested. **Delegated disclosure - a "may share"
right - is a later ruling.** Until then, disclosure is the owner's act alone.

### 3.4 Evaluator

The covenant evaluator reuses the *causal pattern* of grants and revocations,
and nothing else:

```
active(H) = { s in causalPast(H) : s is a valid peer.share }
          \ { target(u) : u in causalPast(H), u is a valid peer.unshare }
```

Both sets grow monotonically along the graph, so a withdrawn covenant can never
come back to life, and the verdict at any named head is settled by that head's
ancestry alone. This is the same theorem as docs/02 §3, applied to a different
question.

`covenant.ActiveAt(led, heads...)` returns the live covenants at those points.
It is **not** `ledger.ActiveGrants`, which means core writing authority and
keeps that meaning.

The core gains one small, generic, read-only view to make this possible:

```go
// Ledger.CausalPast returns the accepted ancestors of the given points,
// inclusive, in deterministic order.
func (l *Ledger) CausalPast(at ...frame.ID) []frame.ID
```

It knows nothing about peers or covenants. It is a graph query, not a policy.

### 3.5 What a covenant does not do

- It does not **start** a transfer. No stream opens or closes automatically.
- It does not make a courier trustworthy.
- It does not imply the reverse direction.
- It does not survive the scope it names.
- **It does not follow from shared ownership.** Two carriers having the same
  owner implies nothing. A berth - a same-anchor copy of your own ledger - is
  reunited through the docs/08 rite; disclosure to anyone else stays a
  covenant decision.

## 4. The courier

A courier is a keyless, untrusted program **outside the core** that moves opaque
bytes from one ledger to another. Its whole vocabulary is two of the six socket
operations:

```
get    from the source ledger   -> raw, already-signed bytes
append to the target ledger     -> the target judges them
```

It may not sign, hold a key, grant authority, or decide validity. It never
becomes core functionality. If a courier is malicious it can withhold, delay,
duplicate or reorder; it cannot cause anything invalid to be accepted, because
the target's ledger re-parses and re-verifies every byte and evaluates authority
over causal history.

**The sender's bundle layer, not the courier, decides what the courier is
handed.** That is where covenants are read and where scoping happens - the same
rule as docs/04: scoping happens at bundling time, not at sending time.

### 4.1 What a courier cannot do, and it is not a small thing

Every event names the anchor of the ledger it belongs to, and the acceptance
rule rejects any event whose carrier is not this ledger's genesis. So:

> **A ledger cannot accept another ledger's events.**

This was measured, not assumed. Alice bundles two events for Bob under a live
covenant; applying that bundle to Bob's daemon yields `0 accepted, 2 rejected`,
and Bob's ledger is untouched. Applying the same bundle back into Alice's own
ledger yields `2 accepted`, idempotently.

That is correct behaviour, not a defect: a ledger is single-owner by
construction, and letting a stranger's events in would break that.

The consequence is that **holding a peer's events requires a mirror**: a
separate carrier that holds *their* ledger and verifies it against *their*
anchor, kept beside your own and never merged into it. That is not built, and
it needs a ruling - see section 11.

The same-anchor case is different and now has its name and its rite: a copy of
your own ledger on another machine or medium is a **berth**, and berths reunite through the sentence shell - union both ways, each
side judging for itself, heads closing only on the exact reply. See docs/08.

## 5. Wants and receipts

The precise question, which the whole vocabulary exists to answer:

> **After named head H, and within scope S, which bytes are held, wanted,
> offered, or acknowledged?**

Four message kinds. All are **bundle/courier layer**, carried over a wide link
as JSON. **None of them ever travels over radio.**

Every message names the pair `(after, scope)` so that both sides are talking
about the same region of the same graph.

### 5.1 `have`

"After H, within S, I hold these ids."

```json
{"kind":"have","ledger":"<anchor>","after":"<id>","scope":"home/journal",
 "ids":["<id>","<id>"]}
```

`after` is a head the sender believes the receiver already has, so the list is a
difference rather than a census. `after` may be the anchor, meaning "from the
beginning".

### 5.2 `want`

"After H, within S, send me these."

```json
{"kind":"want","ledger":"<anchor>","after":"<id>","scope":"home/journal",
 "ids":["<id>"]}
```

A want is a request, never an entitlement. The sender still consults its
covenants before answering, and may answer with nothing.

### 5.3 `offer`

"I can supply these, at these sizes."

```json
{"kind":"offer","ledger":"<anchor>","after":"<id>","scope":"home/journal",
 "items":[{"id":"<id>","bytes":383}]}
```

An offer lets a receiver decide before spending a narrow or metered link. It is
the natural place for a byte budget to be applied.

### 5.4 `ack`

"I judged these; here is what happened."

```json
{"kind":"ack","ledger":"<anchor>",
 "accepted":["<id>"],"rejected":["<id>"],"pending":["<id>"]}
```

The three outcomes are exactly the ledger's three verdicts, and they carry their
settled meanings from docs/02: **pending means the ancestry has not arrived, not
that the event is bad.** A courier seeing `pending` should fetch ancestors, not
give up.

An `ack` is a report, not a receipt of custody. It says what one ledger decided,
which is the only thing anyone can honestly report.

### 5.5 Ordering

None is required. A courier may deliver in any order; the receiving ledger's
acceptance is a fixpoint over arrival order, as docs/02 establishes. Wants and
acks are therefore idempotent and safely retried.

## 6. Content

### 6.1 The minimal descriptor

Bulk content never sits in an event: the 4 KiB inline payload limit makes that
structurally impossible. The event carries a **descriptor** instead.

Canonical TLV, same codec as grants:

| tag | name | value |
|---|---|---|
| `0x0001` | `hash` | 32 bytes, sha256 of the content bytes |
| `0x0002` | `size` | 8 bytes, big-endian, the content length |
| `0x0003` | `type` | UTF-8 media type, 1..64 bytes, e.g. `text/markdown` |

Three fields, and no fourth. There is no filename: content is addressed by
hash, and the type gives the extension when one is wanted.

The descriptor is **not core**. The core sees an opaque payload under 4 KiB and
asks nothing about it. `content` is a bundle-layer codec that anyone may use or
ignore.

The conventional verbs for such events are `content.put` and `content.delete`.
Both are ordinary, non-reserved verbs.

### 6.2 Deleting content

The event can never be erased. The **content it referred to** can be, and that
deletion is itself a new, irreversible event:

```
address  photos/2026/beach
verb     content.delete
payload  the descriptor of what was removed
```

History is never rewritten. The ledger grows by recording what happened next.

### 6.3 The path content travels

```
narrow path (radio)   news + knock only.       Never a descriptor, never content.
wide path  (IP, an overlay, a file) events, and then content.
```

The order is fixed and it matters:

1. The **descriptor event** crosses first, as an ordinary event, subject to the
   sender's covenants.
2. The receiver, having accepted that event, now knows a hash, a size and a
   type. It can decide whether it wants the bytes at all.
3. The receiver asks for the content **by hash**, over a wide link.
4. The receiver verifies `sha256(bytes) == hash` before storing anything.

Content is fetched, never pushed, and always after the event that describes it.
A hash arriving without an accepted descriptor event is not a reason to fetch
anything.

**Content does not live in the carrier.** It lives in an application's declared
content root, which belongs to the adapter (§8). The carrier inventory of
docs/05 is unchanged by any of this.

### 6.4 Selective inter-personal encryption

**Defined here; not built.** It belongs to the bundle layer, not the core, and
not the courier.

The problem: a courier is untrusted, and a wide link may be shared. Some content
should be readable only by its recipient.

The shape:

- A covenant may carry `sealTo`, the recipient's **X25519** public key. It is a
  separate key from the Ed25519 signing key, deliberately: signing and
  encryption keys are not interchangeable and conflating them is a known
  mistake.
- To seal for that recipient: generate an ephemeral X25519 key, do ECDH against
  `sealTo`, derive a key with HKDF-SHA256, encrypt with AES-256-GCM. All four
  primitives are in the Go standard library (`crypto/ecdh`, `crypto/hkdf`,
  `crypto/aes`, `crypto/cipher`), so the zero-dependency and no-hand-written-
  cryptography rules hold.
- **Only content is sealed, never the event.** The event stays plainly
  verifiable so any node can check the graph without being able to read the
  bytes. This is the same principle as the carrier: seal the payload, leave the
  structure legible.
- The descriptor's `hash` is the hash of the **plaintext**, so a recipient
  verifies what it actually got, and the same content sealed twice keeps one
  identity.

What is deliberately still open: key rotation, sealing to several recipients at
once, and what happens to already-sealed content when a covenant is withdrawn.
Withdrawal stops future disclosure; it does not unsee what was seen.

## 7. What is core, bundle, courier, adapter

Restated as a checklist, because this is the boundary that erodes first.

**Core** - unchanged by this document except for one generic read-only view
(`Ledger.CausalPast`). It gains no knowledge of peers, covenants, wants,
descriptors or files.

**Bundle** - `covenant` (evaluate disclosure at a head), `content` (descriptor
codec), and later the sealing of §6.4. Reads the ledger; writes nothing.

**Courier** - keyless, untrusted, outside the core, speaking only `get` and
`append`.

**Adapter** - owns an application's content root, translates between an
application and the generic CLI. Never changes the carrier.

**Adapters over the API.** If gRPC or GraphQL ever become useful, they are
adapters over the existing six socket operations, never the core. AI chains are
external API clients and nothing more.

## 8. The first acceptance scenario: one offline laptop

A single-file HTML template with several forms - a business plan, a
brainstorming sheet - and no network at all.

### 8.1 The rule that shapes everything

While a person types, drafts live in temporary local state. **That creates no
event.** Nothing enters the ledger until an explicit command records it.

### 8.2 The path

1. The person types. Drafts sit in browser local state, outside Rokh.
2. They export a commit bundle: an ordinary JSON file.
3. An explicit CLI action then, for each form:
   - stores the committed text as a plain `.txt`, `.json` or `.md` file in an
     **explicitly declared content root outside the carrier**;
   - appends a Rokh event whose payload is the **descriptor**, not the text;
   - prints the event id.
4. Re-rendering the HTML from the ledger shows the latest committed state.
   Further commits create further events.

### 8.3 Boundaries this scenario must not cross

- The content root belongs to the **template or its adapter**, never to the
  core. It must not appear in the carrier inventory and must not make the
  carrier a content store.
- **Rokh is not specialized for this template.** The acceptance test is that the
  generic CLI plus at most a thin adapter suffices.
- A refreshed HTML file is a **mutable local projection**, not ledger history.
  Deleting it loses nothing; it is rebuilt from the ledger.
- `file://` cannot reach a Unix socket. The answer is a **CLI file handoff**:
  the page exports a file, the adapter consumes it. **No listener is added to
  Rokh**, and no port is opened.

### 8.4 Why this scenario is the right first one

Form text easily exceeds 4 KiB, so it exercises the content-reference ruling
rather than talking about it. Both a two-line note and a forty-kilobyte plan
travel the same path: content to a file, descriptor to the ledger.

## 9. No database

Committed content is plain files. SQLite and PostgreSQL are not implementation
choices at this stage and are not presented as options.

If an index is ever needed it must be **outside the carrier, discardable,
rebuildable from the ledger, and never authoritative**. Proposing one is a
separate ruling.

## 10. Persian readiness without changing the project language

The project, its API and this work are English. That is unchanged.

**Textual content** is a different thing from an identifier, and the two rules
are deliberately different:

| | rule |
|---|---|
| **payload** (prose, form text, notes) | any valid UTF-8, preserved **byte for byte**. Persian, ZWNJ (U+200C), anything |
| **address and verb** (identifiers) | combining marks and format or bidirectional characters are **rejected**, as docs/01 §4 sets out |

The identifier policy is not relaxed and is not applied to payloads. Refusing
ambiguity in an identifier is what makes an address mean one thing; prose has no
such duty.

Conformance vectors include ordinary Persian text and ZWNJ in payload content,
and the offline path is tested for byte-exact round-tripping from HTML through
the CLI to storage and back.

**No claim is made of universal Persian-identifier support.** Precomposed
Persian letters pass; decomposed forms and ZWNJ in an address do not. Changing
that is a later ruling.

## 11. Still open, and needing a ruling

1. **Transport frame cap** and **short-id length** - measure real hardware.
1. **Mirrors.** A ledger cannot accept another ledger's events (section 4.1),
   so holding a *foreign* ledger means a mirror: read-only beside yours,
   verified against its own anchor, never merged. Seats declare mirrors as
   such (docs/08 section 2); operating on one beyond reading still needs a
   ruling. The same-owner case is settled: it is a berth, and it reunites.
1. **Causal completeness against scoped disclosure.** A covenant scoped to
   `home/journal` does not cover the authority chain at `rokh`, yet nothing
   verifies without it. Today `bundle` reports how many ancestors are missing
   and offers `--with-ancestry`, off by default, which sends them but discloses
   more than the covenant names. Neither answer is obviously right and the
   choice is a ruling, not a default.
2. **Delegated disclosure** - a "may share" right, so someone other than root
   can author a covenant.
3. **Same-owner mirroring** - its own custody and disclosure ruling.
4. **Sealing details** - key rotation, multiple recipients, and what withdrawal
   means for content already sealed and sent.
5. **Extending the core reserved verbs**, if disclosure should ever become a
   core concept rather than a bundle-layer one.
