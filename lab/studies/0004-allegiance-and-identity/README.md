---
id: "0004"
title: "Allegiance of the moment, and identity among ledgers"
status: examined
of: "rokh at 91889d8, the packages bond, covenant, harness and selective, and a design the owner described"
date: 2026-10-03
---

# Allegiance of the moment, and identity among ledgers

> Two uses of Rokh the owner described: a person's allegiance, given
> one-sided to whom they choose, taken back at any moment, and counted as
> voices carried onward; and the first anchor of a ledger as an identity that
> others come to know. What the texts and the code already say of them, what
> they weigh, how the count the owner described behaves on signed ledgers,
> and where a harness or a ruling must decide. Every statement says its kind:
> measured (M), read in the code (C), extrapolated (X), proposed (P). Nothing
> here is a ruling. *Pledge* is this study's word for an allegiance given, and
> *voice* for the one vote that each person is.

## Summary

**The owner's design.**

- **Allegiance.** In a democracy of private orders, allegiance is fixed
  neither by a calendar nor by birth.
  - A pledge is one-sided. A person gives it to whom they choose, and it
    counts by their act alone. Two people who each pledge to the other, of
    their own will, have made a pact.
  - Every person is one voice. A person holds their own voice and every voice
    pledged to them, and may pledge all of it onward. The head of a family
    holds the voices of its members, the elder of a clan those of several
    heads, and several elders may form a village. These are abstractions of
    the natural act of giving one's vote, or of shaking someone's hand.
  - A person can always see where their voice finally arrived. Whether they
    reached that person directly or through others makes no difference.
  - A pledge to two halves the voice between them.
  - A pledge may be taken back at any moment, and its ruler may be anyone.
  - Allegiance and money, the owner thinks, take their meaning, the way they
    are written and their format in the harnesses, for Rokh records events.
    This is not ruled.
- **Identity.** The first anchor of a ledger, its genesis, was meant as what
  an identity card or a passport is, except that it is proven by the person
  among other ledgers, not given by a state.
  - A ledger may stay on its own.
  - To be credited, it must be known by others, and its owner may disclose
    the history of what they did, or not.

What was found:

1. **The texts already put allegiance in a harness (C).**
   - Each harness binds with Rokh by a covenant of four clauses: its own space
     for addresses and verbs, its version, the list of what it can do, and its
     answer to a verb it does not know. The core judges bytes, signatures and
     authority, and does not know or guess what a verb means (T11.10, built as
     the `harness` package).
   - The ledger without consensus places allegiances among institutions, a
     realm that Rokh is not and does not judge (N 9.4). Rokh makes no common
     history; being common, if it is ever needed, is the work of a higher
     layer (N 2.7).
   - A count of pledges is not a vote on any ledger: no verdict depends on it
     (N-Axiom6).
   - Read by these texts, a pledge's meaning, its form and its count are a
     harness's, as the owner thinks. Whether the words of a pledge become a
     contract that every harness reads is still open.
2. **A one-sided pledge is one ordinary event in the pledger's own ledger
   (M).**
   - Written as a harness writes, a pledge naming one anchor is 429 bytes,
     and each further anchor adds 67. A pledge naming nobody, which takes it
     back, is 363.
   - All 21 pledges of a village of 21 ledgers were accepted.
   - The ruler writes nothing. The village ends with 16 or 16.5 voices, and
     its ledger holds nothing but its genesis.
   - The bond package is the form of the pact, not of the pledge (C). A bond
     stands closed when each founder has accepted it in their own ledger:
     two one-sided acts.
3. **The count the owner described is a flow of voices, and on that village
   it holds (M).**
   - Each voice moves along the live pledges, split equally between several,
     and ends with whoever pledges to nobody.
   - The heads carry 4.5, 3 and 2.5 voices, the elders 8.5 and 3.5.
   - A member's voice arrives whole at the village through three pledges, as
     does the voice of one who pledged to it directly. A voice split between
     two heads arrives as two halves.
   - Every voice can be traced to where it ends.
   - The count needs nothing of the core but what the core keeps already:
     each ledger's verdict on each pledge, and each pledge's parents.
4. **Three cases the rule does not yet settle (M).**
   - Two who pledge only to each other hold two voices that end nowhere, and
     what they carry grows without end.
   - In a cycle with a way out every voice arrives, but "own and gathered"
     counts them more than once on the way: two people carry 4 and 3.
   - Two pledges from two seeds that have not met are concurrent, and the
     ledger has two heads. Split between them, the village ends with 16.5
     voices and a neighbour with 1.5; counted for neither until the person
     writes again, with 16 and 1.
5. **A count shows only what was disclosed, and an anchor costs nothing (C,
   X).**
   - To see where a voice arrived is to read every onward pledge on the way.
     Each sits in its own pledger's ledger, shown as that person chooses
     (T7.2).
   - Anyone can make anchors, and each would be a voice. A voice is a person
     only as far as someone knows them: the handshake the owner names.
   - There is no global count. A count is a count as far as known at the
     moment of its making, and its time is testimony.
6. **An identity is an anchor, and an anchor proves only itself (M, C).** A
   genesis is 232 bytes, and its id is the anchor every later event of that
   ledger names.
   - What it says of a person is nothing until other ledgers record something
     naming it: a pledge, an acceptance of a bond, a receipt, a testimony.
   - Each such record sits in the other person's own ledger and is signed
     there.
   - Credit is therefore what others have written, and it can be checked
     anywhere the records are shown.
7. **Disclosure is already its own grammar (C).**
   - What a ledger shows to a peer is a covenant: outbound, one-directional,
     and evaluated outside the core.
   - What it shows to everyone is a read-open address.
   - Showing one field and hiding the rest is the selective package. Its byte
     form is an open ruling (T13.1), and this study does not decide it.

## What was run, where, and on what

On one host, a Linux container on x86-64, with Go 1.26.4, on the source of
commit 91889d8 of `main`.

- [`data/pledge_size_test.go`](data/pledge_size_test.go) gives a person and a
  ruler a ledger each, builds the leaf of a founding bond with both anchors
  doing `allegiance`, and has each accept it in their own ledger with
  `bond.Accept`. It weighs each event and asks each ledger for its verdict.
  It ran once. Read now, it weighs a pact written as a bond.
- [`data/count_test.go`](data/count_test.go) binds a harness covenant for
  pledges, gives 21 people a ledger each, and writes their pledges, each
  signed by its pledger in their own ledger. It weighs the pledges and asks
  each ledger for its verdict. It then reads them as a harness would, takes
  as live the pledges that no later pledge of the same ledger has in its
  causal past, and counts the voices by the owner's rule: once with
  concurrent pledges split between them, once with them counted for neither.
  - It ran three times: once as a trial in a copy of the source, and twice
    for the record.
  - The first record was not kept, because its header took the version of
    Go from outside the module.
  - Every figure agrees across the three. Only the order in which a pledge's
    anchors are listed differs, because anchors are random.

| file | SHA-256 |
|---|---|
| [`data/pledge-sizes.txt`](data/pledge-sizes.txt) | `9fff8f9443286f1ed25e16ff1399b0616c2183092f10a6e984eb53ce45932adf` |
| [`data/pledge_size_test.go`](data/pledge_size_test.go) | `81a80ddb92024e8b9cd8de8932d84928991e4d36b340a9dcc7711e953e066347` |
| [`data/count.txt`](data/count.txt) | `9d782cf62d93319a0d28b750b3e990ef8734a808078ca583017d8ca52dcb53cf` |
| [`data/count_test.go`](data/count_test.go) | `71e8863a2f1ea7b9e56d427ed54b011f258257e97ea81c0240d8e7530f3251b3` |

## Measured

**A one-sided pledge** (M, `data/count.txt`). It is written as a harness
writes: the verb `pledge.give` at the address `pledge/allegiance`, and a
payload in canonical JSON that carries the harness's type and version and
the anchors named.

| what | bytes | the pledger's own ledger |
|---|---|---|
| a genesis: the anchor of a person | 232 | |
| a pledge naming one anchor | 429 | accepted |
| a pledge naming two anchors | 496 | accepted |
| a pledge naming nobody: the pledge taken back | 363 | accepted |
| the 21 pledges of the village | 9,077 | all accepted |

The person whose two pledges stand for two seeds that have not met has a
ledger with two heads.

**The count** (M, `data/count.txt`). The village:

- three families, of three, two and one members, each member pledging to the
  head of their family; the first two heads pledging to one elder, the third
  to another; both elders pledging to the village;
- one pledging to the village directly, and one splitting between the first
  head and the third;
- two pledging only to each other;
- two in a cycle with a way out: one pledging to the other and to the
  village, the other pledging back;
- one writing two pledges on one parent, as two seeds that have not met
  would, one to the village and one to a neighbour who pledges to nobody;
- one pledging to the first head, and later taking it back.

Under either reading of concurrent pledges:

- the heads carry 4.5, 3 and 2.5 voices, and the elders 8.5 and 3.5;
- the two in the cycle with a way out carry 4 and 3, and both their voices
  reach the village;
- the two who pledge only to each other carry without end, and their two
  voices end nowhere;
- the one who took their pledge back ends with their own voice.

Where the voices end:

| | concurrent pledges split | concurrent pledges for neither |
|---|---|---|
| the village | 16.5 | 16 |
| the neighbour | 1.5 | 1 |
| the one with two seeds | 0 | 1 |
| the one who took it back | 1 | 1 |
| held in the closed cycle | 2 | 2 |
| in all | 21 | 21 |

Where one voice ends, with concurrent pledges split:

- a member of the first family: the village, 1, through the head and the
  elder;
- the one who pledged directly: the village, 1;
- the one who split: the village, 1, as two halves;
- the one with two seeds: the village 0.5, and the neighbour 0.5;
- one of the cycle with a way out: the village, 1;
- one of the two who pledge only to each other: held in the closed cycle, 1.

**A pact written as a bond** (M, `data/pledge-sizes.txt`).

| what | bytes | the ledger's verdict |
|---|---|---|
| a genesis | 232 | |
| the leaf: two anchors, doing *allegiance* | 187 | |
| one founder's acceptance, "I pledge" | 446 | accepted |
| the other's acceptance, "I answer for it" | 453 | accepted |

An acceptance is larger than a note of study 0003 because the bond package
writes its payload as canonical JSON, with a declared type.

## Read in the code

- **A harness binds with Rokh, and the core does not read its meaning** (C,
  `source/rokh/harness`, T11.10 and T11.11).
  - A covenant has four clauses: a namespace, a version, the verbs it can do,
    and its answer to a verb it does not know, to refuse or to ignore. A verb
    is the namespace, a full stop, and the rest.
  - `Stamps` writes the harness's type and version at the front of its
    payload, inside the signed bytes.
  - `Read` sorts an event into the harness's own known verb, an unknown one
    refused or ignored, or not its own. The core is not asked.
- **Allegiance is an institution's** (C, N 2.4, N 2.7 and N 9.4).
  Allegiances and institutions are a realm of their own, and judging the
  realms is not Rokh's. A common history, if it is ever needed, is a higher
  layer's work, and concurrent acts that fight in the world are for the
  application layer to resolve.
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
  returning an item, and settling the account. In a pact written as a bond,
  leaving closes the future and does not clear a debt.
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
  - A count inherits it: a thousand anchors pledged to one person are a
    thousand voices to whoever does not ask who they are.
- **The size of a voice** (X).
  - A voice is one pledge of about 430 bytes. A village of a thousand people
    rests on about a thousand pledges, some 0.43 MB, and showing them all
    fits a vessel and a courier easily.
  - On the narrow mesh a pledge is two frames at 222 bytes, so a pledge can
    be given by radio, and a count cannot.
  - A pact written as a bond is 1,086 bytes with its leaf. The leaf is needed
    to check it, for an acceptance names the leaf only by its hash.

## Proposed

Nothing below changes a byte form. It is a harness. What would need a ruling
is gathered at the end.

**P1. A pledge** is one event in its pledger's own ledger, in the space of a
pledge harness: the verb `pledge.give` at the address `pledge/allegiance`,
naming every anchor of the pledger's present choice.
- It counts by its pledger's act alone. The ruler writes nothing.
- The live pledge is the last in the ledger's causal order. A new pledge
  replaces the one before; to add a ruler, the pledger names both. What two
  concurrent pledges count for is gathered below.
- Naming nobody takes the pledge back.

**P2. A pact** is two pledges toward each other. When the two want a named
undertaking, with joining, leaving and settling kept apart, it is a bond.
Two who pledge allegiance to each other and to nobody else are the closed
cycle of finding 4, and what their voices count for is gathered below.

**P3. The count.**
- Each voice starts at its own anchor and moves along the live pledges, split
  equally, until it reaches an anchor that pledges to nobody.
- Whoever holds a count shows the pledges it rests on. Whoever reads it can
  check every one, trace every voice, and ask the pledgers' ledgers for a
  later pledge the counter has not seen.
- A count states the moment of its making as testimony.

**P4. The trace.** A person sees where their voice arrived by reading the
onward pledges on the way. A pledge harness can make its space read-open
(contract 4.5), so that every pledge is shown to everyone; or it can carry a
voice only as far as the pledge carrying it is shown to those whose voices
it carries.

**P5. An identity shown as much as its owner wants.** Others' records about a
person are kept in those others' ledgers. The person gathers what they want to
show:
- to everyone, by a read-open address;
- to one peer, by a covenant;
- one field at a time, once the selective byte form is ruled.

Not showing is always possible, and costs only the credit not shown.

**What would need a ruling, and is not ruled here:**
- whether the words of a pledge stay one harness's, or are written into a
  contract every harness reads, as study 0003 asks of money;
- what the voices of a closed cycle count for: for nobody, for its members
  together as one holder, or each for its own pledger;
- whether what a person carries is shown where a cycle with a way out counts
  a voice twice, or only where each voice ends;
- what two concurrent pledges count for: split between them, or for neither
  until the person writes again;
- whether a pledge to several is always split equally, or by shares its
  pledger names;
- whether every pledge is shown to everyone, so that every voice can be
  traced, or only to those whose voices it carries;
- whose voice counts as one: every anchor, or only an anchor that someone
  known has recognised;
- the byte form of disclosure field by field, already open as T13.1.

## Not run

- No harness was bound to a booth, and no count exists in the source: P1 to
  P5 are not code, and `data/count_test.go` counts inside a test.
- The count ran on one village of 21 people. Larger villages, longer chains,
  cycles of more than two, and shares other than equal were not run.
- The concurrent pledges were written on one parent by one program, not by
  two seeds that met in a reconcile.
- Credit was not modelled with numbers, and many anchors made to fake voices
  were not examined.
- Sizes were measured on one host.

## To reproduce

```sh
cp lab/studies/0004-allegiance-and-identity/data/pledge_size_test.go source/rokh/oracle/
cp lab/studies/0004-allegiance-and-identity/data/count_test.go source/rokh/oracle/
cd source/rokh && go test -count=1 -run 'TestPledgeSize|TestPledgeCount' -v ./oracle
rm oracle/pledge_size_test.go oracle/count_test.go
```
