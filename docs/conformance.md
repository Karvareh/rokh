# Rokh — conform the code to the specification

## 0. The relationship has been inverted

Until now the documents described the code. From today the code conforms to the
documents.

Two files are the specification. Read both in full before touching anything:

- **`رساله.md`** — the treatise, in Persian: thirteen bands, ninety-one numbered propositions. Cite as **`T`**.
- **`without-consensus.md`** — nine sections; an English rendering of the Persian original, with the original's numbering. Cite as **`N`**.

`N` is not merely commentary: its own header states that its design rulings bind every
implementation. And the two documents number independently — `3.5` exists in both and
means different things — so **every citation must carry its source prefix**: `T3.5`,
`N4.10`. A bare number is a bug.

Neither document is uniformly binding. Each contains three kinds of statement, and you
must treat them differently:

| kind | how to treat it |
|---|---|
| **design ruling** | binding. Implement it or fail. |
| **base profile** | today's chosen numbers and algorithms. Implement as *named, versioned* things, never as scattered constants. |
| **status and field notes** | reports. `T13` is a status ledger, not a requirements list. |

Everything in `docs/01`–`docs/08` and everything in the Go tree is **temporary
evidence**, not authority. Where code and specification disagree, the code is wrong —
including where it is older, larger, tested, or mine.

### 0.1 What this document is now

This is the work order, and it is kept as the record of what was asked. Its §4 sections
say things like "entirely absent from the code today"; those were true when written and
several are no longer.

**Do not read a status claim here as the present state.** The present state is
`conformance/STATE.md`, which no one writes by hand — `go test ./conformance` generates
it from `conformance/obligations.tsv`, and the same run refuses any claim it cannot
verify against the tree. Where this document and that file disagree, that file is right,
for the same reason the specification beats the code: it is the one that is checked.

Each stale section below carries a **`— now:`** line saying where the work actually
stands and where in the tree to look. The instruction above it is left exactly as it was
given.

## 1. Your only constraint

Technical reality: what computer science, cryptography, the operating system and the
language actually permit. **Not** the existing code, not its size, not the effort of
changing it.

You may merge packages, split them, rename every identifier, rewrite from zero.
Nothing here is precious. What matters is that the result be small, legible, and
demonstrably the specification made executable.

For removal, follow whatever reversible ritual this project already binds you to — this
work order does not grant, and cannot grant, an exemption from it. Report what left the
active body, but do not treat shrinking the tree as a goal. Conformance is the goal.

## 2. Citations are the contract

Every proposition has a stable **citation id** — a number in most cases, a name in the
seven axioms of `N3`. That id is its identifier. Never renumber, never invent one. A rule realised in code carries its citation in an English
doc comment:

```go
// A rejected event is never stored. Rejection means "never was", not
// "exists but bad".  — T3.3
```

### The conformance map — build this first

A test that:

1. parses both specification files and extracts every proposition id, prefixed —
   including the seven axioms of `N3`, which carry names rather than numbers. Their
   citation form is fixed here and is **not** an invention on your part:
   `N-Axiom1` … `N-Axiom7`. Use exactly those and no others;
2. scans the Go tree for `— T…` / `— N…` citations;
3. reads a checked-in ledger of propositions **deliberately not in code**;
4. fails on any of: an unknown id cited; an **evidence-bearing** item neither cited nor
   listed; a listed item whose stated reason is not one of the three below; an item both
   cited and listed with a contradictory status; a cited **evidence-bearing** item with
   **no test exercising it**.

The evidence requirement — citation, test, or a stated reason for absence — applies only
to evidence-bearing items. For a status or field note, recording its kind is enough.

Each item in the map also carries a **kind**, taken from the specification's own
three-way distinction:

- **design ruling** — needs a citation *and* a test.
- **base profile** — needs a citation *and* a test.
- **status or field note** — needs neither; it is a report, not a requirement.

Only the first two are evidence-bearing. Do not force a status line into a code comment
to make the map green.

Only three reasons excuse the absence of an evidence-bearing item:

- **`ruling open`** — `T13` marks the ruling itself unsettled.
- **`outside the design`** — deliberately excluded.
- **`field test`** — not a code question at all.

**`stated, not built` is not an excuse.** It means the ruling is *closed* and the work
is owed. Everything so marked in `T13` is on your list, not off it.

And note that `T13.1`, `T13.5` and `T13.6` each carry several states in one number. The
map must be able to say *partly cited, partly open* for a single id, rather than forcing
one verdict.

A citation proves lexical coverage, not behaviour. That is why every **evidence-bearing**
citation must reach a test. A status or field note never does.

## 3. Words

Code and identifiers are English; the treatise is Persian, and the sentence
surface has been English since version 1. The English words the code uses for
the treatise's terms are fixed: event, ledger, anchor; address, verb; lineage
(the parent edges), concurrent; grant, revoke; disclosure, carrier, open;
working state, nonce; descriptor, receipt; unknown (a receipt outcome only);
harness, application (a program bound to a booth), base profile.

## 4. The work, in dependency order

The chain is: **bytes and generations → carrier → atomic commit → working state →
authority and receipt → disclosure and harness → bond.** Read the cited propositions in
the source before implementing; the summaries below are pointers, not the ruling.

### 4.1 Profiles and generations — T3.4, T3.6, T3.7, T8.7, N4.10

Numbers and algorithms scattered as constants become named, versioned profiles: payload
cap (T3.4), nonce length (T3.6), the algorithm list (N4.10), carrier layout (T8.7).

The binding rule is T3.7: **signed bytes are never rewritten.** A new generation brings
its own name and its own verifier; upgrading touches no existing byte. Old generations
stay readable *for as long as their verifier is kept* — say exactly that, and if an old
cipher breaks, what moves is the security boundary, not the history.

**Keep the generations separate.** The event profile, the carrier profile and the local
port are not bound to one tag by any ruling. If you choose to align them, name that an
engineering choice in the code, not a requirement of the specification.

### 4.2 The nonce, and both collisions — T3.6, T3.2, T3.5, T13.8

Freshness is today an optional oracle attestation. In the specification it is a **field
of the event**, from a source defined by its property — unpredictable, independent of
prior events — with its length from the profile.

- Two byte-identical writes from the same head must be two events, not one.
- Two unseen writers can still draw the same nonce. **Do not claim otherwise anywhere.**

**Three situations look alike and must not be merged.** The current ledger returns the
prior state for any id it already holds, without comparing the bytes. That is the right
behaviour **for the first row only**; the third row is precisely the case those bytes
would have distinguished. Keep them separate:

| situation | correct behaviour |
|---|---|
| the same bytes arrive again (replay, re-sync) | no-op, valid — never an error |
| a nonce collision while authoring | redraw and retry |
| two *different* byte strings under one full name | the provisional guard below |

If you implement "reject any id already present", you break deduplication and healthy
replay.

**Short-name collision (T3.5) is missing from the code.** Today short names are a fixed
four or eight bytes. The ruling: a short name is a guide for the eye only; acceptance is
always by full name and bytes; and when two short names meet, the short name lengthens.
A fixed-width prefix on the narrow path is fine *only* because it is a hint and never an
acceptance criterion — make that impossible to misuse.

**Full-name collision (T13.8) is an open ruling. Do not close it.** The ledger *can*
detect it: two different byte strings under one name are distinguishable by comparing
the bytes. What it cannot do is refer to both unambiguously. So detect it and refuse to
proceed with a named error — and mark that in code as a **provisional fail-closed
guard**, not as the ruling and not as conformance. Never silently pick one, overwrite,
or "handle" it.

### 4.3 The carrier as a contract — T8, T8.1, T8.6, T8.7, N4.6

Express the carrier by its properties, with today's layout as one implementation:
self-contained, portable, encrypted such that **nothing of the ledger is readable
without the key** — object names included — and no unintended persistent plaintext.

Bound the claim honestly (T8, T12.6): Rokh can promise not to write outside the carrier
deliberately; it cannot promise what the host leaves in memory, swap, backups or
metadata. And separate ledger content from what is unavoidably public: bootstrap
metadata, sizes, timings.

**Key recovery is outside the design (T8.6, T13.7)** — a ruling, not an inability. Write
the phrase that way every time: *key recovery*. Bare "recovery" collides with crash
recovery in T8.5, which is required. Note also that the two losses differ: lose the key
and the encrypted copies are useless; lose every copy and the key is worthless.

### 4.4 The commit point and crash recovery — T8.5

Recording has a point: nothing before it, everything after. Power failure or a killed
process leaves the carrier at its last sound state with no half-written event. Storage
does not guarantee long atomic writes, so Rokh must say for itself when it has accepted.

The gap to close and to test is the whole commit, not one file write: today an object is
placed and *then* a ref is moved. Kill the process between those two and assert
soundness.

### 4.5 The working state, and the shell's meaning — T4.4, T4.5, T4.1, T4.3, T8.2, T9.2, T9.1, T9.3, T9.4, T13.3

The largest missing subsystem. Today `write` composes, signs and commits in one motion.
The specification puts a boundary in the middle:

> What is on the screen and what you have done is **working state**: not final, and
> changeable as often as you like. One explicit act turns it into an event, and after
> that there is no way back.

Requirements:

- A working state that is not the ledger, and exactly one explicit act that crosses.
- Before crossing, the shell shows the **shape of the effect** (T9.2) — the predicted
  effect, not the effect. An effect that lands outside Rokh does not come back. And
  seeing a preview is not the same as human consent (T12.6).
- Whether the working state lives in memory or on disk **is not an architectural
  question** (T4.5). Design so either works.
- The prohibition is on **authorship**, not on machines (T4.1). Timers, watchers,
  indexers, couriers, always-awake services may exist and run. The rule is narrow and
  exact: none of them **creates an event, and none crosses the working-state boundary on
  its own**. Carrying an already-written event — what a courier does — is not authorship
  and stays permitted.
- **Showing is also an effect** (T4.3) and passes the gate *proportionate to it* —
  authority, effect, receipt. This does not make a local preview into a disclosure or a
  recording; T9.2 requires exactly such a preview before committing, so the two must not
  collapse into one another.
- T13.3 closes *presenting the carrier as a real folder*. It does not make the working
  folder identical to the encrypted carrier — keep them distinct. And the prohibition is
  **no unintended persistent plaintext outside the declared boundary**, not "no
  persistent plaintext at all": T4.5 explicitly permits a disk-backed working state.
- Keep the eight verbs' stable meaning and the separation of meaning from one shell's
  syntax (T9.1, T9.3, T9.4): understanding → showing the shape → authority → committing
  → receipt each keep their own place.
- How a diff becomes events — one, or several — is an engineering choice. Say which you
  chose and why; do not present it as the ruling.

### 4.6 Causal authority and event bounds — T6.5, T6.1, T6.2, T6.3, T6.4

Causal judgement, revocation and clocklessness already exist in the code; treat those as
a **regression guard** with citations and tests, not as new work.

What is new: a grant's bound is an **event** — the end of the work, a revocation, or a
**pre-named termination event**. That last one is not implemented: naming it, carrying
it, and evaluating it is the work. The clock is testimony (T5.3) and testimony does not
close authority; if any expiry reads a clock, remove it. Scope narrows only (T6.1).

— now: built. `bond.Bound` carries the three ways a keeping ends, `Terminal` being the
pre-named event, and it has no field that holds a time — the absence is the point.
`bond.Passage.Check` holds it across every stage of the cycle.

### 4.7 The receipt ritual and the bound of effect — T10, T10.1–T10.5, T10.6, T10.7, T2.2, T4.3

**Entirely absent from the code today.** There is no intent, no outcome, no receipt.

— now: built, in `receipt/`. Both halves are recorded, signed events: `Ritual.Open`
records the intent and the ledger's name for it is what the result answers through the
ledger's own parent link (`Ritual.Close`). The payload declares its own type inside the
signed bytes, so `Concerns` reads address, verb **and** type — an outcome verb over an
intent payload is neither. The four effect declarations are `harness.Effects`, required
to bind and with no default. A repeat handle is checked against the ledger, and the code
says plainly what that cannot do.

Build it as the harnesses' contract on Rokh, not as core (T10.5): intent before the
step, outcome after, the second naming the first as parent (T10.1); the six witnesses —
origin, authority, addressee, receipt, state, way back (T10.2); failure carries a
receipt too (T10.3); a receipt whose ending is lost closes as **unknown** (T10.4) — and
that word is used nowhere else.

Two adapter rules:

- A harness recognises its events by **address, verb and payload type** — never by the
  name, which is an unreadable hash (T10.6) — unreadable, but **not a secret**: it
  reveals equality and lets anyone test a guessed byte string (T3.2). And it does not
  *reject* foreign events;
  rejection belongs to the ledger and means "never was" (T3.3). It simply does not pass
  them through its own aperture.
- Rokh alone cannot guarantee exactly-once for an effect outside it (T10.7). A
  cooperating destination can, given a deduplication identifier. Every harness declares
  up front: idempotent repeat, retry, compensation, unknown ending.

### 4.8 Disclosure — T7.3, T7.4, T7.5, T7.6, T13.1

`T13.1` carries **two** subjects; both are owed:

- **Sealing content for a named recipient is a closed ruling, not built.** Today the
  code records `SealTo` and nothing more. Actually seal.

  — now: built, in `seal/`. X25519 to a reading key that is not a signing key, decided
  before the courier is handed anything. The byte form of *selective* disclosure below
  is still the open ruling it was, and `selective.Commit` is still an interface.
- **Selective disclosure**: the want is settled — one field shown alone, signature still
  verifiable, nothing else revealed. The **byte form is an open ruling** (field name,
  type and position, ordering, repetition, binding to the event, and what the receiver
  needs besides the field). Leave a clean seam. Do not design the wire format.

And T7.3 closes exactly this much: effective disclosure always derives from the owner's
authority but **may be exercised by a separate, narrow, revocable delegation**. It does
not mean any covenant author gains disclosure. A write delegate never gains it by
extension; a disclosure delegate never widens its own scope.

Keep T7.4 in this package: disclosure is decided **when the bundle is closed**, not when
it is sent, so what must not travel never reaches the courier. And T7.6: encryption
hides neither size nor timing nor traffic shape, and withholding a past the receiver
needs suspends judgement rather than denying it.

### 4.9 The harness covenant and the first connection — T11.10, T13.5

Every harness declares four things: its **namespace** for addresses and verbs, its
**version**, the **list of what it can do**, and its **response to an unknown verb**.
The core judges bytes, signatures and authority; it does not know the meaning of an
unknown verb and must not guess.

T13.5 separates three states: the design covenant is **closed**; the executable
connection is **not built**; whether anyone else actually writes is a **field test**.
Your part is the middle one.

— now: the middle one is built. The daemon takes a `bind` op with the four clauses and
the four effect declarations, and `Server.write` holds a bound harness to its covenant:
a verb it did not declare gets the answer it declared, and nothing guesses. The version
travels inside the signed payload (`harness.Stamp`), so it is bound to the event and not
to a table of what was running that week. The field test is still a field test.

### 4.10 The bond and shared work — T11.5, T11.6, T11.7, T11.8, T11.9, T13.6

Closed ruling, not built: a **foundation leaf** — a unique byte sequence naming the
founders' anchors and the undertaking, named by the hash of those bytes; each party
accepting that name **in their own ledger**; ledgers never merging; each shared work
carrying its own leaf and name, separate from the bond's; closure proving **mutual
acceptance only**, never an exclusive effect on anything outside the ledgers.

— now: built, in `bond/`. `Leaf` and `Name` for the leaf; `Accept` for what one person
writes in their own ledger; `Settled` for closure across several. The test runs two real
ledgers with different anchors and checks that neither holds a single event of the
other's. There is no function here that merges two ledgers and there will not be. The
four authorities are `bond.Held`, separated so that none is ever derived from another;
the shared pen is `bond.Pen`; the cycle is `bond.Stage`, and its transitions are left
open — `Permitted` refuses all sixty-four, obvious ones included, because succession is
still the owner's to settle.

Carry the constraints with it: joining, leaving, amending the leaf, returning an item
and settling are five separate acts, and leaving closes the future without erasing an
open debt (T11.7). A bond is a lasting knot between independent owners; an organisation
and a system may serve it but neither creates it (T11.5).

**T11.8 and T11.9 are hard bounds, not a finished work package.** T11.8 still depends on
two open rulings — root key rotation (T13.2) and succession (T13.6) — so build only what
those do not block. The bounds that *are* closed and belong in whatever you build:

- a separate anchor per person; **no shared root key and no ledger that owns everyone**;
- read, write, acting on another's behalf, and custody kept apart, none implying another;
- a shared item carried with its share, its custodian, and a receipt of return;
- the **complete life cycle** as named stages: entry, birth, **reaching the capacity to
  hold one's own ledger**, separation, incapacity, death, succession, and the stage in
  which an account stands closed. Each is a *state*, not a ritual: the stages are a
  closed bound, while their transitions and the authority behind each are open (T13.6);
- and the two prohibitions of T11.9: no one becomes the owner of another person, and a
  custodian does not become owner of what is in their keeping — while a bond survives a
  change of custodian.

**Root rotation (T13.2) and succession transitions (T13.6) remain open rulings. Do not
invent them.**

## 5. What you must not do

- **Do not close a ruling the specification marks open.** If the code needs a decision
  that is not there, stop and say so. An invented answer buried in code is worse than a
  missing feature.
- **Do not add a token, a consensus, a majority vote, or a winning chain** to the core.
  And keep the no-consensus claim as narrow as `N2.8`–`N2.12` made it: it is a claim
  about the ledger of one person, not about every institution or every unique item.
- **Do not turn a degree into acceptance** (T12.2). If an evaluator is ever wired in, it
  reports; no threshold makes a degree an acceptance.
- **Do not widen a claim.** Open source does not prove the running binary; reproducible
  builds give the *expected* bytes and verifying the installed and running bytes is
  another loop (T12.6). Absence of network does not make a compromised host safe
  (T12.6). Integrity of a presented history does not prove its completeness (T5.6).
  Structural union is **commutative and non-rewriting** — that is what T5.4 says, and it
  says nothing about semantic conflict (T5.5). Do not restate it as "conflict-free". Hash equality is a computational assumption; byte equality is proven only by
  comparing bytes (T3.2).
- **Do not reintroduce a clock** as an ordering, an authority bound, or a tie-break.
- **Do not add an external network path** — no IP, no listener beyond the machine.
  The local unix socket of the current profile (N4.10) is not that and stays.
- **Do not let any background process create an event, or cross the working-state
  boundary on its own.** Carrying an event already written is not authorship.

## 6. Acceptance

1. The conformance map exists and passes under the strict rules of §2 — including the
   requirement that every **evidence-bearing** citation reaches a test, and that
   `stated, not built` never excuses an absence.
2. `go build ./... && go test ./...` green, `gofmt` clean, still zero dependencies
   outside the standard library — recorded as a **discipline**, since it is not a
   technical boundary.
3. The externally visible, profiled choices — payload cap, nonce length, signature and
   hash, key derivation and its rounds, carrier layout, socket mode — come from named
   profiles, with event, carrier and port generations kept separate unless you argue
   otherwise explicitly. This is not a demand that every internal constant be profiled.
4. The working state exists and is tested; no path writes an event without one explicit
   act; and showing passes the gate proportionate to it, without a local preview being
   treated as a disclosure or a recording.
5. A process killed between placing an object and moving a ref leaves the carrier sound.
6. Receipts exist: intent, outcome, the six witnesses, failure, unknown ending.
7. No open ruling has been quietly closed — in particular T13.8 and the succession
   transitions of T13.6.

## 7. How to report back

Not "what I built". In this order: **which propositions moved from absent to cited and
tested**; which are still absent and under which of the three permitted reasons; where
the specification forced you to notice it is itself wrong or incomplete; and what left
the active body.
