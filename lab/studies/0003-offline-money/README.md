---
id: "0003"
title: "Private money by hand: a coin on a tag, and banking by its issuer"
status: examined
of: "rokh at 91889d8, the event RKH3 of contract 3.4, and a design the owner described"
date: 2026-10-03
---

# Private money by hand: a coin on a tag, and banking by its issuer

> Whether Rokh, as version 1 builds it, can carry money that a person issues,
> hands over without a network, keeps without a wallet, and banks with its own
> issuer; what one coin weighs in bytes; and where the core stops and the
> harness around it must decide. Every statement says its kind: measured (M),
> read in the code (C), extrapolated (X), proposed (P). Nothing here is a
> ruling.

## Summary

Rokh is an individual's **append-only** ledger of addressable events, rolling
over one another asynchronously, without lockstep, and asymmetrically. Money
in it is not money by consensus: it is a good that a person makes and offers,
as any other, and its issuer answers for it. The owner's design, examined
here:

- An issuer's seed issues coins, each one unique.
- A coin is handed over by an event appended to it: *passed to that key*.
  The event is written on a near-field tag, or on a physical coin carrying
  one, in the clear: anyone may read it.
- Taken into a wallet, which is a Rokh ledger, the coin arrives as another
  event, now the wallet's.
- So a coin can be kept and handed on without any wallet at all.
- The issuer may bank its coins: a server, their security, support, and
  hand-overs online to anywhere. A coin may also come without that service;
  each is a feature of the coin.

What was found:

1. **The core already carries it (M, C).** A coin's issue and its hand-overs
   are ordinary RKH3 events in the issuer's ledger. Under one open grant
   (contract 4.5), every holder's key may write a hand-over, and the core
   accepts all of them. No byte form changes.
2. **One coin weighs 347 bytes when issued, and 380 more per hand-over (M).**
   The issue with one hand-over is 727 bytes, with two 1,107, with three
   1,487.
   - The common small near-field tags hold about 144, 504 or 888 bytes (X, as
     their specifications say). So a coin fits a small tag with at most one
     hand-over, and a longer life needs a larger tag or a checkpoint.
   - On the narrow mesh a hand-over is 2 frames of 222 bytes, or 8 of 51
     (X); it travels with fragmentation, as every event does.
3. **The core decides nothing about custody (M).**
   - Two hand-overs of the same step, by the same holder, are both accepted,
     side by side, as two heads.
   - A hand-over written by a key that never held the coin is accepted too.
   - Who holds a coin is therefore not the core's question but the harness's:
     the language game played on these bytes. That is how the owner put it,
     and the code agrees.
4. **A double hand-over can be proven, not prevented (C, X).** Both events
   are signed, so the holder who made them cannot deny them. Offline, nothing
   but the tag itself stops a second hand-over.
   - A coin held *without a wallet* has its holding key written on the tag in
     the clear. Whoever copies the bytes holds it, and a double hand-over is
     then nobody's in particular.
   - That coin is as unique as its tag, and no more.

## What was run, where, and on what

On one host, a Linux container on x86-64, with Go 1.26.4, at commit 91889d8
of `main`:

- [`data/coin_size_test.go`](data/coin_size_test.go) builds an issuer's
  ledger: a genesis, one open grant over the address `c` for the verb `t`,
  and one coin.
  - Then three hand-overs, each signed by the key the previous one named.
  - Then two hand-overs of the same step by one holder, and one by a key that
    never held the coin.
  - It weighs each event, and asks the ledger for its verdict on each.
  - It ran once; sizes vary by a byte or two with the random freshness, never
    more.
- The size budget of the core, `TestEventSizeBudget` in
  `source/rokh/oracle/size_test.go`, ran beside it.

| file | SHA-256 |
|---|---|
| [`data/coin-sizes.txt`](data/coin-sizes.txt) | `17463cb84dd9a2de8e115dfe97b37f2bacc7129ed8e673585f7156149da18bdb` |
| [`data/event-sizes.txt`](data/event-sizes.txt) | `06e42a7eb618ab108a681487157046f72ba73656aba8559f7fbc8e42577e694c` |
| [`data/coin_size_test.go`](data/coin_size_test.go) | `86f88e63152cb4448d2012405f685cceee9fb089d0d7ddfcd80b5a8af8d35713` |

## Measured

**The bytes of one coin** (M, `data/coin-sizes.txt`). The payload of the issue
is a kind (1 byte), a value (4 bytes) and the first holder's key (32 bytes);
that of a hand-over is the next holder's key (32 bytes); the address is
`c/` and a 16-character serial.

| event | bytes | head | body |
|---|---|---|---|
| the issuer's genesis | 232 | | |
| the open grant over the coins | 334 | | |
| the issue of one coin | 347 | 235 | 112 |
| one hand-over | 380 | 273 | 107 |
| one hand-over, head only | 273 | 273 | — |

| a coin as it travels | bytes |
|---|---|
| the issue alone | 347 |
| the issue and 1 hand-over | 727 |
| the issue and 2 hand-overs | 1,107 |
| the issue and 3 hand-overs | 1,487 |

The genesis and the open grant are the issuer's once, not the coin's: a
reader who knows the issuer holds them already.

**What the core says** (M):

- all five events of the coin's ledger: `accepted`;
- two hand-overs of the same step by its holder: both `accepted`, and the
  ledger has two heads;
- a hand-over by a key that never held the coin: `accepted`.

**The core's own event sizes** (M, `data/event-sizes.txt`): a genesis 311
bytes, a grant 431, a delegated write 436, the default testimony 63. These
carry testimony; the coin above carries none, which is why its events are
smaller. `source/rokh/docs/04-bandwidth.md` still gives 231, 347 and 383, the
sizes before RKH3; its table is out of date, which mission W-20 covers.

## Read in the code

- **One ledger per issuer** (C, contract 3.4). Every event names its carrier,
  the anchor of one ledger, so a coin's whole life sits in its issuer's
  ledger, wherever the bytes travel. A hand-over cannot be moved under another
  ledger.
- **Any key may write under an open grant** (C, contract 4.5). The grant
  names its scope and verbs; the event is accepted when the grant is live in
  its causal past and covers its address and verb. Nothing else is asked.
  That is what lets a stranger hand a coin over, and what lets every holder do
  it without the issuer.
- **The bytes of an event are signed, not sealed** (C,
  `source/rokh/event/event.go`). Sealing to readers happens in the vessel's
  envelope (contract 3.1), not in the event. Written on a tag, an event is
  public, as the design wants.
- **A head only proves lineage and authorship, nothing of content** (C,
  contract 3.4). The next holder is in the body. So a hand-over cannot travel
  as its head alone; 380 bytes is its floor on RKH3.
- **Concurrent events stay concurrent** (C, contract 4.4 and the ledger's
  fold). A reconcile shows both and chooses neither. A double hand-over is
  therefore visible to whoever holds both, and settled by nobody inside the
  core.
- **The narrow mesh carries news, not events** (C, `source/rokh/announce`,
  `04-bandwidth.md`). An announcement of 19 to 51 bytes says that something
  changed; the event follows over whatever link can carry it, fragmented on
  the narrowest.

## Extrapolated

- **Tags** (X: read in the specifications of common tags, not measured). The
  small near-field tags offer about 144, 504 or 888 bytes of user memory,
  less a few bytes of record framing; larger ones offer 2 to 8 KiB.
  - 144 bytes holds no coin.
  - 504 bytes holds the issue alone.
  - 888 bytes holds the issue and one hand-over.
  - 8 KiB holds the issue and about 20 hand-overs.
- **The narrow mesh** (X: arithmetic on the frame sizes of `04-bandwidth.md`,
  which that document says must be measured on real hardware). A hand-over
  is 2 frames at 222 bytes and 8 at 51. Under a duty cycle of one percent at
  the slowest rate that is minutes of airtime: possible for money, not for
  many payments a minute.
- **A wallet's coins** (X). A wallet keeps a coin as one event of its own,
  pointing to the coin's last hand-over by its id (32 bytes), with the coin's
  events kept beside it. A thousand coins with three hand-overs each are about
  1.5 MB, which a vessel holds without effort (study 0001).

## Proposed

Nothing below changes a byte form: it is a harness, a language game played on
RKH3 events. What would need a ruling is gathered at the end.

**P1. The words of the game.** At an issuer's address `c/<serial>`:

| verb | written by | payload |
|---|---|---|
| `i`, issue | the issuer | kind, value, the first holder's key |
| `t`, hand-over | the current holder | the next holder's key |
| `r`, redeem | the current holder | what is asked for in return |
| `a`, answer | the issuer | paid, or refused, and why |

A refusal is an event like any other, signed by the issuer, and as public as
the issuer makes it: the issuer may write it at a read-open address, or seal
it to the holder alone.

**P2. Custody, as the harness reads it.** A coin's holder is the key that the
last valid step names. A step is valid when it is signed by the key the step
before it named, and the issue names the first. Two valid steps from the same
step are a double hand-over: the harness shows both, as the core does, and
names the key that signed both. What follows is for the issuer, or for a
court the parties chose, to decide.

**P3. Three ways to hold a coin**, and what each rests on:

| way | where its key lives | a double hand-over is | rests on |
|---|---|---|---|
| in a wallet | the holder's ledger, sealed | provable, and the holder's | the holder's word, and the issuer's court |
| bearer, on a plain tag | on the tag, in the clear | possible by any copy, and nobody's | the tag not being copied |
| bearer, on a tag that signs | inside a chip on the tag, which signs once per step and refuses a second | not possible without breaking the chip | the chip |

Which of these a coin allows is written in its issue, as its kind: a feature
of the coin, as the owner said.

**P4. Banking by the issuer.**
- A holder deposits by handing the coin over to the issuer's key.
- From then on the coin moves inside the issuer's ledger, online, to any
  point that can reach the issuer's booth, without bytes travelling by hand.
- A withdrawal is a hand-over from the issuer to the holder's key, written on
  a tag again.
- The issuer also renews: it takes a coin with a long chain in, and gives back
  a new issue with no hand-overs. That keeps a coin small enough for its tag.
  How a renewed coin closes the old one, so that the old chain cannot be spent
  again, is the issuer's rule to publish.

**P5. Measure on the hardware.** Before anything above is built, weigh the
coin on real tags and real radios: the write time of 727 bytes, and the
airtime of 380, measured, not computed.

**What would need a ruling, and is not ruled here:**
- whether money stays a harness entirely, or its words (P1) are written into
  a contract of their own;
- whether renewal (P4) may rest on a checkpoint the issuer signs, which is
  near question D-12;
- whether a smaller hand-over is wanted badly enough for a new generation of
  the event, since on RKH3 380 bytes is the floor (law 2 of AGENTS.md).

## Not run

No tag, no radio and no chip were used; every figure for them is read or
computed. No harness was built: P1 to P4 are not code. Signing chips on tags
were not examined. Renewal, redemption and refusal were not measured. Sizes
were measured once, on one host.

## To reproduce

```sh
cp lab/studies/0003-offline-money/data/coin_size_test.go source/rokh/oracle/
cd source/rokh && go test -count=1 -run TestCoinSize -v ./oracle
rm oracle/coin_size_test.go
```
