---
id: "0003"
title: "Private money by hand: notes in a container, and banking by their issuer"
status: examined
of: "rokh at 91889d8, the event RKH3 of contract 3.4, and a design the owner described"
date: 2026-10-03
---

# Private money by hand: notes in a container, and banking by their issuer

> Whether Rokh, as version 1 builds it, can carry money that a person issues,
> hands over without a network, keeps without a wallet, and banks with its own
> issuer; what it weighs in bytes; and where the core stops and the harness
> around it must decide. Every statement says its kind: measured (M), read in
> the code (C), extrapolated (X), proposed (P). Nothing here is a ruling.

## Summary

Rokh is an individual's **append-only** ledger of addressable events, rolling
over one another asynchronously, without lockstep, and asymmetrically. Money
in it is not money by consensus. It is a good that a person makes and offers,
as any other, and its issuer answers for it.

**The owner's design.**
- **The container.** A near-field tag, or a physical coin carrying one, is a
  raw container. What matters is the bytes on it, not the object; only the
  bytes move, and one container may carry more than one token.
- **The note.** An issuer writes a note into a container. Anyone may take its
  bytes and place them, of their own will, in their own ledger.
- **The standing offer.** Whenever a person's ledger and the issuer's meet,
  the issuer exchanges the note under the terms it has undertaken: its
  standing offer.
- **Banking.** The issuer may also bank its notes: a server, their security,
  support, and transfers online to anywhere. A note may come without that
  service; each is a feature of the note.

The study examines two models and measures both:

- **A. Notes in a container** (the owner's design). A note is one event,
  signed by its issuer, carrying one or more tokens and naming no holder. A
  holder takes the note into their own ledger; the issuer's ledger records
  what it redeemed.
- **B. A hand-over chain.** Each hand-over is an event in the issuer's
  ledger, signed by the holder of the step before and naming the next. This
  model was examined first; it is kept because it shows where the core stops.

What was found:

1. **The core already carries both (M, C).** Every event of both models is
   an ordinary RKH3 event, and the core accepts each one. No byte form
   changes.
2. **A note is small, and holds many tokens (M, X).**
   - A note of one token is 363 bytes, and each further token adds 20.
   - A tag of 888 bytes holds a note of up to 26 tokens.
   - One event holds up to 203 tokens under the payload limit of 4,096 bytes;
     a note of 200 is 4,343 bytes.
   - In model B a coin is 347 bytes when issued and 380 per hand-over.
3. **Who holds what is the harness's question, never the core's (M).** In
   model B the core accepts a double hand-over as two heads. It also accepts
   a hand-over written by a key that never held the coin.
4. **Bytes can be copied, so a note's worth rests on its issuer's offer and on
   the people it passed through (C, X).** In model A, anyone who has a note's
   bytes can present them. What the issuer gives for a second presentation of
   the same token is the issuer's standing offer to state, and the issuer's
   ledger records every answer. A holder who takes a note into their own
   ledger records who handed it to them. A note refused later is then a claim
   against that person, before whatever court they chose. Rokh proves who
   did what, and it prevents nothing. The ledger without consensus says the
   same of any unique item: it needs an external ritual, or custody with
   bounded authority (N 2.10), and here the issuer is that custody.

## What was run, where, and on what

On one host, a Linux container on x86-64, with Go 1.26.4, at commit 91889d8
of `main`. Each program ran once; sizes vary by a byte or two with the random
freshness, never more.

- [`data/note_size_test.go`](data/note_size_test.go) signs notes of 1, 10,
  25, 100 and 200 tokens. Each token is a 16-byte serial and a 4-byte value,
  and each note carries the id of the issuer's standing offer.
- [`data/coin_size_test.go`](data/coin_size_test.go) builds model B:
  - an issuer's ledger, with a genesis, one open grant over the address `c`
    for the verb `t`, and one coin;
  - three hand-overs, each signed by the key the previous one named;
  - two hand-overs of the same step by one holder, and one by a key that never
    held the coin.

  It weighs each event and asks the ledger for its verdict on each.
- The size budget of the core, `TestEventSizeBudget` in
  `source/rokh/oracle/size_test.go`, ran beside them.

| file | SHA-256 |
|---|---|
| [`data/note-sizes.txt`](data/note-sizes.txt) | `fa727cffb224442b2d741e9f2b2816ed24ae6341002fc9c28e7c06e4787e1892` |
| [`data/coin-sizes.txt`](data/coin-sizes.txt) | `17463cb84dd9a2de8e115dfe97b37f2bacc7129ed8e673585f7156149da18bdb` |
| [`data/event-sizes.txt`](data/event-sizes.txt) | `06e42a7eb618ab108a681487157046f72ba73656aba8559f7fbc8e42577e694c` |
| [`data/note_size_test.go`](data/note_size_test.go) | `68f2a48c264c251154e050594f8b114702cc01c49c771fd7c150f281fc3a26e7` |
| [`data/coin_size_test.go`](data/coin_size_test.go) | `86f88e63152cb4448d2012405f685cceee9fb089d0d7ddfcd80b5a8af8d35713` |

## Measured

**A note** (M, `data/note-sizes.txt`). One event, signed by its issuer; its
payload is a kind byte, the id of the standing offer (32 bytes), and its
tokens.

| tokens in the note | bytes |
|---|---|
| 1 | 363 |
| 10 | 543 |
| 25 | 843 |
| 100 | 2,343 |
| 200 | 4,343 |

**Model B, a hand-over chain** (M, `data/coin-sizes.txt`). The payload of the
issue is a kind (1 byte), a value (4 bytes) and the first holder's key (32
bytes). That of a hand-over is the next holder's key (32 bytes).

| event | bytes | head | body |
|---|---|---|---|
| the issuer's genesis | 232 | | |
| the open grant over the coins | 334 | | |
| the issue of one coin | 347 | 235 | 112 |
| one hand-over | 380 | 273 | 107 |
| one hand-over, head only | 273 | 273 | — |

The issue with one hand-over is 727 bytes, with two 1,107, and with three
1,487. The genesis and the open grant are the issuer's once, not the coin's.
A reader who knows the issuer holds them already.

**What the core says of model B** (M):
- all five events of the coin's ledger: `accepted`;
- two hand-overs of the same step by its holder: both `accepted`, and the
  ledger has two heads;
- a hand-over by a key that never held the coin: `accepted`.

**The core's own event sizes** (M, `data/event-sizes.txt`): a genesis 311
bytes, a grant 431, a delegated write 436, the default testimony 63. These
carry testimony, and the notes and coins above carry none.
`source/rokh/docs/04-bandwidth.md` still gives 231, 347 and 383, the sizes
before RKH3: its table is out of date, which mission W-20 covers.

## Read in the code

- **A unique item is a higher layer's, and needs custody** (C, N 2.7, 2.9,
  2.10 and 9.4).
  - Rokh makes no common history. A layer above it that makes an item unique
    and spendable brings back the problem consensus answers.
  - Rokh has no way of its own for such an item. Each stakeholder closing the
    same work in their own ledger is enough for shared work, not for a unique
    item, which needs an external ritual that keeps uniqueness and order, or
    custody with bounded authority.
  - The issuer who answers each presentation at contact (P2) is that
    custody. Value and property are a realm that Rokh is not and does not
    judge.
- **One ledger per issuer** (C, contract 3.4). Every event names its carrier,
  the anchor of one ledger. A note or a coin is bound to its issuer's
  ledger wherever its bytes travel, and cannot be moved under another.
- **The bytes of an event are signed, not sealed** (C,
  `source/rokh/event/event.go`). Sealing to readers happens in the vessel's
  envelope (contract 3.1), not in the event. Written on a container, a note
  is public, as the design wants.
- **A ledger takes in another ledger's events only as content** (C). An
  event names its own carrier, and a ledger accepts only events of its own
  carrier. A holder therefore keeps a note in their own ledger as the payload
  of an event of their own, or beside it as content, and points to it by its
  id.
- **The knot between two ledgers already has a form** (C,
  `source/rokh/bond`). A bond is accepted by each of its founders in their
  own ledger, and the ledgers never merge. That is the form the issuer's
  standing offer and a holder's acceptance of it can take.
- **Any key may write under an open grant** (C, contract 4.5). Model B rests
  on it, and it is why the core cannot say who holds a coin there.
- **A head only proves lineage and authorship, nothing of content** (C,
  contract 3.4). A token is in the body, so it cannot travel as a head alone.
- **Concurrent events stay concurrent** (C, contract 4.4 and 4.8). A
  reconcile shows both and chooses neither.
- **The narrow mesh carries news, not events** (C, `source/rokh/announce`,
  `04-bandwidth.md`). An announcement of 19 to 51 bytes says that something
  changed. The event follows over whatever link can carry it, fragmented on
  the narrowest.

## Extrapolated

- **Containers** (X: read in the specifications of common tags, not
  measured). The small near-field tags offer about 144, 504 or 888 bytes of
  memory for data, less the framing of their records; larger ones offer 2 to
  8 KiB. The table takes 12 bytes of framing for one record of a short type,
  and 7 more for each further record.

  | container | model A (notes) | model B (one coin) |
  |---|---|---|
  | 144 bytes | nothing | nothing |
  | 504 bytes | a note of up to 7 tokens | the issue alone |
  | 888 bytes | a note of up to 26 tokens | the issue and one hand-over |
  | 8 KiB | two notes, about 370 tokens in all | the issue and about 20 hand-overs |

- **The narrow mesh** (X: arithmetic on the frame sizes of `04-bandwidth.md`,
  which that document says must be measured on real hardware). A note of one
  token is 2 frames at 222 bytes, and 8 at 51. Under a duty cycle of one
  percent at the slowest rate that is minutes of airtime: possible for money,
  not for many payments a minute.
- **A holder's notes** (X). A thousand notes of ten tokens each are about
  0.5 MB, which a vessel holds without effort (study 0001).

## Proposed

Nothing below changes a byte form. It is a harness: a language game played on
RKH3 events. What would need a ruling is gathered at the end.

**P1. The words of the game.**

| verb | written by, in whose ledger | payload |
|---|---|---|
| `offer` | the issuer, in its own | the terms: what it gives for a token, to whom, how often, and what a second presentation gets |
| `note` | the issuer, in its own | the id of the offer, and the tokens |
| `take` | the holder, in their own | the note's bytes or id, and who handed it over |
| `present` | the holder, in their own, carried to the issuer | the tokens presented, and what is asked in return |
| `answer` | the issuer, in its own | paid, or refused, and why, naming the presentation |

**P2. Contact settles it.** Offline, a note moves as bytes, and anything may
happen to copies. When a holder's ledger and the issuer's meet, the
presentation and the answer are written, each in its own ledger.
- **The issuer's answer** follows its standing offer. One rule an issuer may
  state: the first presentation of a token is paid, and every later one is
  answered with a refusal naming the first.
- **The holder's protection** is their own record of who handed them each
  note. A refused note is a claim against that person, and the parties' court
  decides. Rokh gives it the signed evidence and prevents nothing.

**P3. Three ways to hold a token**, and what each rests on:

| way | what moves | a second spending is | rests on |
|---|---|---|---|
| a note in a container (A) | bytes anyone can copy | refused at contact, by the issuer's offer | the issuer's offer, and the record of who handed it on |
| a hand-over chain (B) | an event naming the next holder | provable, and its signer's | the holder's word, and the court |
| a container that signs | the chip signs once per step and refuses a second | not possible without breaking the chip | the chip |

Which of these a note allows is written in its issuer's offer: a feature of
the note, as the owner said.

**P4. Banking by the issuer.**
- A holder deposits by presenting notes.
- The issuer then moves value inside its own ledger, online, to any point that
  can reach its booth.
- A withdrawal is a new note, written into a container.
- Renewal is the same: notes with long histories are presented, and fresh ones
  are given back.

**P5. Measure on the hardware.** Before anything above is built, weigh a note
on real tags and real radios: the write time of 843 bytes, and the airtime of
363, measured, not computed.

**What would need a ruling, and is not ruled here:**
- whether money stays a harness entirely, or its words (P1) are written into
  a contract of their own;
- whether a ledger may take another ledger's event in whole, as an event and
  not as content, which would change what a carrier binds;
- whether a smaller note is wanted badly enough for a new generation of the
  event, since an RKH3 event of one parent, written by its root, carries 291
  bytes of its own apart from its address, verb and payload: a note of one
  token is 363 bytes, and those three are 72 of them (law 2 of AGENTS.md).

## Not run

- No tag, no radio and no chip were used; every figure for them is read or
  computed.
- No harness was built: P1 to P4 are not code.
- Signing chips on tags were not examined.
- Presentation, answer and renewal were not measured.
- Sizes were measured once, on one host.

## To reproduce

```sh
cp lab/studies/0003-offline-money/data/note_size_test.go source/rokh/oracle/
cp lab/studies/0003-offline-money/data/coin_size_test.go source/rokh/oracle/
cd source/rokh && go test -count=1 -run 'TestNoteSize|TestCoinSize' -v ./oracle
rm oracle/note_size_test.go oracle/coin_size_test.go
```
