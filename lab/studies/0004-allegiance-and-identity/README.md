---
id: "0004"
title: "Allegiance of the moment, and identity among ledgers"
status: examined
of: "rokh at 91889d8, the packages bond, covenant and selective, and a design the owner described"
date: 2026-10-03
---

# Allegiance of the moment, and identity among ledgers

> Two uses of Rokh the owner described: a person's allegiance to one ruler at
> a time, given and taken back at any moment; and the first anchor of a
> ledger as an identity that others come to know. What the code already
> carries of them, what they weigh, and where a harness or a ruling must
> decide. Every statement says its kind: measured (M), read in the code (C),
> extrapolated (X), proposed (P). Nothing here is a ruling. *Pledge* is this
> study's word for an allegiance given.

## Summary

**The owner's design.**

- **Allegiance.** In a democracy of private orders, allegiance cannot be
  fixed by a calendar or by birth. A person pledges to a ruler of their
  choosing and gives that ruler their voice.
  - A person has one voice, so they pledge to one ruler at a time.
  - They may take the pledge back at any moment.
  - The ruler may be anyone they choose: the head of their family, the elder
    of their clan, or someone else.
- **Identity.** The first anchor of a ledger, its genesis, was meant as what
  an identity card or a passport is, except that it is proven by the person
  among other ledgers, not given by a state.
  - A ledger may stay on its own.
  - To be credited, it must be known by others, and its owner may disclose
    the history of what they did, or not.

What was found:

1. **An identity is an anchor, and an anchor proves only itself (M, C).** A
   genesis is 232 bytes, and its id is the anchor every later event of that
   ledger names.
   - What it says of a person is nothing until other ledgers record something
     naming it: an acceptance of a bond, a receipt, a testimony.
   - Each such record sits in the other person's own ledger and is signed
     there.
   - Credit is therefore what others have written, and it can be checked
     anywhere the records are shown.
2. **A pledge is a bond, and the code already makes one (M, C).** A bond of
   two founders, the person and the ruler, doing *allegiance*, is accepted
   by each in their own ledger: 446 and 453 bytes, both accepted. The bond
   package's own acts include leaving, which closes the future and is not
   settling. So a pledge taken back is an event too, and the record of who
   pledged to whom, and when, only grows.
3. **"One at a time" is not in the core, nor in the bond package (C).** A
   person may accept two pledges, and from two seeds two pledges can be made
   concurrently. Nothing chooses between them. The rule the owner gave must be
   a rule of the harness, and what it does with concurrent pledges needs a
   ruling.
4. **There is no global count, and there cannot be (C).** A ruler's support
   is the pledges it can show: each signed by the pledger, accepted by the
   ruler, with no leaving known to whoever counts.
   - A leaving written offline reaches the ruler only when the ledgers meet.
     So a count is always a count *as far as known*, at the moment of
     contact.
   - Its time is testimony, as every time in Rokh is.
5. **Disclosure is already its own grammar (C).**
   - What a ledger shows to a peer is a covenant: outbound, one-directional,
     and evaluated outside the core.
   - What it shows to everyone is a read-open address.
   - Showing one field and hiding the rest is the selective package. Its byte
     form is an open ruling (T13.1), and this study does not decide it.

## What was run, where, and on what

On one host, a Linux container on x86-64, with Go 1.26.4, at commit 91889d8
of `main`.

[`data/pledge_size_test.go`](data/pledge_size_test.go) gives a person and a
ruler a ledger each, builds the leaf of a founding bond with both anchors
doing `allegiance`, and has each accept it in their own ledger with
`bond.Accept`. It weighs each event and asks each ledger for its verdict. It
ran once.

| file | SHA-256 |
|---|---|
| [`data/pledge-sizes.txt`](data/pledge-sizes.txt) | `9fff8f9443286f1ed25e16ff1399b0616c2183092f10a6e984eb53ce45932adf` |
| [`data/pledge_size_test.go`](data/pledge_size_test.go) | `81a80ddb92024e8b9cd8de8932d84928991e4d36b340a9dcc7711e953e066347` |

## Measured

(M, `data/pledge-sizes.txt`)

| what | bytes | the ledger's verdict |
|---|---|---|
| a genesis: the anchor of an identity | 232 | |
| the leaf of a pledge: two anchors, doing *allegiance* | 187 | |
| the person's acceptance, "I pledge" | 446 | accepted |
| the ruler's acceptance, "I answer for it" | 453 | accepted |

An acceptance is larger than a note of study 0003 because the bond package
writes its payload as canonical JSON, with a declared type.

## Read in the code

- **One ledger, one anchor** (C, `source/rokh/ledger/ledger.go` and contract
  4.5). A ledger takes only events that name its own genesis as their
  carrier, so an event of another ledger cannot be put into it. Nobody writes
  into an identity's ledger but its root and the keys holding a grant there,
  each grant leading back to the root; an open grant, which only the root
  writes, lets any key write within its scope.
- **A bond never merges two ledgers** (C, `source/rokh/bond/bond.go`).
  - A leaf names its founders' anchors and the work undertaken, and its name
    is the hash of its bytes.
  - It stands closed when every founder has accepted that same name in their
    own ledger. That proves mutual acceptance and nothing more.
- **Five acts, kept apart** (C, `bond.Acts`): joining, leaving, amending,
  returning an item, and settling the account. Leaving closes the future and
  does not clear a debt. A pledge withdrawn and a pledge that leaves an
  obligation behind are already two different things.
- **Disclosure is not authorship** (C, `source/rokh/covenant`). A grant is
  the right to write; a covenant is what may be shown to a peer. The core
  never learns what a peer is.
- **Testimony, not time** (C, law 4 of AGENTS.md and `source/rokh/oracle`). An event
  may carry attestations, but a clock in Rokh is testimony to be weighed,
  never an order.

## Extrapolated

- **Credit without a centre** (X). An anchor costs nothing to make, so a
  thousand new anchors are worth nothing until someone with standing records
  something naming them.
  - Credit spreads only through records written by ledgers that already have
    some. That is a web of knowing, not a register.
  - It is as strong as the care with which people record whom they know.
- **The size of a voice** (X). A pledge, the ruler's acceptance of it, and
  the leaf both name are 1,086 bytes together. The leaf is needed to check
  them, for an acceptance names it only by its hash. A ruler with a thousand
  pledges holds about 1 MB of them, and showing them all fits a vessel and a
  courier easily. On the narrow mesh a pledge with its leaf is three frames at
  222 bytes, so a pledge can be given by radio, and a count cannot.

## Proposed

Nothing below changes a byte form. It is a harness. What would need a ruling
is gathered at the end.

**P1. A pledge** is a founding bond of two anchors, the person's and the
ruler's, doing `allegiance`.
- The person accepts it with their place: *I pledge*.
- The ruler accepts it with theirs: *I answer for it*.
- Taking it back is the bond's own leaving, written by the person in their
  ledger.
- The ruler may leave too, which ends its answering for that person.

**P2. One at a time, as the harness reads it.**
- A person's live pledge is the last one in their ledger's causal order with
  no leaving after it. A new pledge then implies leaving the one before.
- Two pledges with neither in the other's causal past are concurrent: the
  harness shows both and counts neither, until the person writes a leaving of
  one. The person, not the count, chooses.

**P3. A count as far as known.**
- A ruler's count is the pledges it holds that are live as far as it knows.
- Each is shown with its leaf, the person's acceptance and the ruler's own.
  Whoever reads the count can check every one, and can ask the pledgers'
  ledgers for any leaving the ruler has not seen.
- A count states the moment of its making as testimony.

**P4. An identity shown as much as its owner wants.** Others' records about a
person are kept in those others' ledgers. The person gathers what they want to
show:
- to everyone, by a read-open address;
- to one peer, by a covenant;
- one field at a time, once the selective byte form is ruled.

Not showing is always possible, and costs only the credit not shown.

**What would need a ruling, and is not ruled here:**
- whether "one voice, one ruler at a time" is a rule of Rokh, or of each
  harness, and what concurrent pledges count for;
- whether a pledge carries further: whether a family head who has the
  pledges of their family, and pledges to an elder, gives the elder those
  voices too, or only their own;
- whether a pledge counts with the person's acceptance alone, or only once
  the ruler has accepted it as well;
- the byte form of disclosure field by field, already open as T13.1.

## Not run

- No count was built and no harness exists: P1 to P4 are not code.
- Concurrent pledges were not run; the claim about them is read in the code.
- Credit was not modelled with numbers, and many anchors made to fake credit
  were not examined.
- Sizes were measured once, on one host.

## To reproduce

```sh
cp lab/studies/0004-allegiance-and-identity/data/pledge_size_test.go source/rokh/oracle/
cd source/rokh && go test -count=1 -run TestPledgeSize -v ./oracle
rm oracle/pledge_size_test.go
```
