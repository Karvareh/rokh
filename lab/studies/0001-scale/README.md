---
id: "0001"
title: "How far one Rokh goes"
status: examined
of: "rokh at 4111458, the release 1.0.0 with the repair of ledger.Load"
date: 2026-10-01
---

# Scale: how far one Rokh goes

> What one Rokh, as version 1 builds it, holds and costs as it grows, measured
> on one host; why, where the code says so; what the numbers imply beyond what
> was run; and what could change. Every statement is one of four kinds, and
> says which: **measured** (M), **read in the code** (C), **extrapolated** (X),
> **proposed** (P). A limit of version 1 is called a limit of version 1: of
> this implementation, or of its contract. None of the limits found here is
> shown to be a limit of Rokh as its texts describe it.

*This study was `rokh/docs/11-scale.md` in rokh, with its raw data in
`rokh/docs/11-scale/`, until commit b8f909a; its history to then is there.
Paths named here are paths of rokh, and `STATE.md` and `AGENTS.md` are rokh's.
Its raw data is in [`data/`](data/README.md).*

## Summary

**What was run.** Fifteen benchmarks, in `rokh/bench` and `rokh/cmd/rokh`, on
one host (§1), against the production code of commit 4111458; their raw outputs
are in [`data/`](data/README.md). They measured vessels of 4 MiB to
1 TiB of *capacity* holding thirteen short notes each (to 16 GiB on a disk,
beyond that in memory); one thing of 1 MiB to 2 GiB of *content*; one history
of up to a million events; up to 8,000 keys granted in one ledger and a million
keys at an open address; envelopes of 1 to 33 readers; up to 128 concurrent
writers on 32 doors, all with one key; up to 16 concurrent readers; 24 seeds
through the command line and 2,000 in one ledger. No person, booth session,
socket or network took part (§0).

**What was found.**

1. (M) One short note writes the last pack, every inventory segment and a head:
   (1 + ⌈N/4096⌉) slabs. That is 0.56 MiB at 4 MiB of capacity and 256 MiB at
   1 TiB, whatever the content; every commit writes at least 1/4096 of the
   capacity, and takes about 5 µs for every slab of it, 23 s at 1 TiB (§M1).
   (C) A commit writes and verifies every segment (§C1); the contract asks only
   the changed ones.
2. (M) Opening costs 86 µs an event of the history in a bare ledger, 146 at a
   door, 210 for each command of the command line; a million events load in
   86 s, in a process of 5.4 GiB at its highest (§M4, §M5). (C) Version 1
   verifies every event at every opening and holds the whole ledger in memory
   (§C8).
3. (M) At a door a write, a status and a log grow with the history: a write
   takes 25 ms at 1,000 events and 1.2 s at 300,000 (§M5). (C, and one profile)
   6.5 s of the 7.0 s its answers took at 100,000 events went to finding the
   branch references by opening every branch pointer ever recorded (§C2, §M12).
4. (M) Grants, revocations and keyring changes take memory as the square of
   their number: 2 GiB for grants to 8,000 keys, and as much again to revoke
   them; a lineage of 2,000 seeds holds 1.3 GiB (§M6, §M11). (C) Every such
   event keeps a whole copy of the set of all of them in its past (§C3).
5. (M) A carrier records about 35 commits a second whatever the doors and
   writers; a writer's wait grows with the queue, and unfairly: among 128
   writers on 32 doors, 11 of 1,024 writes were refused after 15 s. No write
   answered `recorded` was lost (§M9).
6. (M) A large thing takes four times its size in memory, 8 GiB for 2 GiB, and
   bringing it slows as it grows, 78 MiB/s to 26 (§M3).
7. (C) Version 1 fixes: at most 32 readers in an envelope and 32 slot cells in
   a vessel (contract 3.1, 4.6); at most 2^22 slabs of 2^26 bytes, 256 TiB of
   capacity; 4 KiB of payload and sixteen parents to an event (§C6).
8. (X) By the same rates: a vessel of 256 TiB writes 64 GiB for one note; a PiB
   of capacity is beyond the version 1 format; a billion events take a day of
   one processor to open and 2 to 3 TB of memory; grants to some 20,000 keys
   fill this host; the billionth seed of a line holds four billion events (§X).
9. (P) The limits of items 1 to 6 are choices of this implementation, which
   changes inside version 1 remove without changing a byte form (§P1). The
   numbers of item 7 belong to the version 1 contract, which a new generation
   may change (§P2). Many people are, by a reading of the texts that is the
   owner's to confirm, many Rokhs and the bonds between them (§P3).

**The first limits a Rokh meets as it grows.**

| | met at | limit | evidence | belongs to |
|---|---|---|---|---|
| 1 | the 33rd reader of an address; the 32nd key besides the owner's to open a vessel | an envelope names at most 32 readers; a vessel holds 32 slot cells | C, M (the 33rd refused) | the v1 contract (3.1, 4.6) |
| 2 | a few writers at once | one commit at a time per carrier, about 35 a second; waits unfair; refusals after 15 s among 128 writers | M, C | this implementation (`turn`, the door) |
| 3 | about 10^5 events | a door's write, status and log walk the whole vessel or ledger | M, C, one profile | this implementation |
| 4 | about 10^4 keys granted, or seeds given | authority sets copied whole per event: memory as the square | M, C | this implementation |
| 5 | about 100 GiB of capacity | every commit writes and twice verifies every segment | M, C | this implementation (the contract writes the changed ones) |
| 6 | a thing of a few GiB | a large thing held four times over; growing step by step | M, C | this implementation |
| 7 | about 10^6 to 10^7 events | every opening verifies the whole history and holds it in memory | M, C (§C8) | this implementation; N2.6 asks that one person *can* verify everything, not that each opening does |
| 8 | 256 TiB of capacity | the most slabs and the largest slab | C | the v1 contract (1) |
| 9 | about 10^5 founders | a bond's leaf lists every founder and each must hold all of it | M, C | the leaf as v1 writes it (`bond.Leaf`); T11.6 says what a leaf must say, not how |

**Not run, no evidence, and the state of the tests:** §F and §T.

## 0. What is counted

The ledger knows keys, not people, and a carrier has a capacity apart from what
it holds. The report keeps these apart.

- **Key.** A signing key (Ed25519, the author of an event) or a reader key
  (X25519, named in an envelope); a keyring generation pairs the two. Every
  count of keys below counts these.
- **Person.** Not in the ledger. A person may hold several keys (generations,
  devices, delegations), and a key does not show who holds it (contract U9). No
  benchmark models a person. Where a table speaks of people (§X3), it assumes
  one key each and says so; with more keys a person, the same limits come at
  fewer people.
- **Session.** A booth session of `rokh.booth/1` (hello, prove). None was used:
  the doors were driven through `(*daemon.Server).Handle` in one process,
  without a socket.
- **Door.** One opening of a carrier by a daemon (`daemon.Server`), as a
  separate program opens it. **Writer** and **reader**: one stream of requests
  to one door, running beside the others. In `BenchmarkScaleWriters` every
  writer signs with the same delegated key, `clerk`: 128 writers are 128
  streams, one key, no session.
- **Operation.** One request to a door: a write, a status, a log. One write is
  one event and one commit.
- **Event, commit, record.** An event is a signed entry of the ledger; a commit
  is one generation of a vessel and may carry many events (the carriers behind
  `BenchmarkScaleDoor` were filled 2,000 notes a commit); a record is what a
  pack holds: an event, a chunk of content, a branch pointer.
- **Founder.** An anchor named in a bond's leaf: one ledger, not one person and
  not one key.
- **Capacity and content.** A vessel's capacity is its N slabs of S bytes,
  every one of them a file whether used or not; its content is the records its
  packs hold. A vessel is always N + 4 files of N·S + 4·64 KiB, whatever it
  holds (contract 1): its files show its capacity, never its content (U1). The
  capacity sweeps (§M1, §M2) held thirteen notes of 510 bytes, about 6.6 KB, at
  every capacity from 4 MiB to 1 TiB, and their costs follow the capacity.
  Large content was measured apart: one thing of up to 2 GiB (§M3) and up to
  300,000 notes at a door (§M5).

## 1. Where, on what, and from which data

- **Host.** Four virtual processors at 2.10 GHz with the processor's AES and
  SHA instructions; 15.7 GiB of memory; one virtual disk, ext4; Linux 6.18; Go
  1.26.4 linux/amd64. Everything ran as the superuser, which matters to two
  tests (§T); nothing measured depends on permissions.
- **Code.** The production code of every run is that of commit 4111458: the
  release, ba7e695, with the repair of `ledger.Load` (§C7). Every later commit
  changes only benchmarks and documents. The commit that holds each benchmark
  as it ran is listed with its raw output in
  [`data/README.md`](data/README.md).
- **Raw data.** [`data/`](data/README.md) keeps the seventeen benchmark
  outputs, the runner's start and end times, the summary of the one processor
  profile, and the logs of the two test suites, with the SHA-256 of each as
  written. One line of each benchmark output was changed: it named the
  processor's product, which no document of Rokh names.
- **How often.** Each size ran once (`-benchtime 1x`): one sample, no spread.
  Two measurements ran twice: the writers (the second run agrees with the first
  within 15 per cent) and the door at 100,000 events (a write in 366 and
  367 ms, a status in 211 and 213 ms). One measurement ran at a time; short
  checks of seconds ran beside the sparse vessels of 256 GiB and 1 TiB and
  beside the second run of the writers
  ([`data/README.md`](data/README.md)), so those rows may carry a
  little of that load.
- **Memory.** "Held" is the live heap after a collection; "process at most" is
  the process's high-water mark, which only rises within one run, so it is read
  for the largest size of each run.

## M. Measured

Every number in this part was read from a raw output in
[`data/`](data/README.md), named under each table with the benchmark
that wrote it.

### M1. Vessel capacity, in memory

A vessel is N slabs of S bytes: S from 256 KiB to 64 MiB, N from 16
to 4,194,304. Every 4,096 slabs have one inventory segment, held in a slab of
its own; G = ⌈N/4096⌉. `BenchmarkScaleVessel` makes a vessel on the sparse
medium (`bench/scale_test.go`): a vessel fills every free slab and every
unwritten head file with randomness nothing reads back, the measurement leaves
that fill zero, and the medium keeps an all-zero file as its length alone.
Everything the vessel writes or reads for itself (packs, segments, heads) is
still sealed, written, read, hashed, opened and counted; what is not measured
is a disk's time. The note recorded is a head of 180 bytes and an envelope of
330, 510 bytes in all, after eight others: thirteen notes, about 6.6 KB, at
every capacity.

| capacity | N | G | make | open | read to open | held open | one note | written | read | files written | written ÷ 510 B |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 4 MiB | 16 | 1 | 7 ms | 0.8 ms | 0.5 MiB | 0.26 MiB | 4.6 ms | 0.56 MiB | 1.8 MiB | 3 | 1,157 |
| 64 MiB | 256 | 1 | 27 ms | 1.7 ms | 0.5 MiB | 0.27 MiB | 4.9 ms | 0.56 MiB | 1.8 MiB | 3 | 1,157 |
| 1 GiB | 4,096 | 1 | 0.61 s | 20 ms | 0.5 MiB | 0.48 MiB | 23 ms | 0.56 MiB | 1.8 MiB | 3 | 1,157 |
| 4 GiB | 16,384 | 4 | 2.9 s | 59 ms | 1.25 MiB | 1.1 MiB | 67 ms | 1.31 MiB | 3.3 MiB | 6 | 2,699 |
| 16 GiB | 65,536 | 16 | 8.9 s | 0.24 s | 4.25 MiB | 3.8 MiB | 0.30 s | 4.31 MiB | 9.3 MiB | 18 | 8,867 |
| 64 GiB | 262,144 | 64 | 26 s | 1.4 s | 16.2 MiB | 14 MiB | 1.26 s | 16.3 MiB | 33 MiB | 66 | 33,539 |
| 256 GiB | 1,048,576 | 256 | 94 s | 4.7 s | 64 MiB | 56 MiB | 5.2 s | 64.3 MiB | 129 MiB | 258 | 132,229 |
| 1 TiB | 4,194,304 | 1,024 | 346 s | 19 s | 256 MiB | 224 MiB | 23.2 s | 256.3 MiB | 513 MiB | 1,026 | 526,987 |

| capacity | S | N | one note | written | read | written ÷ 510 B |
|---|---|---|---|---|---|---|
| 16 MiB | 1 MiB | 16 | 9.2 ms | 2.06 MiB | 5.6 MiB | 4,241 |
| 4 GiB | 1 MiB | 4,096 | 27 ms | 2.06 MiB | 5.6 MiB | 4,241 |
| 64 GiB | 1 MiB | 65,536 | 0.37 s | 17.1 MiB | 35.6 MiB | 35,081 |
| 1 GiB | 64 MiB | 16 | 0.72 s | 128.1 MiB | 320.6 MiB | 263,301 |
| 4 GiB | 64 MiB | 64 | 0.76 s | 128.1 MiB | 320.6 MiB | 263,301 |
| 16 GiB | 64 MiB | 256 | 0.84 s | 128.1 MiB | 320.6 MiB | 263,301 |

"one note" is the mean of five commits (two where one writes 256 MiB or more);
"make" is the vessel's own work of making N files, without a disk. The 1 TiB
vessel is the most slabs the format allows. The process held 7.1 GiB at its
highest, most of it the medium's record of the files. Raw:
[`vessel.txt`](data/vessel.txt), `BenchmarkScaleVessel`.

### M2. Vessel capacity, on the disk

`BenchmarkScaleVesselDisk` makes the vessel in a folder on the disk, every slab
filled with the host's randomness and every file flushed, and records the same
note.

| capacity | S | N | make | made at | open | one note |
|---|---|---|---|---|---|---|
| 4 MiB | 256 KiB | 16 | 0.04 s | 110 MiB/s | 2 ms | 7.5 ms |
| 64 MiB | 1 MiB | 64 | 0.37 s | 173 MiB/s | 13 ms | 33 ms |
| 1 GiB | 1 MiB | 1,024 | 6.2 s | 167 MiB/s | 13 ms | 38 ms |
| 4 GiB | 1 MiB | 4,096 | 44 s | 93 MiB/s | 29 ms | 59 ms |
| 16 GiB | 1 MiB | 16,384 | 179 s | 91 MiB/s | 0.11 s | 163 ms |
| 1 GiB | 64 MiB | 16 | 8.6 s | 119 MiB/s | 0.34 s | 1.34 s |
| 4 GiB | 64 MiB | 64 | 33 s | 123 MiB/s | 0.40 s | 1.39 s |

On the disk a note took about twice what it took in memory at the same shape:
59 ms against 27 at 4 GiB, 1.34 s against 0.72 with slabs of 64 MiB. Raw:
[`vesseldisk.txt`](data/vesseldisk.txt), `BenchmarkScaleVesselDisk`.

### M3. A large thing

`BenchmarkScaleContent` brings one thing into a carrier on the disk in one
recording, with the `content.put` event that names it, from a source read as a
stream (`content.Bring`); then opens the folder afresh and reads the thing back
whole (`content.Fetch`). The carrier starts at 64 slabs of 1 MiB and grows by
itself 64 at a time, as `rokh init --growth auto:64:65536` makes one. Here the
content is the thing; the capacity is what the vessel grew to.

| content | bring | rate | growth steps | carrier's files after | open again | fetch | process at most |
|---|---|---|---|---|---|---|---|
| 1 MiB | 0.05 s | 20 MiB/s | 0 | 64.25 MiB | 9 ms | 0.02 s | 18 MiB |
| 16 MiB | 0.20 s | 78 MiB/s | 0 | 64.25 MiB | 41 ms | 0.30 s | 75 MiB |
| 256 MiB | 3.7 s | 70 MiB/s | 4 | 320.2 MiB | 0.36 s | 4.5 s | 1.0 GiB |
| 1 GiB | 29 s | 35 MiB/s | 16 | 1,088 MiB | 4.5 s | 17 s | 4.0 GiB |
| 2 GiB | 80 s | 26 MiB/s | 32 | 2,112 MiB | 8.1 s | 37 s | 8.0 GiB |

From 256 MiB up the process held four times the thing at its highest. Raw:
[`content.txt`](data/content.txt), `BenchmarkScaleContent`.

### M4. One history, in memory

`BenchmarkScaleChain` signs n notes on one branch, each naming the one before,
with the testimony a door leaves (a clock and a chance); judges them one at a
time as a door does; walks back from the newest to the first; and then loads
the whole history again from its bytes, verifying every event, as opening does.

| events | sign | judge | load | load, all | held | stored | newest to first | causal past | process at most |
|---|---|---|---|---|---|---|---|---|---|
| 1,000 | 96 µs | 65 µs | 86 µs | 0.09 s | 2,277 B | 382 B | 0.12 ms | 1.1 ms | 14 MiB |
| 10,000 | 97 µs | 70 µs | 69 µs | 0.69 s | 2,094 B | 383 B | 2.5 ms | 19 ms | 60 MiB |
| 100,000 | 100 µs | 70 µs | 74 µs | 7.4 s | 1,948 B | 384 B | 27 ms | 0.29 s | 0.52 GiB |
| 1,000,000 | 98 µs | 91 µs | 86 µs | 86 s | 2,298 B | 385 B | 0.49 s | 6.5 s | 5.4 GiB |

Sign, judge and load are per event; held is the ledger's memory per event;
stored is an event's bytes, head and body; "newest to first" follows the first
parent of each event back to the genesis; the causal past is every ancestor of
the newest, in the ledger's order. Raw: [`chain.txt`](data/chain.txt),
`BenchmarkScaleChain`.

### M5. A door on the disk, and reconcile

`BenchmarkScaleDoor` makes a carrier on the disk holding n notes, filled 2,000
notes a commit, and opens a door on it as a door opens: the carrier, the
owner's session, the references, the ledger loaded from them, the daemon. Then
it asks, through the door's own `Handle`, for ten writes, ten statuses and ten
times the last 50 lines of the log. The vessel column is capacity; the content,
not measured, is by the byte forms about 560 bytes a note with one reader
(contract 2.5, 3.1).

| events | open | held per event | one write | status | last 50 of the log | capacity | process at most |
|---|---|---|---|---|---|---|---|
| 1,000 | 0.14 s | 4,438 B | 25 ms | 0.65 ms | 0.64 ms | 64 MiB | 24 MiB |
| 10,000 | 1.5 s | 3,510 B | 36 ms | 4.5 ms | 3.6 ms | 64 MiB | 90 MiB |
| 100,000 | 14.1 s | 2,709 B | 366 ms | 211 ms | 147 ms | 64 MiB | 0.71 GiB |
| 300,000 | 43.8 s | 2,952 B | 1,175 ms | 752 ms | 523 ms | 192 MiB | 2.5 GiB |

Raw: [`door.txt`](data/door.txt), `BenchmarkScaleDoor`.

`BenchmarkScaleReconcile` copies such a carrier, records one note in each copy,
opens each copy as the command line opens it (`keyview.Open`, with the owner's
passphrase: the whole history read, and every event's envelope opened and
judged at its own point), and reconciles the two.

| events | open, as the command line does | reconcile | records moved | process at most |
|---|---|---|---|---|
| 1,000 | 0.21 s | 0.055 s | 2 | 32 MiB |
| 10,000 | 2.2 s | 0.18 s | 2 | 0.18 GiB |
| 100,000 | 20.6 s | 2.3 s | 2 | 1.4 GiB |

Raw: [`reconcile.txt`](data/reconcile.txt), `BenchmarkScaleReconcile`.

### M6. Keys named in one ledger

`BenchmarkScaleAuthority` grants to n keys in one ledger (a key of its own and
a scope of its own each), then takes every one of those grants back, newest
first; and, apart from that, adds n keyring generations of n key ids. Each
event is judged in its causal past.

| keys | grants: held | judge one | revocations: held | judge one | keyring: held | judge one | live keyring |
|---|---|---|---|---|---|---|---|
| 250 | 2.6 MiB | 83 µs | 2.6 MiB | 102 µs | 2.6 MiB | 96 µs | 0.9 ms |
| 500 | 9.2 MiB | 90 µs | 9.1 MiB | 81 µs | 9.3 MiB | 86 µs | 1.1 ms |
| 1,000 | 34 MiB | 110 µs | 34 MiB | 91 µs | 35 MiB | 92 µs | 2.7 ms |
| 2,000 | 136 MiB | 126 µs | 136 MiB | 164 µs | 137 MiB | 105 µs | 5.9 ms |
| 4,000 | 523 MiB | 177 µs | 522 MiB | 211 µs | 523 MiB | 124 µs | 12 ms |
| 8,000 | 2,027 MiB | 249 µs | 2,026 MiB | 1,061 µs | 2,029 MiB | 200 µs | 27 ms |

"held" is the memory the ledger took for those events; for revocations, what
taking every grant back added to what the grants held. "live keyring" is the
keyring as seen from the heads, which every sealing asks for. Taken back in the
order they were granted, the set of revocations after k of them equals the set
of the first k grants and the ledger's pool keeps it once; that first run,
which showed about 2 KB a key, is superseded. Raw:
[`authority.txt`](data/authority.txt) (grants, keyring),
[`revokes.txt`](data/revokes.txt) (revocations, newest first),
`BenchmarkScaleAuthority`.

### M7. Readers of an envelope, and the passphrase

`BenchmarkScaleEnvelope` seals a body of 300 bytes to r reader keys and opens
it by the last; `BenchmarkScalePassphrase` derives the owner's key at the
default cost and tries it on every slot cell.

| reader keys | envelope | overhead | seal | open |
|---|---|---|---|---|
| 1 | 432 B | 132 B | 132 µs | 54 µs |
| 2 | 500 B | 200 B | 176 µs | 110 µs |
| 4 | 636 B | 336 B | 258 µs | 58 µs |
| 8 | 908 B | 608 B | 558 µs | 62 µs |
| 16 | 1,452 B | 1,152 B | 990 µs | 54 µs |
| 32 | 2,540 B | 2,240 B | 1,785 µs | 53 µs |
| 33 | refused | | | |

The passphrase at the default 600,000 rounds: 128 ms; its key tried on all 32
slot cells: 132 µs. Raw: [`envelope.txt`](data/envelope.txt) (300 seals a
size), [`passphrase.txt`](data/passphrase.txt).

### M8. Keys at an open address

`BenchmarkScaleOpenAddress` makes one open grant at `commons` (contract 4.5),
and n keys, each its own, that nobody granted, added to the keyring or named in
an envelope, write one note there each: n keys, n events, n operations.

| keys | held per key | judge one | sign one | process at most |
|---|---|---|---|---|
| 1,000 | 2,454 B | 75 µs | 107 µs | 13 MiB |
| 10,000 | 2,244 B | 72 µs | 103 µs | 47 MiB |
| 100,000 | 2,111 B | 72 µs | 102 µs | 0.36 GiB |
| 1,000,000 | 2,447 B | 78 µs | 105 µs | 3.7 GiB |

Raw: [`openaddress.txt`](data/openaddress.txt),
`BenchmarkScaleOpenAddress`.

### M9. Writers and readers at once

`BenchmarkScaleWriters` puts doors on one carrier of 1,000 events, each its own
daemon on the folder, and writers at each door, each writing eight notes, all
at once, all with the key `clerk`. The times are each write's, from asking to
the answer.

| doors × writers | writes | writes a second | median | slowest 1% | slowest | refused |
|---|---|---|---|---|---|---|
| 1 × 1 | 8 | 37.6 | 26 ms | 29 ms | 30 ms | 0 |
| 1 × 4 | 32 | 38.5 | 97 ms | 130 ms | 148 ms | 0 |
| 1 × 16 | 128 | 32.4 | 483 ms | 550 ms | 574 ms | 0 |
| 1 × 64 | 512 | 31.2 | 1.96 s | 2.28 s | 2.30 s | 0 |
| 2 × 1 | 16 | 34.0 | 28 ms | 37 ms | 253 ms | 0 |
| 4 × 1 | 32 | 33.2 | 28 ms | 505 ms | 754 ms | 0 |
| 8 × 1 | 64 | 37.6 | 25 ms | 1.26 s | 1.49 s | 0 |
| 16 × 1 | 128 | 40.3 | 22 ms | 2.51 s | 2.97 s | 0 |
| 32 × 1 | 256 | 40.0 | 22 ms | 5.47 s | 6.21 s | 0 |
| 8 × 8 | 512 | 38.3 | 204 ms | 11.6 s | 11.7 s | 0 |
| 16 × 4 | 512 | 39.3 | 94 ms | 11.1 s | 12.1 s | 0 |
| 32 × 4 | 1,024 | 36.7 | 96 ms | 23.1 s | 25.5 s | 11 |

The eleven refusals were `turn_busy`. Every write answered `recorded` was in
the ledger read afresh afterwards (1,000 notes, the genesis and its grant, and
every recorded write). An earlier run of the first eight rows and of 8 × 8
agreed within 15 per cent. Raw: [`writers2.txt`](data/writers2.txt), and
[`writers.txt`](data/writers.txt) for the earlier run.

`BenchmarkScaleReaders` puts readers on a carrier of 10,000 events, each asking
in turn for the status and the last 50 lines of the log, forty times; first
with nobody writing, then with one writer recording through a door of its own
all the while.

| doors × readers | writer | reads a second | median | slowest 1% | slowest | writes a second |
|---|---|---|---|---|---|---|
| 1 × 1 | none | 382 | 1.7 ms | 12 ms | 16 ms | |
| 1 × 4 | none | 784 | 2.6 ms | 23 ms | 31 ms | |
| 1 × 16 | none | 1,064 | 6.5 ms | 79 ms | 120 ms | |
| 4 × 4 | none | 981 | 2.1 ms | 163 ms | 342 ms | |
| 1 × 4 | one | 32 | 144 ms | 423 ms | 436 ms | 24.6 |
| 4 × 4 | one | 107 | 72 ms | 417 ms | 486 ms | 17.6 |

No read failed. Raw: [`readers.txt`](data/readers.txt),
`BenchmarkScaleReaders`.

### M10. A bond's leaf

`BenchmarkScaleLeaf` makes the founding leaf of a bond among n founders
(anchors): its bytes, its name, reading it back, and its standing when all but
one have accepted it.

| founders | leaf | name it | read it | standing |
|---|---|---|---|---|
| 2 | 200 B | 0.13 ms | 0.03 ms | 0.001 ms |
| 100 | 6.8 KB | 0.38 ms | 0.43 ms | 0.013 ms |
| 10,000 | 670 KB | 31 ms | 44 ms | 2.4 ms |
| 1,000,000 | 67 MB | 2.7 s | 3.4 s | 255 ms |

Raw: [`leaf.txt`](data/leaf.txt), `BenchmarkScaleLeaf`.

### M11. Seeds

`BenchmarkScaleSeeds` (in `cmd/rokh`) makes seeds as a person does, with
`rokh seed`, 24 times: in a line, each seed given by the one before it, and in
a fan, every seed given by one root. It counts what each new folder holds from
its own `rokh log`, then writes a note in the last seed and reconciles it into
the root.

| seed | line: took | line: events | line: the ledger's own | fan: took | fan: events | fan: the ledger's own |
|---|---|---|---|---|---|---|
| 1 | 0.81 s | 7 | 6 | 0.86 s | 7 | 6 |
| 2 | 0.82 s | 11 | 10 | 0.69 s | 10 | 9 |
| 4 | 0.82 s | 19 | 18 | 0.79 s | 16 | 15 |
| 8 | 0.84 s | 35 | 34 | 0.86 s | 28 | 27 |
| 16 | 0.93 s | 67 | 66 | 0.87 s | 52 | 51 |
| 24 | 0.94 s | 99 | 98 | 0.94 s | 76 | 75 |

Every seed held one note, the first, written in the root before any seed. Each
folder was 4.25 MiB: the 4 MiB of slabs asked for, and the four head files.
Reconciling the last seed, with its one new note, into the root took 1.76 s for
the line and 1.87 s for the fan, and brought the note home: the root then held
100 events (line) and 77 (fan), all but two of them the ledger's own. Raw:
[`seeds.txt`](data/seeds.txt), `BenchmarkScaleSeeds`.

`BenchmarkScaleSeedLedger` makes the same four events a seed adds (the keyring
add of its key, a grant to its signer, the give, the take) for up to 2,000
seeds, judged by one ledger, as a line and as a fan (where the root holds every
take once it has reconciled every seed), and loads the whole lineage again from
its bytes.

| seeds | shape | events | depth | held | per seed | judge one | the last take | load all |
|---|---|---|---|---|---|---|---|---|
| 250 | line | 1,001 | 1,000 | 23 MiB | 97 KB | 149 µs | 0.48 ms | 0.12 s |
| 500 | line | 2,001 | 2,000 | 88 MiB | 185 KB | 199 µs | 0.27 ms | 0.37 s |
| 1,000 | line | 4,001 | 4,000 | 336 MiB | 353 KB | 375 µs | 0.28 ms | 1.37 s |
| 2,000 | line | 8,001 | 8,000 | 1,295 MiB | 679 KB | 623 µs | 0.88 ms | 4.31 s |
| 250 | fan | 1,001 | 751 | 17 MiB | 72 KB | 141 µs | 0.54 ms | 0.13 s |
| 500 | fan | 2,001 | 1,501 | 62 MiB | 131 KB | 190 µs | 0.32 ms | 0.35 s |
| 1,000 | fan | 4,001 | 3,001 | 242 MiB | 253 KB | 289 µs | 2.9 ms | 1.11 s |
| 2,000 | fan | 8,001 | 6,001 | 926 MiB | 486 KB | 536 µs | 5.9 ms | 4.03 s |

Depth is the number of steps from the last take back to the first event by the
parents. Raw: [`seedledger.txt`](data/seedledger.txt),
`BenchmarkScaleSeedLedger`.

### M12. One processor profile

The door at 100,000 events was measured again under the processor profile (the
write in 367 ms, the status in 213 ms, against 366 and 211 in §M5). Of the
7.0 s its thirty answers took, 6.5 s were inside `(*carrier.Carrier).Refs`:
4.6 s opening branch pointers and 1.5 s rebuilding the vessel's index. Across
both, slabs read again cost 2.5 s of SHA-256 and 1.2 s of AES-GCM. Raw:
[`door-profile.txt`](data/door-profile.txt).

## C. Read in the code

What the code of commit 4111458 does, read where it says it, to explain the
numbers of part M. Nothing in this part was measured on its own, except where a
section says so.

### C1. What a commit reads and writes

- A commit copies the whole inventory, frees every old segment and writes all
  ⌈N/4096⌉ segments again, each to a new slab, and looks at every entry for a
  free slab (`vessel/tx.go`, `(*Tx).Commit`). The contract writes "the changed
  inventory segments" (2.6, step 3). Since a segment moved changes the entries
  of the segments that cover its old place and its new one, every segment
  changes because every segment is written.
- The last pack is read and rewritten whole with the new records behind what it
  held, when they fit (`(*Tx).Commit`).
- The commit's first step takes the highest valid generation, and its fourth
  sees that it still is (contract 2.6). Both are answered by verifying the
  whole generation, every segment by its digest and its tag
  (`vessel/vessel.go`, `(*Vessel).refresh`, `(*Vessel).choose`,
  `(*Vessel).verify`), and the first also lists every slab file of the vessel
  (`(*Vessel).survey`).

So one note writes (1 + ⌈N/4096⌉)·S + 64 KiB and reads (2·⌈N/4096⌉ + 3)·S +
576 KiB, both to the byte at every size of §M1, and spends processor time on
every slab of the capacity.

### C2. Why a door's answers grow with the history

1. Every commit drops the vessel's index of records (`vessel/tx.go`,
   `(*Tx).Commit`, `v.index = nil`), and the next read rebuilds it by reading,
   hashing and opening every pack (`vessel/vessel.go`, `(*Vessel).indexOnce`).
2. The branch references are found by opening every branch pointer ever
   recorded (`carrier/carrier.go`, `(*Carrier).Refs`): each commit records one,
   and a superseded one stays in its pack. The pointers lie in every pack, and
   the vessel keeps at most eight opened slabs (`vessel/vessel.go`,
   `(*Vessel).slab`), so each asking reads, hashes and opens nearly the whole
   vessel again. A door asks before every answer (`daemon/daemon.go`,
   `(*Server).behind`) and again for a status or a write. §M12 measured this as
   6.5 s of 7.0 s.
3. The ledger's order is dropped at every accepted event (`ledger/ledger.go`,
   `(*Ledger).settle`) and made again whole when next asked for
   (`(*Ledger).ordered`); `log` without a cursor takes the whole order and
   walks it to keep its last lines (`daemon/daemon.go`, `(*Server).log`).

### C3. Why authority takes memory as the square

Every grant, revocation, keyring change and seed event is judged in its causal
past, and the ledger keeps, for that event, the set of every grant (or
revocation, or keyring and seed event) in that past: a sorted list copied whole
with one more member (`ledger/set.go`, `idset.add`, `union`), kept in a pool
that shares equal lists under a key as long as the list (`interner.canon`). A
take also reads the whole system set to find its signer (`ledger/ledger.go`,
`(*Ledger).evaluate`, rule S1).

### C4. Why a large thing takes four times its size

The recording keeps every sealed chunk until its commit (`carrier/carrier.go`,
`(*Recording).Content`, through `(*Tx).Put`); the commit packs them all into
slab bodies at once (`vessel/tx.go`, `(*Tx).Commit`); a growing vessel grows
one step and tries the whole commit again, packing everything again, until it
fits (`(*Tx).CommitGrowing`); reading back opens every chunk twice, once to
hash the whole and once to hand it over (`carrier/carrier.go`,
`(*Carrier).ContentToSized`), and gathers the whole before anything is written
(`content/vessel.go`, `Fetch`, as E8 requires).

### C5. How the writing turn is taken

A writer takes the kernel's lock on `rokh/head0.rkh`, trying again every 10 ms
(`turn/turn.go`, `acquire`), with a patience of 15 s at a door
(`daemon/commit.go`, `turnPatience`), after waiting for the door's own lock,
which has no patience. Nothing orders those who wait: whoever tries first once
the lock is free takes it.

### C6. The numbers version 1 fixes

| what | value | where |
|---|---|---|
| readers named in one envelope | 1 to 32 | contract 3.1; `key/key.go`, `MaxReaders`, `SealReaders` |
| readers of an address | owner generations and every live key whose reads cover it | contract 4.4; `(key.Ring).Readers` |
| slot cells in a vessel | 32: the owner and 31 keys | contract 1, 4.6; `vessel/names.go`, `SlotCells` |
| slabs, slab size | 16 to 2^22; 2^18 to 2^26 bytes, chosen to fit FAT32 | contract 1; `vessel/names.go` |
| a vessel's files | always N + 4 files of N·S + 4·64 KiB, whatever it holds | contract 1, U1 |
| generations a slab stays untouched | R = 3 | contract 1, 2.6; `vessel/names.go`, `Retention` |
| salt and passphrase cost | one per rokh, inherited by every seed | contract 2.3 |
| payload of an event | 4 KiB in the base profile | T3.4; `event/event.go`, `MaxPayload` |
| parents of an event | 16 | `event/event.go`, `MaxParents` |
| a booth's listener | a Unix socket, or TCP on the loopback interface only | `transport/transport.go`, `Listen` |

An address read by more than 32 keys, the owner's generations among them,
cannot be written: the session refuses to seal (`key/key.go`, `SealAt`). The
seed's own key reads nothing for that reason (`cmd/rokh/v1_seedplan.go`).

### C7. The fault found and repaired

The walk that loads a history went into the parents of each event by recursion,
about 1.9 KiB of stack for each generation it descended. A probe kept outside
rokh loaded chains of 500,000 and 650,000 events and ended the process with
a stack overflow at 1,000,000, whose walk went 645,365 generations deep.
`(*Ledger).ExtendWith` in `ledger/load.go` now keeps its own stack (commit
4111458), and `ledger/deep_test.go` loads 3,000 events under a stack of
512 KiB, which the old walk overflowed at a few hundred. §M4 shows a million
events loading.

### C8. What an opening does

`carrier.Open` reads the head files and verifies every segment and every slab
the last three generations wrote (§C1). `ledger.Load` then reads every event
the branch references reach and judges each one: its hash, its signature and
its authority in its causal past; the whole ledger stays in memory
(`ledger/load.go`). The command line opens through `keyview.Open`, which also
opens every event's envelope at its own point. Nothing of a verification is
kept between two openings.

## X. Extrapolated

What the measurements of part M imply beyond the sizes that were run, by the
mechanisms of part C. Each model says what it rests on and how far the runs
reach. None of it was run.

### X1. Vessel capacity, to the format's end and past it

The model, fit to every size of §M1 (16 ≤ N ≤ 4,194,304; S of 256 KiB, 1 MiB
and 64 MiB; thirteen notes of content): one note writes (1 + ⌈N/4096⌉)·S +
64 KiB and reads (2·⌈N/4096⌉ + 3)·S + 576 KiB, and takes about 5 µs of the
processor for every slab of the capacity (4.9 µs at a million, 5.5 at four
million) plus about 1.6 ms for every MiB sealed, opened and hashed (the rows of
64 MiB slabs), before any disk's time, which about doubled it in §M2. Once N
passes 4,096, ⌈N/4096⌉·S is the capacity divided by 4,096, whatever the slab:
every commit writes at least 1/4096 of the capacity.

- **Where a vessel stops being usable for notes.** With the default slab of
  1 MiB a note takes about 6.5 µs for every slab: a second at about 150 GiB of
  capacity and ten seconds at about 1.5 TiB, in memory. With slabs of 64 MiB no
  note takes less than 0.7 s, and one takes about 5.5 s at 4 TiB.
- **The format's end, 256 TiB** (S = 64 MiB, N = 4,194,304): one note writes
  1,025 slabs, 64 GiB, and reads 2,051, 128 GiB; about 23 s of the processor
  for the inventory (the 1 TiB row has the same N) and some five minutes to
  seal, open and hash 192 GiB. Making such a vessel would take some four weeks
  at the rates of §M2.
- **A PiB of capacity** is four times beyond the version 1 format under any
  parameters. A vessel of the same design at a PiB would write at least 256 GiB
  and read at least 512 GiB for every note, and spend some 90 s on an inventory
  of 16,777,216 slabs of 64 MiB: many minutes a note on any disk.
- **Content is another matter.** These costs follow capacity. What content a
  vessel can hold and bring in one recording is bounded first by memory, four
  times the thing (§M3): about 3.5 GiB on this host, and for a person's PiB of
  content, a thing at a time, far below the capacity of any vessel.

### X2. History

- **On this host**, 15.7 GiB: a bare ledger of some three million events (the
  process reached 5.4 GiB at its highest for one million; once loaded a ledger
  holds 2.3 KB an event, which alone would allow seven), and a door of some two
  million (2.5 GiB at its highest for 300,000).
- **A write at a door** costs about 25 ms and 3.9 µs more for every event
  already held (§M5): a second at about 250,000 events, ten at about 2.5
  million.
- **Opening**: 86 µs an event in a bare ledger, 146 at a door, 210 for each
  command of the command line; at a million events two and a half minutes for a
  door and three and a half for each command; at five million, on a machine
  with the memory for it, twelve and seventeen.
- **A billion events**: about a day of one processor to open, at the bare
  ledger's rate, and 2 to 3 TB of memory, which no machine of this kind has.
- **From the newest event to the first.** In a ledger held in memory, 0.49 µs a
  step by the parents (§M4): a million steps in half a second, a billion in
  about eight minutes. From the carrier nothing is held until it is verified,
  so the first event is reached only after opening, at the rates above. The
  format counts no generations: an event names up to sixteen parents, and
  nothing else bounds the length of a history.

### X3. Keys, and people

- **Keys granted** take about 33·n² bytes (§M6, fit from 250 to 8,000): 3.3 GB
  for 10,000, 13 GB for 20,000 (the end of this host), 330 GB for 100,000,
  33 TB for a million. Judging one more grant copies the list: about 80 µs and
  20 ns more for every key already granted. Revocations and keyring changes go
  the same way.
- **Keys at an open address** cost the same each, about 2.1 to 2.5 KB and 72 to
  78 µs from a thousand to a million (§M8): 210 GB for 10^8 keys, 17 TB for
  8·10^9.
- **One note each, through one carrier**, at today's cost (§M5): 25 ms·n +
  1.95 µs·n². At the best rate measured, if a commit cost the same at any
  length: n / 35 seconds.

The table counts keys and assumes one key for each person. A person who holds
several keys meets the same limits at fewer people.

| people, one key each | keys granted: memory | reader keys of one address | slot cells | one note each, one carrier | keys at an open address: memory | founders of a bond: the leaf |
|---|---|---|---|---|---|---|
| a family, 5 | 0.8 KB | 404 B on each envelope, 0.35 ms to seal | 5 of 32 | 0.13 s | 11 KB | 0.4 KB |
| a household, 30 | 30 KB | 2.1 KB, 1.7 ms | 30 of 32 | 0.8 s | 66 KB | 2 KB |
| a village, 1,000 | 34 MiB (M) | cannot be sealed | all 32 used; 968 cannot open | 27 s | 2.4 MB (M) | 67 KB |
| a town, 10^4 | 3.3 GB | | | 7.4 min | 22 MB (M) | 670 KB (M) |
| a city, 10^6 | 33 TB | | | 23 days (8 h at 35 a second) | 2.4 GB (M) | 67 MB (M) |
| a country, 10^8 | 330 PB | | | 600 years (33 days) | 210 GB | 6.7 GB |
| the Earth, 8·10^9 | 2·10^21 B | | | 4 million years (7 years) | 17 TB | 536 GB |

(M) marks a measured cell; the rest follow from the fits. A founder is a
ledger's anchor: one for each person who keeps a Rokh.

### X4. Seeds

- **What a seed holds of those before it.** In a line the n-th seed holds 3 +
  4n events (§M11): the genesis, the owner's keyring add and the first note,
  and four for itself and every seed before it; in a fan the n-th holds 4 + 3n.
  The notes do not grow; the ledger's own events do.
- **Memory** grows as the square of the seeds: about 340·n² bytes in a line and
  240·n² in a fan (§M11). On this host a lineage of some 7,000 seeds in a line,
  or 8,000 in a fan, can no longer be opened, and therefore no longer
  reconciled, since reconcile opens both sides whole (§M5).
- **The billionth seed of a line** would hold four billion and three events:
  everything the root held when it gave its first seed, the first seed whole as
  it was when it gave the second, and for every seed the keyring add of its
  key, its grant, its give and its take. Nothing of the first is lost, since a
  seed carries the whole causal past of its give and nothing recorded is
  erased; what the first seed recorded after it gave the second is not there
  until a reconcile brings it. It would take some 1.5 TB of events, some 9 TB
  of memory for the ledger and, by the square, some 3·10^20 bytes of authority
  sets: it cannot be opened, and its take cannot be judged, since a take reads
  a system set of three billion members.
- **Cause and effect.** A take is refused unless its give is in its causal past
  (`(*Ledger).evaluate`), and in a line each give is made on the seed before
  it, after that seed's take. So the causes run in one line from the first
  event to the last: genesis, the first give, the first take, the second
  keyring add, grant and give, the second take, and so on, four billion steps,
  each an event naming the one before it. In a fan every seed is one step from
  the root, and the root that reconciles them all carries three events a seed.

### X5. How each thing grows

| what | grows with | how | measured over |
|---|---|---|---|
| what one note writes | the capacity | linearly, at least 1/4096 of it | 4 MiB to 1 TiB |
| what one note costs in time | the slabs of the capacity | linearly, about 5 µs a slab | 16 to 4,194,304 slabs |
| opening a vessel | the slabs | linearly | 16 to 4,194,304 slabs |
| a large thing: memory, time | its size | four times it; time more than its size | 1 MiB to 2 GiB |
| judging one event | the history | constant, 65 to 90 µs | 1,000 to 10^6 events |
| opening a ledger; its memory | the history | linearly | 1,000 to 10^6 events |
| a write, a status, a log at a door | the history | linearly | 1,000 to 300,000 events |
| memory of grants, revocations, keyring | the keys named | as the square | 250 to 8,000 keys |
| judging one more of them | the keys named | linearly | 250 to 8,000 keys |
| an envelope, and sealing it | its reader keys | linearly, to 32 | 1 to 33 reader keys |
| opening an envelope | its reader keys | constant | 1 to 32 reader keys |
| a key at an open address | the keys there | constant | 1,000 to 10^6 keys |
| commits a second | writers and doors | constant, about 35 | 1 to 128 writers, 1 to 32 doors |
| a writer's wait | the writers ahead | linearly, and unfairly | 1 to 128 writers |
| reads a second | readers | with the processors, not the readers; a writer cuts them | 1 to 16 readers |
| a bond's leaf | its founders | linearly, 67 B each | 2 to 10^6 founders |
| what a seed holds of those before it | the seeds before it | linearly | 1 to 2,000 seeds |
| the memory of a lineage of seeds | the seeds | as the square | 250 to 2,000 seeds |
| reconcile | both histories | linearly, however little differs | 1,000 to 100,000 events |

Beyond "measured over", every line of this table is the model.

## P. Proposed

What could change, for the owner to decide. Nothing here is built, and nothing
here is a ruling: the first kind keeps every byte form of version 1; the second
is a new generation with a name of its own, since a byte form is never edited
(T3.7, AGENTS.md); the third is a reading of the texts, which only the owner
can confirm.

### P1. Within version 1

1. **Write only the changed inventory segments**, as contract 2.6 already says,
   and place a commit's new segments among the ranges it changes anyway; keep a
   list of free slabs instead of looking at every entry; see at the fourth step
   that no other commit came by the head files' tokens, and verify a generation
   whole only when they moved. A note would then cost its pack, a few segments
   and a head at any capacity (§C1).
2. **Keep the vessel's index across commits**, adding what a commit wrote
   instead of dropping it; and find a branch by the last pointer that names it,
   whose header already carries the branch's name (contract 2.5), so that one
   pointer is opened for each branch, not every pointer ever recorded (§C2).
3. **Keep the ledger's order and extend it**: an event accepted after every
   head it names goes at the end; only one that lands among older events makes
   the order again. The last lines of a log are read from the end (§C2).
4. **Authority sets that share their structure**: a persistent ordered set (a
   balanced tree, or a hash trie, copied along one path only) in place of a
   sorted slice copied whole. Every event still carries the whole set of its
   causal past and every verdict is the same, since the law is about the causal
   past, not about how it is held (T6.2); one more grant copies one path of
   some twenty nodes, and a million keys granted hold about a gigabyte, not
   33 TB (§C3).
5. **A fair turn, and one commit for those already waiting**: writers queued at
   a door go into the commit the first of them makes, since they have all
   asked; the turn is taken in the order it was asked for, not by whoever tries
   first (`turn` is a host adapter, and the lock is its business). The door's
   rate becomes the disk's, not one note a commit (§C5).
6. **Large things in bounded memory**: chunks sealed and packed as they are
   read, the recording holding where they went rather than their bytes; a
   growing vessel grows at once by what a recording needs (§C4).

None of these changes what a person can do or see. Together they remove the
limits 2 to 6 of the summary's table, all of them this implementation's.

### P2. New generations

1. **A vessel beyond 256 TiB.** The slab count must grow past 2^22, and the
   inventory must stop being read whole: a tree of segments with its root in
   the head, each segment named by its hash as the segments are now, and packs
   appended, never rewritten. The contract itself names the second half as the
   way to close its gap G1 ("committed bytes that are never overwritten and a
   commit log that only grows"). A commit then writes its records, one path of
   the tree and a head; opening reads the head and what it walks.
2. **Opening from a checkpoint.** A record the owner's key signs, saying that
   the history up to certain heads was verified and what the authority sets
   were at them. An opening verifies from the last checkpoint it trusts; the
   whole walk stays possible and is what a checkpoint is checked against, so
   one person offline can still verify everything (N2.6). This is what would
   let a billion events be opened by a machine that cannot hold them. Whether
   an opening may rest on a checkpoint at all is the owner's to rule.
3. **Readers by a key for each address, not each envelope.** An address sealed
   to one key for a period, the key given to its readers once, in the keyring;
   a change of readers starts a new period (and a tree of keys makes a change
   cost a logarithm of the readers). An envelope is then the same size for two
   readers and for two million; taking a reader back closes the next period,
   not the past, which is what revocation already is (T6.3).
4. **A bond's leaf as a tree of its founders.** The name commits to the root;
   each founder accepts with the path from their anchor to the root, about 33
   hashes among eight billion, instead of holding the whole leaf. T11.6 asks
   that the leaf say the founders' anchors; whether a root that commits to them
   says them is the owner's to rule.
5. **Seeds that carry what they rest on.** A seed holds the events its own
   authority rests on and a checkpoint for the rest of its source's history,
   instead of every system event of its source (contract 4.7, S2).

### P3. Many people

A reading of the texts, for the owner to confirm: every ledger is the history
of one person, and being common is the work of a higher layer (N2.7); the claim
is about the ledger of a person, not about every institution (N2.8); shared,
non-exclusive work is closed by each in their own ledger (N2.10); a lasting
knot among many is a bond, in which each keeps their own ledger and the ledgers
never become one (T11.5, T11.6). Read so, billions of people are billions of
Rokhs, and what must grow to them is the bond (P2.4) and the carrying of events
between ledgers, not one ledger. One Rokh, with P1 and P2 made, stays the
ledger of one person and of the few who read and write with them, at any size
of content and any length of history.

## S. Seeds by role: a direction, not a capability

> The owner's direction: one Rokh lives in several seeds, each with a role. A
> mother seed is kept offline, a working seed lives on a solid-state disk, and
> a frontier seed is reachable by others. This section asks what stays the same
> in every seed of one Rokh and what may follow the role, the environment and
> the covenants. It says what version 1 does, and writes down where the
> direction meets a reference text (S5). It rules on nothing and changes no
> reference text. "Mother", "working" and "frontier" are the owner's words, not
> the documents'.

### S1. The same in every seed (version 1, by contract)

- **The anchor**: every seed has the genesis of its Rokh (contract 4.7, S2;
  docs/03 §8), and a ledger accepts no event of another anchor (T8.4).
- **The events and their verdicts**: the same bytes are judged in their own
  causal past (T6.2) by the same rules (contract C4, C8), so the same past
  gives the same verdict in every seed, and a verdict is final (T6.4).
- **The lineage**: every seed holds every system event and the signed head of
  every ancestor (S2, S3).
- **The keyring and the readers**: the readers of an address at a point are a
  fold of that point's causal past (contract 4.4), the same in every seed. The
  readers of an event are fixed by the ledger, not by the seed that holds it.
- **The root**: only the root gives a seed (S1).
- **The passphrase's salt and cost**: one per Rokh, inherited by every seed
  (contract 2.3).
- **The byte forms**, the four heads and R = 3 of every vessel (contract 1, 2),
  the payload's 4 KiB (T3.4), and that nothing recorded is erased (T3.7, T6.3).

### S2. What may differ from seed to seed (version 1)

- **The vessel**: its own id and VK (contract 4.7, S2), and its own key for
  pointers; its slab size, slab count and growth (contract 1).
- **Its scopes**: whole, or a slice that holds its scopes whole and every other
  ancestor as a signed head only (S3).
- **Its slot cells**: which keys open this vessel with a passphrase (contract
  4.6).
- **Its custody**: the owner's cell holds the root's signing seed, or zeros
  ("cold custody", contract 4.6); `rokh init --cold FILE` keeps the root key
  out of the vessel, in a file. Making a cold seed from a warm source was not
  examined.
- **Its branches** until a reconcile; **its medium and profile**: a folder, or
  the chest (T8.7, docs/03); **its doors**: a booth listens on a Unix socket or
  on the loopback interface only (`transport.Listen`).

### S3. The three roles

| | mother, offline | working, on a solid-state disk | frontier, reachable |
|---|---|---|---|
| what it is for (the direction) | the whole Rokh, kept apart | daily writing | what others may consult |
| scopes, as version 1 allows | whole | whole or a slice | a slice: what may be shown |
| the root key, as the direction places it | in its cell, or in a key file beside it | cold | cold |
| capacity | large, fixed | small slabs: each note writes at least two slabs and a head (§M1), so the slab sets the disk's wear | small |
| reached | not at all: a Rokh is whole without a network (T1.4) | by its owner's doors on its host | in version 1, by nothing outside the host; reach would come from a courier or a host service outside Rokh (T4.1, docs/07 §4) |
| what it risks | old cells in an old copy (U6); a host that sees it while open (U8, T12.6) | a commit cut by power on a disk that does not honour a flush (U4) | an always-on machine holding the ledger and a key (N5.2), the case the texts leave open |

### S4. Selective encryption, retention, backup, synchronization

- **Selective encryption.** Version 1 seals each event to the readers of its
  address at its point, the same in every seed (contract 4.4); each vessel's
  own shared key seals only its pointers and the home's own records (E7); a
  slice leaves out bodies, it does not seal them otherwise (S3). Sealing for a
  named recipient is ruled and not built (T13.1, N9.5); showing one field alone
  is wanted and its byte form is open (T7.5). What a frontier holds is decided
  when it is made, as disclosure is decided when the bundle is closed, never
  when it is sent (T7.4).
- **Retention.** Nothing recorded is erased (T3.7, T6.3, STATE.md); a vessel
  keeps every slab of its last three generations whole (contract 2.6). What a
  seed keeps is decided by its scopes when it is made (S3). A seed that later
  forgets is not in version 1, and every seed keeps every system event and
  every ancestor's head (S2), so its lineage grows with every seed (§X4).
- **Backup.** Keeping the key and multiplying the copies are the owner's work,
  and a lost key is not recovered (T8.6, N6.6, U11). A copy of a folder opens
  for certain only if no more than R = 3 commits start while it is made (G1,
  U3); an older copy keeps the cells it had (U6). A seed is a copy with a VK of
  its own. What the host itself copies is outside Rokh (T8).
- **Synchronization.** Two seeds meet by reconcile: the union both ways, judged
  by each side, commutative, recording no event unless asked (T5.4, contract
  4.8, C6). It is not a synced folder: two machines on one folder are not kept
  apart (U5, G2). Nothing in the core runs by itself (T4); a machine may carry
  and unite events as long as it makes none (T4.1). Contract R6 asks that seeds
  that meet reconcile without being asked; version 1 does not build it, and the
  comment above `cmdReconcile` (`cmd/rokh/v1_vessel.go`) says so by the owner's
  word of 2026-09-29, which no reference text holds (STATE.md). A reconcile
  reads both histories whole (§M5).
- **Covenants.** What may be shown to a peer is decided by covenants: ordinary
  events at the address `peer` (`peer.share`, `peer.unshare`), read outside the
  core when a bundle is closed (docs/07, `covenant`). Being events, they are
  the same in every seed that holds their scope; a covenant permits and never
  starts a transfer. An application's covenant with Rokh (T11.10) is ruled, and
  its runtime is not built (N9.5). So in version 1 the role of a seed changes
  which covenants it carries and serves, not what they say.

### S5. Where the direction meets the reference texts

For the owner's decision; each row is a meeting, not a verdict.

| | the direction asks | the texts say | version 1 does | to decide |
|---|---|---|---|---|
| 1 | readers that differ by seed | readers are a fold of the causal past (contract 4.4) | readers by address; seeds differ by scope | whether a role may seal otherwise, and in what generation |
| 2 | a passphrase cost that differs by role | salt and cost one per Rokh (contract 2.3); another profile is allowed while the carrier's properties hold (T8.7) | one cost | a profile by role, as a new generation |
| 3 | a seed others can reach | no code path of Rokh opens a network socket (N, the requirement under Axiom 5), and the door for programs is a Unix socket (N, the base profile of §4); machines may carry events and make none (T4.1) | Unix sockets and loopback TCP only | whether reach lives inside Rokh or beside it |
| 4 | an always-on frontier | an always-on machine holding the ledger and a key is an open case, answered by rotation and by keeping the signing key apart (N5.2); rotating the root key is open (T13.2) | cold custody exists (contract 4.6) | the open rulings themselves |
| 5 | a frontier that is a mirror | a mirror between two owners: ruled, read-only, never merged (T13.4); open (N9.5); same-owner mirroring both settled as a berth and open (docs/07 §11) | no mirror is built | which text holds, and whether a frontier is a seed or a mirror |
| 6 | a seed that keeps less over time | nothing recorded is erased (T3.7, T6.3) | a slice, made once | whether forgetting a body differs from erasing |
| 7 | synchronization that runs itself | no event by itself (T4), the core never acts on its own (AGENTS.md); R6 | reconcile when asked | where a synchronizing service may live |
| 8 | three named roles | seed, berth, mirror and courier are the documents' words (AGENTS.md, docs/07), and no word is coined where one serves (AGENTS.md) | seeds without roles | whether roles become words of the documents |

None of these is shown here to be a limit of Rokh. Rows 1 and 2 meet the
version 1 contract, which a new generation may change; rows 3 to 7 meet the
texts themselves, whose rulings are the owner's, and in row 5 the texts differ
from each other; row 8 meets the rule of words of rokh's AGENTS.md.

## F. Not run, and where there is no evidence

- **Capacity.** No vessel of more than 16 GiB on a disk, none of more than
  1 TiB anywhere, and none with more than thirteen notes of content in the
  capacity sweeps; no PiB, which version 1 cannot express. No vessel on a
  solid-state disk known as such, on removable media, on FAT32 or exFAT, in a
  synced folder, or in the chest.
- **Content.** Nothing larger than 2 GiB; no large content in a vessel of large
  capacity; no fetch through a booth or the home.
- **History.** No bare ledger of more than a million events, no door of more
  than 300,000, no reconcile of more than 100,000; no history with many
  branches or merges (every measured history but the seeds' is one branch).
- **Keys and people.** No person was modelled, only keys (§0). No booth
  session, socket, courier, gate or network took part. No more than 8,000 keys
  granted or revoked, 32 reader keys in an envelope, or one key in the writers'
  runs. The 32 slot cells were read in the code, not filled. No key was rotated
  and no passphrase changed.
- **Seeds.** No more than 24 seeds through the command line; no slice seeds; no
  cold seeds; no seed between two booths, which version 1 does not serve; no
  reconcile of the 2,000-seed lineages; contract R6 read in the code only
  (§S4).
- **The direction of §S.** Nothing of it was run.
- **Repetition and spread.** Each size ran once; only the writers and the door
  at 100,000 events ran twice. No spread, no confidence interval.
- **Causes.** One processor profile, of the door at 100,000 events. The cost of
  a commit per slab (§C1) is read in the code, not profiled.
- **Other machines.** Nothing on macOS, Windows or Android; no measurement of
  energy, or of a disk's wear.
- **Tests.** Which conditional skips fired in the suites was not recorded,
  since the runs were not verbose; see §T.

## T. The state of the tests

| what | where | as whom | result | evidence |
|---|---|---|---|---|
| `gofmt -l`, `go vet ./...`, both modules | tree of 91f6101 | superuser | clean | the session's reading of the output; no log |
| `go test -timeout 45m ./...`, the core | tree of 91f6101 | superuser | 35 packages ok, 5 without tests, `rokh/cmd/rokh` FAIL: `TestAMarkerNamingASeedGivenAndTakenIsRefused`, `TestAResumedSeedGivesNoCellToAKeyTakenBackSince` | [`suite-rokh.txt`](data/suite-rokh.txt) |
| `go test ./...`, the home | tree of 91f6101 | superuser | 8 packages ok, 2 without tests | [`suite-home.txt`](data/suite-home.txt) |
| `go test ./cmd/rokh` | tree of 91f6101 | an unprivileged account, uid 65534 | ok, 518.347 s | the session's reading of the output; no log |
| the two tests above | release commit ba7e695 | superuser | both FAIL, as on 91f6101 | the session's reading of the output; no log |
| the two tests above | release commit ba7e695 | uid 65534 | both pass | the session's reading of the output; no log |

- The two tests fail only as the superuser: each cuts a seed by making the
  source's slab files read-only, and the superuser writes them anyway, so the
  seed is not cut. STATE.md records them. They were neither repaired nor
  skipped.
- The three tests STATE.md names as failing are skipped as before; no skip was
  added. Whether the suites' conditional skips (a short socket folder, a
  `python3`, the large-stream tests) fired was not recorded.
- The conformance test ran within the core's suite and left
  `conformance/STATE.md` as it was.
- The commits after 91f6101 change documents and raw data only, and no test was
  run on them.

## Appendix: to reproduce

**The measurements.** In `rokh/`, with Go 1.26.4, one at a time:

```sh
go test ./bench    -run '^$' -bench Scale      -benchtime 1x -timeout 0 -v
go test ./cmd/rokh -run '^$' -bench ScaleSeeds -benchtime 1x -timeout 0 -v
```

or one benchmark at a time, with the commands listed beside each raw output in
[`data/README.md`](data/README.md); `BenchmarkScaleEnvelope` wants
`-benchtime 300x`. `go test ./...` runs none of them; of their files it runs
only the test of the sparse medium.

- **Time.** On this host the first round took 32 minutes and the second 13.
- **Memory.** The sparse vessel of 1 TiB reached 7.1 GiB; the chain of a
  million events 5.4 GiB; a thing of 2 GiB 8.0 GiB; grants and revocations of
  8,000 keys some 4 GiB of live heap together.
- **Disk.** `BenchmarkScaleVesselDisk` needs 16 GiB free where it makes its
  vessels, a temporary folder or `ROKH_SCALE_DIR`; `BenchmarkScaleContent`
  needs 2.2 GiB.
- **The profile.**

  ```sh
  go test ./bench -run '^$' -bench 'BenchmarkScaleDoor/events=100000$' \
      -benchtime 1x -timeout 0 -cpuprofile cpu.out
  go tool pprof -top -cum -focus='daemon.\(\*Server\).Handle' bench.test cpu.out
  ```

- **Reading.** Each output line of a benchmark carries its metrics by name
  (`commit-ms`, `write-MiB/commit`, `heap-B/event`); the tables of part M copy
  them, rounded. Numbers of another host will differ; how they grow should not.

**The tests.** In `rokh/` and in `rokh-home/`:
`go vet ./... && go test -timeout 45m ./...`. Run as the superuser, the two
tests of §T fail. Run by an unprivileged account, they pass: that account needs
read access to a copy of rokh, the Go toolchain of `go.work`, and a
writable `HOME`, `GOCACHE` and `TMPDIR`.

**The commits.** 4111458 repairs `ledger.Load`; a72bc74, b2c436a, f459779,
6e65793, 617ce7b and 91f6101 add and correct the benchmarks; 086b386 and the
commits after it write this report, its raw data and STATE.md.
