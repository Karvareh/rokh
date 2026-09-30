# Scale: how far one Rokh goes

> Measured on one machine: a vessel from 4 MiB to 1 TiB, and one thing in it of
> up to 2 GiB; a history from a thousand events to a million; from one person
> to eight thousand named in one ledger, and a million at an open address; from
> one seed to two thousand. Every number below came from a benchmark named
> beside it. What could not be run is called a model, and says what it rests
> on.

## 0. How it was measured

The measurements are benchmarks, and run only when asked:

```sh
cd rokh
go test ./bench    -run '^$' -bench Scale      -benchtime 1x -timeout 0 -v
go test ./cmd/rokh -run '^$' -bench ScaleSeeds -benchtime 1x -timeout 0 -v
```

`-benchtime 1x` makes each size once and measures it over its own operations
(`BenchmarkScaleEnvelope` wants `-benchtime 300x`). Each benchmark reports with
`b.ReportMetric`, one line per size, so the numbers can be read side by side.
`go test ./...` runs none of them; of their files it runs only the test of the
medium below.

They were run one at a time on one machine: four virtual processors at 2.1 GHz
with the processor's own AES and SHA instructions, 15.7 GiB of memory, one
virtual disk, Linux, Go 1.26.4. The numbers belong to that machine. What
carries over is how they grow.

Two things stand in for what one machine cannot hold:

- **The sparse medium** (`bench/scale_test.go`, `sparseMedium`, `quickRandom`).
  A vessel fills every free slab, and every head file it has not written, with
  randomness that nothing reads back. In the measurement that fill is left
  zero, and the medium keeps an all-zero file as its length alone. Everything
  the vessel writes or reads for itself (packs, inventory segments, heads) is
  still sealed, written, read, hashed and opened, and every call and byte is
  counted. So a vessel of 4,194,304 slabs, 1 TiB, is made, opened and committed
  to in the memory of one machine, and what one commit reads and writes is
  measured exactly. What the sparse medium does not measure is a disk's time:
  vessels up to 16 GiB were made on the disk as well
  (`BenchmarkScaleVesselDisk`).
- **One ledger in memory** (`BenchmarkScaleChain`, `BenchmarkScaleAuthority`,
  `BenchmarkScaleOpenAddress`, `BenchmarkScaleSeedLedger`): events signed and
  judged as a door judges them, with no vessel under them.
  `BenchmarkScaleDoor`, `BenchmarkScaleWriters`, `BenchmarkScaleReaders` and
  `BenchmarkScaleReconcile` put a carrier on the disk under the same work.

## 1. The size of one vessel

A vessel is N slabs of S bytes each: S from 256 KiB to 64 MiB, N from 16 to
4,194,304 (`vessel/names.go`: `MinSlabLog2`, `MaxSlabLog2`, `MinSlabs`,
`MaxSlabs`). Every 4,096 slabs have one inventory segment of 4,096 entries of
46 bytes, held in a slab of its own (contract 2.4); G = ⌈N/4096⌉ segments in
all. The recording measured is one short note, a head of 180 bytes and an
envelope of 330, 510 bytes in all, after eight others, so that it lands behind
records already in a pack (`BenchmarkScaleVessel`).

### 1.1 Measured in memory, slabs of 256 KiB

| vessel | N | G | make | open | read to open | held open | one note | written | read | files written | written ÷ 510 B |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 4 MiB | 16 | 1 | 7 ms | 0.8 ms | 0.5 MiB | 0.26 MiB | 4.6 ms | 0.56 MiB | 1.8 MiB | 3 | 1,157 |
| 64 MiB | 256 | 1 | 27 ms | 1.7 ms | 0.5 MiB | 0.27 MiB | 4.9 ms | 0.56 MiB | 1.8 MiB | 3 | 1,157 |
| 1 GiB | 4,096 | 1 | 0.61 s | 20 ms | 0.5 MiB | 0.48 MiB | 23 ms | 0.56 MiB | 1.8 MiB | 3 | 1,157 |
| 4 GiB | 16,384 | 4 | 2.9 s | 59 ms | 1.25 MiB | 1.1 MiB | 67 ms | 1.31 MiB | 3.3 MiB | 6 | 2,699 |
| 16 GiB | 65,536 | 16 | 8.9 s | 0.24 s | 4.25 MiB | 3.8 MiB | 0.30 s | 4.31 MiB | 9.3 MiB | 18 | 8,867 |
| 64 GiB | 262,144 | 64 | 26 s | 1.4 s | 16.2 MiB | 14 MiB | 1.26 s | 16.3 MiB | 33 MiB | 66 | 33,539 |
| 256 GiB | 1,048,576 | 256 | 94 s | 4.7 s | 64 MiB | 56 MiB | 5.2 s | 64.3 MiB | 129 MiB | 258 | 132,229 |
| 1 TiB | 4,194,304 | 1,024 | 346 s | 19 s | 256 MiB | 224 MiB | 23.2 s | 256.3 MiB | 513 MiB | 1,026 | 526,987 |

"make" here is the vessel's own work of making N files, without a disk's time.
The 1 TiB vessel is the most slabs the format allows. The process held 7.1 GiB
at its highest, most of it the medium's own record of the files.

### 1.2 Measured in memory, larger slabs

| vessel | S | N | one note | written | read | written ÷ 510 B |
|---|---|---|---|---|---|---|
| 16 MiB | 1 MiB | 16 | 9.2 ms | 2.06 MiB | 5.6 MiB | 4,241 |
| 4 GiB | 1 MiB | 4,096 | 27 ms | 2.06 MiB | 5.6 MiB | 4,241 |
| 64 GiB | 1 MiB | 65,536 | 0.37 s | 17.1 MiB | 35.6 MiB | 35,081 |
| 1 GiB | 64 MiB | 16 | 0.72 s | 128.1 MiB | 320.6 MiB | 263,301 |
| 4 GiB | 64 MiB | 64 | 0.76 s | 128.1 MiB | 320.6 MiB | 263,301 |
| 16 GiB | 64 MiB | 256 | 0.84 s | 128.1 MiB | 320.6 MiB | 263,301 |

### 1.3 Measured on the disk

`BenchmarkScaleVesselDisk` makes the vessel in a folder on the disk, with the
host's randomness in every slab and every file flushed, and records the same
note.

| vessel | S | N | make | made at | open | one note |
|---|---|---|---|---|---|---|
| 4 MiB | 256 KiB | 16 | 0.04 s | 110 MiB/s | 2 ms | 7.5 ms |
| 64 MiB | 1 MiB | 64 | 0.37 s | 173 MiB/s | 13 ms | 33 ms |
| 1 GiB | 1 MiB | 1,024 | 6.2 s | 167 MiB/s | 13 ms | 38 ms |
| 4 GiB | 1 MiB | 4,096 | 44 s | 93 MiB/s | 29 ms | 59 ms |
| 16 GiB | 1 MiB | 16,384 | 179 s | 91 MiB/s | 0.11 s | 163 ms |
| 1 GiB | 64 MiB | 16 | 8.6 s | 119 MiB/s | 0.34 s | 1.34 s |
| 4 GiB | 64 MiB | 64 | 33 s | 123 MiB/s | 0.40 s | 1.39 s |

Making a vessel writes all of it, at the disk's pace. A note on the disk costs
what the model below says for the medium in memory and about as much again for
the disk: 59 ms against 27 at 4 GiB, 1.34 s against 0.72 with slabs of 64 MiB.

### 1.4 What the numbers fit

For one small note in a vessel of N slabs of S bytes:

- **Written: (1 + G)·S + 64 KiB**, to the byte at every size run: the last
  pack, rewritten whole with the note behind what it held; every inventory
  segment; one head file. Once N passes 4,096, G·S is the vessel's size divided
  by 4,096, whatever the slab: **every commit writes at least 1/4096 of the
  vessel.** With the default slab of 1 MiB that is 2.06 MiB for half a
  kilobyte, as STATE.md says, until the vessel passes 4 GiB, and then 1 MiB
  more for every further 4 GiB.
- **Read: (2G + 3)·S + 576 KiB**: every segment twice, the last pack and the
  packs of the last generations, and nine head files. The commit's first step
  takes the highest valid generation, and its fourth sees that it still is
  (contract 2.6); both are answered by verifying the whole generation, every
  segment by its digest and its tag (`vessel/vessel.go`, `(*Vessel).choose`,
  `(*Vessel).verify`).
- **Time: about 5 µs for every slab of the vessel, whatever its size, plus the
  sealing and hashing of the bytes above.** The commit copies the whole
  inventory, looks at every entry for a free slab, encodes every segment
  (`vessel/tx.go`, `(*Tx).Commit`), and lists every slab file of the vessel to
  report the missing (`vessel/vessel.go`, `(*Vessel).survey`). From 4.6 ms at
  16 slabs to 23.2 s at 4,194,304: 4.9 µs a slab at a million, 5.5 at four
  million.
- **Opening** reads and verifies every segment, G·S bytes, and holds about 56
  bytes a slab in memory: 224 MiB for the 1 TiB vessel.

The contract writes "the changed inventory segments" (2.6, step 3). This tree
writes all of them at every commit, each to a new slab, and a segment moved
changes the entries of the segments that cover its old place and its new one:
every segment changes because every segment is written. A commit that wrote
only the segments whose entries it changes (the pack's, and those of the slabs
it frees and fills, its own new segments placed among them) would write the
pack, a few segments and a head for a note, whatever the vessel's size. Nothing
in the byte form changes for that.

### 1.5 The ceiling of the format, and a PiB

The format ends at 2^22 slabs of 2^26 bytes: **256 TiB**. A vessel of a PiB is
four times beyond it, and is not a version 1 vessel under any parameters.

At the ceiling itself, S = 64 MiB and N = 4,194,304, the model above gives one
small note: 1,025 slabs written, 64 GiB; 2,051 read, 128 GiB; about 23 s of the
processor for the inventory (the 1 TiB row, which has the same N) and some five
minutes more to seal, open and hash 192 GiB at the rate the 64 MiB rows show
(about 1.6 ms a MiB), before any disk's time. A vessel of 256 TiB can be made,
in some four weeks at the rates at which 1.3 made vessels; it then records one
note every six minutes or more, and each writes 1/4096 of the vessel.

Where a vessel stops being usable is set long before that, by the same
per-commit cost, which is about 6.5 µs for every slab with the default slab of
1 MiB: a note takes a second at about 150 GiB and ten seconds at about 1.5 TiB,
before the disk's own time, which about doubled it in 1.3. Larger slabs make
fewer of them and more to write: with slabs of 64 MiB no note takes less than
0.7 s, and one takes about 5.5 s at 4 TiB. By this model a vessel of the same
design at a PiB would write at least 256 GiB and read at least 512 GiB for
every note, and spend some 90 s on an inventory of 16,777,216 slabs of 64 MiB:
many minutes a note on any disk. What a PiB needs is in §7.

### 1.6 A large thing

A vessel of many GiB is filled by large things, not by notes: content, which a
`content.put` event describes and which lives inside the vessel in chunks of at
most S − 4 KiB (contract 2.5, E5). `BenchmarkScaleContent` brings one thing
into a carrier on the disk in one recording, from a source read as a stream
(`content.Bring`), then opens the folder afresh and reads the thing back whole
(`content.Fetch`). The carrier has the default slabs, 64 of 1 MiB, and grows by
itself 64 at a time, as `rokh init --growth auto:64:65536` makes it.

| size | bring | rate | growth steps | open again | fetch | process at most |
|---|---|---|---|---|---|---|
| 1 MiB | 0.05 s | 20 MiB/s | 0 | 9 ms | 0.02 s | 18 MiB |
| 16 MiB | 0.20 s | 78 MiB/s | 0 | 41 ms | 0.30 s | 75 MiB |
| 256 MiB | 3.7 s | 70 MiB/s | 4 | 0.36 s | 4.5 s | 1.0 GiB |
| 1 GiB | 29 s | 35 MiB/s | 16 | 4.5 s | 17 s | 4.0 GiB |
| 2 GiB | 80 s | 26 MiB/s | 32 | 8.1 s | 37 s | 8.0 GiB |

- **A large thing is held in memory four times over**, from 256 MiB up: the
  recording keeps every sealed chunk until its commit (`carrier/carrier.go`,
  `(*Recording).Content`, through `(*Tx).Put`), the commit packs them all into
  slab bodies at once (`vessel/tx.go`, `(*Tx).Commit`), and reading it back
  gathers the whole before anything is written (`content/vessel.go`, `Fetch`:
  nothing is written before every chunk has opened and the whole has hashed,
  E8). On this machine the largest thing that can go in or come out is about
  3.5 GiB. STATE.md already says this of the home; it is so in the core too.
- **Bringing slows as the thing grows**: a growing vessel grows one step and
  tries the whole commit again, packing everything again, until it fits
  (`vessel/tx.go`, `(*Tx).CommitGrowing`): 32 tries for 2 GiB, each copying
  2 GiB. The rate falls from 78 MiB/s to 26.
- **Reading it back opens every chunk twice**, once to hash the whole and once
  to hand it over (`carrier/carrier.go`, `(*Carrier).ContentToSized`), and the
  first opening after the commit verifies every slab the last generations
  wrote: 8 s to open again after a thing of 2 GiB.

## 2. The length of one history

### 2.1 Measured: one branch, in memory

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
the newest, in the ledger's order.

### 2.2 Measured: a door on the disk

`BenchmarkScaleDoor` makes a carrier on the disk holding n notes and opens a
door on it as a door opens: the carrier, the owner's session, the references,
the ledger loaded from them, the daemon. Then it asks, through the door's own
`Handle`, for ten writes, ten statuses and ten times the last 50 lines of the
log.

| events | open | held per event | one write | status | last 50 of the log | vessel | process at most |
|---|---|---|---|---|---|---|---|
| 1,000 | 0.14 s | 4,438 B | 25 ms | 0.65 ms | 0.64 ms | 64 MiB | 24 MiB |
| 10,000 | 1.5 s | 3,510 B | 36 ms | 4.5 ms | 3.6 ms | 64 MiB | 90 MiB |
| 100,000 | 14.1 s | 2,709 B | 366 ms | 211 ms | 147 ms | 64 MiB | 0.71 GiB |
| 300,000 | 43.8 s | 2,952 B | 1,175 ms | 752 ms | 523 ms | 192 MiB | 2.5 GiB |

`BenchmarkScaleReconcile` copies such a carrier, records one note in each copy,
opens each copy as the command line opens it (`keyview.Open`, with the owner's
passphrase: the whole history read, and every event's envelope opened and
judged at its own point), and reconciles the two.

| events | open, as the command line does | reconcile | records moved | process at most |
|---|---|---|---|---|
| 1,000 | 0.21 s | 0.055 s | 2 | 32 MiB |
| 10,000 | 2.2 s | 0.18 s | 2 | 0.18 GiB |
| 100,000 | 20.6 s | 2.3 s | 2 | 1.4 GiB |

Every command of the command line opens the carrier this way, so every command
costs about 210 µs for every event of the history before it does anything: two
seconds at ten thousand events, twenty at a hundred thousand. A reconcile goes
through both histories whole, however little differs: 2.3 s to move two notes
between two copies of a hundred thousand events.

### 2.3 What the numbers fit

- **Judging and verifying cost the same for every event**, 65 to 90 µs, most of
  it one signature; so a history is **verified in time linear in its length**:
  86 µs an event in a bare ledger, 146 µs at a door, where the carrier, the
  index and the door's own tables are read and built as well, and 210 µs as the
  command line opens it, every envelope opened. Every opening verifies the
  whole history, and that is the design: one person, offline, with a small
  program, verifies the whole ledger (N2.6).
- **Memory is linear**: about 2.1 KB an event in the ledger, about 3 KB at a
  door, and the process at its highest holds about twice that while it loads.
- **At a door, every answer grows with the history**, and nothing requires it:
  a write costs 25 ms and then about 3.9 µs more for every event already held,
  a status 2.5 µs, the last 50 lines of the log 1.7 µs. Three causes, all in
  this tree:
  1. Every commit drops the vessel's index of records (`vessel/tx.go`,
     `(*Tx).Commit`), and the next read builds it again by reading, hashing and
     opening every pack of the vessel (`vessel/vessel.go`,
     `(*Vessel).indexOnce`).
  2. The branch references are found by opening every branch pointer ever
     recorded: each commit records one, and one superseded stays in its pack
     (`carrier/carrier.go`, `(*Carrier).Refs`). The pointers lie in every pack,
     and the vessel keeps at most eight slabs opened (`vessel/vessel.go`,
     `(*Vessel).slab`), so each asking reads, hashes and opens the whole vessel
     again. A door asks before every answer (`daemon/daemon.go`,
     `(*Server).behind`) and again for a status or a write. In a profile of the
     door at 100,000 events (`-cpuprofile` on the measurement above), this was
     6.5 s of the 7.0 s its thirty answers took.
  3. The ledger's order is dropped at every accepted event (`ledger/ledger.go`,
     `(*Ledger).settle`) and made again whole when next asked for
     (`(*Ledger).ordered`); `log` without a cursor takes the whole order and
     walks it to keep its last lines (`daemon/daemon.go`, `(*Server).log`).

### 2.4 From the newest event to the first

In a ledger already held in memory, the way back to the first event is linear
and short: 0.49 µs a step by the parents, a million steps in half a second; the
causal past with its order, 6.5 s for a million.

From the carrier nothing is held until it is verified, so the first event is
reached only by opening, which verifies the whole history first: 86 s for a
million events in a bare ledger and, by the door's own rate, about two and a
half minutes at a door. By the same rates a billion events take a day of one
processor to reach their first, and some 2 to 3 TB of memory, which no machine
of this kind has.

### 2.5 How long a history can grow

Nothing in the format counts generations: an event names up to sixteen parents
(`event/event.go`, `MaxParents`) and carries up to 4 KiB (`MaxPayload`; T3.4
calls that number a choice, not a limit of the world). A history is limited by
what opening it holds and costs:

- **Memory.** On this machine, 15.7 GiB: a bare ledger of some three million
  events (the process reached 5.4 GiB at its highest for one million, though a
  ledger holds 2.3 KB an event once loaded, which would allow seven), and a
  door of some two million (2.5 GiB at its highest for 300,000).
- **Time at a door.** A write takes a second at about 250,000 events and ten
  seconds at about 2.5 million, by 2.2.
- **Time to open.** At every opening: about 146 µs an event for a door, and
  210 µs for every command of the command line, which opens the carrier each
  time and opens every envelope as it goes. At a million events that is two and
  a half minutes for a door and three and a half for each command; at five
  million, on a machine with the memory for it, twelve and seventeen.

One fault this measurement found and repaired: the walk that loads a history
went into the parents of each event by recursion, about 1.9 KiB of stack for
each generation it descended, and a branch of some 550,000 events ended the
process with a stack overflow, so a longer history could not be opened at all.
It now keeps its own stack (`ledger/load.go`, `(*Ledger).ExtendWith`;
`ledger/deep_test.go`). A history of a million events opens, as 2.1 shows.

## 3. The people one Rokh names

A person enters a Rokh in one of six ways, and each grows differently: as a key
that opens the vessel with a passphrase of its own; as a reader named in the
envelopes of an address; as a grantee who writes under a grant; as a writer
through a door, one commit at a time; as anyone at all, at an open address; and
as a founder of a bond, whose ledger is their own.

### 3.1 Measured: grants, revocations and keyring changes

`BenchmarkScaleAuthority` names n people in one ledger: a grant to each (a key
of their own, a scope of their own), then every one of those grants taken back,
and apart from that n keyring generations. Each event is judged in its causal
past.

| people | grants: held | judge one | revocations: held | judge one | keyring: held | judge one | live keyring |
|---|---|---|---|---|---|---|---|
| 250 | 2.6 MiB | 83 µs | 2.6 MiB | 102 µs | 2.6 MiB | 96 µs | 0.9 ms |
| 500 | 9.2 MiB | 90 µs | 9.1 MiB | 81 µs | 9.3 MiB | 86 µs | 1.1 ms |
| 1,000 | 34 MiB | 110 µs | 34 MiB | 91 µs | 35 MiB | 92 µs | 2.7 ms |
| 2,000 | 136 MiB | 126 µs | 136 MiB | 164 µs | 137 MiB | 105 µs | 5.9 ms |
| 4,000 | 523 MiB | 177 µs | 522 MiB | 211 µs | 523 MiB | 124 µs | 12 ms |
| 8,000 | 2,027 MiB | 249 µs | 2,026 MiB | 1,061 µs | 2,029 MiB | 200 µs | 27 ms |

"held" is the memory the ledger took for those events; for revocations it is
what taking every grant back added to what the grants held. "live keyring" is
the keyring as seen from the heads, which every sealing asks for. The
revocations are taken back newest first: taken in the order they were made, the
set of revocations after k of them would equal the set of the first k grants,
and the ledger's pool would keep it once, which is sharing that revocations in
no particular order do not get.

### 3.2 Measured: readers, and the passphrase

`BenchmarkScaleEnvelope` seals a body of 300 bytes to r readers and opens it by
the last one; `BenchmarkScalePassphrase` derives the owner's key at the default
cost and tries it on every slot cell.

| readers | envelope | overhead | seal | open |
|---|---|---|---|---|
| 1 | 432 B | 132 B | 132 µs | 54 µs |
| 2 | 500 B | 200 B | 176 µs | 110 µs |
| 4 | 636 B | 336 B | 258 µs | 58 µs |
| 8 | 908 B | 608 B | 558 µs | 62 µs |
| 16 | 1,452 B | 1,152 B | 990 µs | 54 µs |
| 32 | 2,540 B | 2,240 B | 1,785 µs | 53 µs |
| 33 | refused | | | |

The passphrase at the default 600,000 rounds: 128 ms; trying its key on all 32
slot cells: 132 µs.

### 3.3 Measured: an open address

`BenchmarkScaleOpenAddress` makes one open grant at `commons` (contract 4.5)
and has n people, each a key of their own that nobody granted, added to the
keyring or named in an envelope, write one note there.

| people | held per person | judge one | sign one | process at most |
|---|---|---|---|---|
| 1,000 | 2,454 B | 75 µs | 107 µs | 13 MiB |
| 10,000 | 2,244 B | 72 µs | 103 µs | 47 MiB |
| 100,000 | 2,111 B | 72 µs | 102 µs | 0.36 GiB |
| 1,000,000 | 2,447 B | 78 µs | 105 µs | 3.7 GiB |

### 3.4 Measured: writers and readers at once

`BenchmarkScaleWriters` puts several doors on one carrier of 1,000 events, each
its own daemon on the folder as separate programs open it, and several writers
at each door, each recording eight notes at once.

| doors × writers | writes a second | median | slowest 1% | slowest | refused |
|---|---|---|---|---|---|
| 1 × 1 | 37.6 | 26 ms | 29 ms | 30 ms | 0 |
| 1 × 4 | 38.5 | 97 ms | 130 ms | 148 ms | 0 |
| 1 × 16 | 32.4 | 483 ms | 550 ms | 574 ms | 0 |
| 1 × 64 | 31.2 | 1.96 s | 2.28 s | 2.30 s | 0 |
| 2 × 1 | 34.0 | 28 ms | 37 ms | 253 ms | 0 |
| 4 × 1 | 33.2 | 28 ms | 505 ms | 754 ms | 0 |
| 8 × 1 | 37.6 | 25 ms | 1.26 s | 1.49 s | 0 |
| 16 × 1 | 40.3 | 22 ms | 2.51 s | 2.97 s | 0 |
| 32 × 1 | 40.0 | 22 ms | 5.47 s | 6.21 s | 0 |
| 8 × 8 | 38.3 | 204 ms | 11.6 s | 11.7 s | 0 |
| 16 × 4 | 39.3 | 94 ms | 11.1 s | 12.1 s | 0 |
| 32 × 4 | 36.7 | 96 ms | 23.1 s | 25.5 s | 11 of 1,024 |

The times are each write's, from asking to the answer. An earlier run, without
the rows at 32 doors and at 16 doors of four, gave the same to within 15 per
cent.

`BenchmarkScaleReaders` puts readers on a carrier of 10,000 events, each asking
in turn for the status and for the last 50 lines of the log, forty times, first
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

No read failed.

### 3.5 Measured: a bond among many

`BenchmarkScaleLeaf` makes the founding leaf of a bond among n founders: its
bytes, its name, reading it back, and its standing when all but one have
accepted it.

| founders | leaf | name it | read it | standing |
|---|---|---|---|---|
| 2 | 200 B | 0.13 ms | 0.03 ms | 0.001 ms |
| 100 | 6.8 KB | 0.38 ms | 0.43 ms | 0.013 ms |
| 10,000 | 670 KB | 31 ms | 44 ms | 2.4 ms |
| 1,000,000 | 67 MB | 2.7 s | 3.4 s | 255 ms |

### 3.6 What the numbers fit

- **People named cost the square of their number.** Every grant, revocation and
  keyring change is judged in its causal past, and the ledger keeps, for that
  event, the set of every grant (or revocation, or keyring change) in that
  past: a sorted list, copied whole with one more member (`ledger/set.go`,
  `idset.add`), kept in a pool that shares equal lists under a key as long as
  the list (`interner.canon`). So n people named hold about 33·n² bytes: 34 MiB
  for a thousand and 2 GiB for eight thousand, as measured; 3.3 GB for ten
  thousand, 330 GB for a hundred thousand and 33 TB for a million, by the same
  square. Judging one more copies the list: 83 µs at 250, 249 µs at 8,000, and
  more when memory is short (a revocation at 8,000: 1.06 ms).
- **Readers cost their number, and stop at 32.** An envelope wraps its key once
  for each reader of its address: 64 + 68·r bytes, and about 55 µs of sealing
  for each reader, one key agreement each. Opening costs the same for any
  number, since a reader finds its own wrap by its key id. The contract allows
  1 to 32 (3.1) and a session asked for more refuses (`key/key.go`,
  `SealReaders`); since every key whose reads cover an address is its reader
  (contract 4.4, `(Ring).Readers`), an address read by more than 32 keys, the
  owner's among them, can no longer be written. The seed's own key reads
  nothing for that very reason (`cmd/rokh/v1_seedplan.go`).
- **Openers stop at 32 slot cells**: the owner and 31 keys (`vessel/names.go`,
  `SlotCells`). A passphrase costs 128 ms at the default rounds, whoever holds
  it.
- **At an open address every person costs the same**: about 2.1 to 2.5 KB and
  72 to 78 µs each, from a thousand people to a million, because the one grant
  is the same for all and no set grows (contract 4.5). It is the one way a Rokh
  takes a crowd, and it takes it as a crowd: nobody there is named, nobody can
  be taken back alone, and what they write is read by the address's readers, at
  most 32.
- **Writers take turns**, one commit at a time for the whole carrier, whatever
  the doors: about 35 commits a second at 1,000 events, each writer waiting for
  those ahead of it: 26 ms alone, 97 ms behind three others, 2 s behind 63.
  Across doors the turn goes to whoever tries first once it is free, each door
  trying again every 10 ms (`turn/turn.go`, `acquire`), so the median wait
  stays short while the slowest grows far faster: the slowest 1% waited 5.5 s
  at 32 doors of one writer each, and 11 s at 8 doors of eight. At 32 doors of
  four writers, 11 of 1,024 writes were refused, `turn_busy`, having waited out
  the turn's patience of 15 s (`daemon/commit.go`, `turnPatience`), and the
  slowest 1% of the others waited 23 s, the door's own queue before the turn
  counted in. Nothing answered `recorded` was lost: every such write was in the
  ledger read afresh afterwards. And every commit costs more as the history
  grows (§2.3): 2.7 writes a second at 100,000 events, 0.85 at 300,000.
- **Readers share a door, and a writer stops them.** With nobody writing,
  readers run side by side and the rate grows with the processors, not the
  readers: 382 reads a second alone, about a thousand with four processors
  busy. One writer brings it down to 32 at one door and 107 at four: every
  commit makes every door's view stale, and the first reader after it brings
  the view up under the door's lock held alone, reading and opening every pack
  of the vessel again (§2.3), while the others wait.
- **A bond's leaf is as long as its founders**: 67 bytes each, named in about
  2.7 µs each, and a founder accepts it only by holding all of it and finding
  their own anchor in it (`bond/across.go`, `Accept`).

### 3.7 From a family to the Earth

One carrier, each person in it in each of the six ways. What was measured is
marked; the rest follows from the fits above, and says where it stops.

| people | named (grants) | readers of one address | openers | one note each, through one carrier | at an open address | founders of a bond |
|---|---|---|---|---|---|---|
| a family, 5 | 0.8 KB | 404 B on each envelope, 0.35 ms to seal | 5 of 32 | 0.13 s | 11 KB | a 0.4 KB leaf |
| a household, 30 | 30 KB | 2.1 KB, 1.7 ms | 30 of 32 | 0.8 s | 66 KB | 2 KB |
| a village, 1,000 | 34 MiB, measured | cannot be sealed | cannot open | 27 s | 2.4 MB, measured | 67 KB |
| a town, 10^4 | 3.3 GB | | | 7.4 min | 22 MB, measured | 670 KB, measured |
| a city, 10^6 | 33 TB | | | 23 days (8 h at a steady 35 a second) | 2.4 GB, measured | 67 MB, measured |
| a country, 10^8 | 330 PB | | | 600 years (33 days) | 210 GB | 6.7 GB |
| the Earth, 8·10^9 | 2·10^21 B | | | 4 million years (7 years) | 17 TB | 536 GB |

"One note each" is the time for every person to record one note through one
carrier, one commit at a time at today's cost, 25 ms and 3.9 µs for every event
already held (§2.3); in brackets, at the best rate measured, if a commit cost
the same at any length. So:

- **At 32 people** the design's own numbers are met: no more readers of one
  address, no more openers of one vessel. Past them, a family shares a Rokh
  only by grants that name each writer and by addresses each one reads with at
  most 31 others.
- **At some twenty thousand people named**, the authority sets alone fill a
  machine of this size: 13 GB at 20,000 by the square. This is in this tree,
  not in the design (§7.1.4).
- **At some ten thousand writers**, one carrier's single turn and its growing
  cost make a round of one note each take minutes; at a million, weeks.
- **At about a hundred million**, even an open address, whose cost per person
  is constant, holds more than one machine can, and its readers are still at
  most 32.
- **At eight billion**, nothing about one Rokh fits: not its memory, not its
  turn, not its readers. By its own texts a multitude is many Rokhs, each a
  person's, joined by bonds (§7.3); and a bond among all of them, as it is
  written today, would be a leaf of 536 GB that every founder must hold.

## 4. Seeds

A seed is a new vessel for the same ledger, given by one Rokh and taken by the
new one; two seeds become one again only by reconcile. Giving a seed records
four events in the lineage, as `cmd/rokh/v1_seedplan.go` (`newGiving`) and the
take make them: the keyring add of the seed's own key, a grant to its signer,
the give on the source, and the take on the new vessel, signed by the seed's
key under that grant.

### 4.1 Measured through the command line

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
100 events (line) and 77 (fan), all but two of them the ledger's own.

### 4.2 Measured in one ledger

`BenchmarkScaleSeedLedger` (in `bench`) follows seeds past what the command
line makes in a measurement: the same four events for every seed, judged by one
ledger, as a line and as a fan (where the root holds every take once it has
reconciled every seed), and the whole lineage loaded again from its bytes.

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
parents; "load all" is the lineage read again from its bytes, as the last seed
(line) or the root that reconciled every seed (fan) opens it.

### 4.3 What the numbers fit

- **What a seed holds of those before it grows linearly with how many there
  were.** In a line the n-th seed holds 3 + 4n events: the genesis, the owner's
  keyring add and the first note, and four for itself and every seed before it.
  In a fan the root gives three events a seed and the take stays in the seed,
  so the n-th seed holds 4 + 3n. The notes do not grow at all: every seed held
  the one note written before the first give.
- **Its memory grows as the square of the seeds.** Every keyring event and
  every seed event joins the system set that each later event carries, and
  every grant the grant set (`ledger/ledger.go`, `(*Ledger).evaluate`), with
  the same whole copies as in §3.6: about 340·n² bytes in a line and 240·n² in
  a fan, 1.3 GiB for 2,000 seeds in a line. On a machine of this size the
  lineage of some 7,000 seeds in a line, or 8,000 in a fan, can no longer be
  opened.
- **Judging grows linearly, and loading as a little less than the square**: one
  event of the lineage costs 149 µs at 250 seeds and 623 µs at 2,000, and the
  whole lineage of 8,001 events loads in 4.3 s, where 10,000 notes load in
  0.7 s. A take also reads the whole system set to find the key that signs it
  (rule S1 in `(*Ledger).evaluate`): 5.9 ms for the last of 2,000 in a fan.
- **Giving a seed through the command line** costs about 0.8 s, almost all of
  it the same for every seed (the passphrase at its rounds, a new vessel and
  its cells), and rose to 0.94 s over 24 seeds as the source's history grew.
- **Two seeds merge by reconcile**, the union of both, judged by each side:
  each side is opened whole, so a merge costs at least the opening of both
  (§2.2) and holds both lineages in memory. A lineage merges as long as it can
  be opened: the 7,000 seeds above.

### 4.4 The billionth seed

What was measured stops at 2,000 seeds; what follows is the same linear count
and the same square, carried on.

- **What it holds of the first.** In a line, the billionth seed holds four
  billion and three events: the genesis, the owner's first keyring add and the
  note written before the first give — everything the root held when it gave
  its first seed — and then, for every one of the billion seeds, the keyring
  add of its key, its grant, its give and its take. The first seed is all
  there: a seed carries the whole causal past of its give, and nothing recorded
  is ever dropped (law 5). What is not there is what the first seed recorded
  after it gave the second, until a reconcile brings it.
- **What it costs.** Some 1.5 TB of events at about 385 bytes each; some 9 TB
  of memory for the ledger at 2.3 KB each; and, by the square, about 3·10^20
  bytes (some 340 EB) of authority sets. It cannot be opened, nor its take
  judged: the take reads a system set of three billion members. By the square
  it stops being possible at some 7,000 seeds on this machine, and at some 10^5
  on a machine with a few TB of memory.
- **Cause and effect.** A take is refused unless its give is in its causal past
  (`(*Ledger).evaluate`), and in a line each give is made on the seed before
  it, after that seed's take. So the causes run in one line from the first
  event to the last: genesis, the first give, the first take, the second
  keyring add, grant and give, the second take, and so on: 4·10^9 steps, each
  an event naming the one before it. Walked in memory at 0.49 µs a step it
  would take half an hour, if a machine held it; verified from the carrier, at
  least four days of one processor at the rate of plain notes (86 µs an event),
  and far longer with the sets as they are. In a fan every seed is a step from
  the root, the depth grows by three a seed rather than four, and the root that
  reconciles them all carries the same weight: three events a seed, as the
  square.

## 5. How each thing grows

| what | grows with | how | measured |
|---|---|---|---|
| what one note writes | the vessel | linearly, at least 1/4096 of it | 0.56 MiB at 4 MiB, 256 MiB at 1 TiB |
| what one note costs in time | the slabs | linearly, about 5 µs a slab | 4.6 ms at 16 slabs, 23 s at 4,194,304 |
| opening a vessel | the slabs | linearly | 0.8 ms to 19 s |
| judging one event | the history | constant | 65 to 90 µs |
| opening a ledger | the history | linearly | 86 µs an event; 146 at a door; 210 at each command |
| memory | the history | linearly | about 2.1 KB an event, 3 KB at a door |
| a write, a status, a log at a door | the history | linearly, though nothing requires it | 25 ms to 1.2 s; 0.65 to 752 ms; 0.64 to 523 ms |
| memory for grants, revocations, keyring | the people named | as the square | 2.6 MiB for 250, 2 GiB for 8,000 |
| judging one more of them | the people named | linearly | 83 to 249 µs |
| an envelope, and sealing it | its readers | linearly, and no further than 32 | 432 to 2,540 B; 0.13 to 1.8 ms |
| opening an envelope | its readers | constant | 53 to 62 µs |
| commits a second | writers and doors | constant | about 35 |
| a writer's wait | the writers ahead | linearly, and unfairly | 26 ms alone, 2 s behind 63 at one door; the slowest 1% 23 s among 128 on 32 doors, 11 refused |
| a person at an open address | the people there | constant | 2.1 to 2.5 KB and 72 to 78 µs, from a thousand to a million |
| a bond's leaf | its founders | linearly | 67 B a founder |
| what a seed holds of those before it | the seeds before it | linearly | 4 events a seed in a line, 3 in a fan |
| memory for a lineage of seeds | the seeds | as the square | 23 MiB for 250, 1.3 GiB for 2,000 in a line |
| a large thing brought in | its size | memory four times it; time as more than its size | 1.0 GiB for 256 MiB, 8.0 GiB for 2 GiB; 3.7 s, 80 s |

## 6. The first bottlenecks, in the order they are met

In the order a Rokh meets them as it grows. The last column says whether the
limit is in the design (the texts or the contract) or only in this tree.

| | met at | what | where | in |
|---|---|---|---|---|
| 1 | the 33rd reader of an address, the 32nd key besides the owner | an envelope names at most 32 readers and a vessel opens for 32 slot cells; beyond them an address cannot be sealed, a key cannot open | contract 3.1; `key/key.go` `MaxReaders`; `vessel/names.go` `SlotCells` | the contract |
| 2 | a few writers at once | one commit at a time for the whole carrier, about 35 a second; the turn goes to whoever tries first, so the slowest wait grows much faster than the median, and among 128 writers on 32 doors some are refused after 15 s | `turn/turn.go` `acquire`; `daemon/commit.go` `turnPatience` | this tree |
| 3 | about 10^5 events | a door's write, status and log each walk the whole vessel or the whole ledger: a write takes 0.37 s at 100,000 events and 1.2 s at 300,000 | §2.3 | this tree |
| 4 | about 10^4 people named, or seeds given | grants, revocations, keyring changes and seed events are kept as whole sets per event: memory as the square of their number, 2 GiB at 8,000 | `ledger/set.go`; §3.6, §4.3 | this tree |
| 5 | about 100 GiB of vessel | every commit writes every inventory segment and verifies all of them twice: 1/4096 of the vessel written for one note, a second of work at about 150 GiB | `vessel/tx.go` `(*Tx).Commit`; §1.4 | this tree (the contract writes only the changed segments) |
| 6 | a thing of a few GiB | a large thing is held in memory four times over, and a growing vessel packs it again at every step it grows: 8 GiB and 80 s for 2 GiB | `carrier/carrier.go`, `vessel/tx.go`, `content/vessel.go`; §1.6 | this tree |
| 7 | about 10^6 events | every opening verifies the whole history and holds it in memory: two and a half minutes and some 3 GB for a million events at a door, beyond one machine at some 10^7 | `ledger/load.go`; N2.6 | the design |
| 8 | 256 TiB | the most slabs and the largest slab of the format | `vessel/names.go` | the contract |
| 9 | about 10^5 founders | a bond's leaf lists every founder, and each must hold all of it to accept: 67 MB at a million | `bond/bond.go`, `bond/across.go` | the design |

The first is met by a large family. The next five are met by a village, a busy
year, a large disk or a large file, and are all in this tree, not in the
design: §7.1 removes them without changing a byte form. The last three are in
the design and need a new generation (§7.2), or the reading of the texts that a
multitude is many Rokhs, not one (§7.3).

## 7. What would have to change

Three kinds of change, in the order they can be made. The first keeps every
byte form of version 1; the second is a new generation with a name of its own
(the law of this tree: a byte form is never edited); the third is what the
texts already say about many people.

### 7.1 Within version 1

1. **Write only the changed inventory segments**, as contract 2.6 already says,
   and place a commit's new segments among the ranges it changes anyway; keep a
   list of free slabs instead of looking at every entry; see at the fourth step
   that no other commit came by the head files' tokens, and verify a generation
   whole only when they moved. A note would then cost its pack, a few segments
   and a head at any size (§1.4).
2. **Keep the vessel's index across commits**: add what a commit wrote to it
   instead of dropping it; and find a branch by the last pointer that names it,
   whose header already carries the branch's name (contract 2.5), so that one
   pointer is opened for each branch, not every pointer ever recorded. A door's
   status and its reads stop growing with the history (§2.3, causes 1 and 2).
3. **Keep the ledger's order and extend it**: an event accepted after every
   head it names goes at the end; only one that lands among older events makes
   the order again. The last lines of a log are read from the end (§2.3, cause
   3).
4. **Authority sets that share their structure**: a persistent ordered set (a
   balanced tree, or a hash trie, copied along one path only) in place of a
   sorted slice copied whole. Every event still carries the whole set of its
   causal past, and every verdict is the same (law 6 is about the causal past,
   not about how it is held); one more grant then copies one path of the tree,
   some twenty nodes and a kilobyte or so, not every person named, and a
   million people named hold about a gigabyte, not 33 TB (§3.6).
5. **A fair turn, and one commit for those already waiting**: writers queued at
   a door go into the commit that the first of them makes, since they have all
   asked; the turn is taken in the order it was asked for, not by whoever polls
   first (`turn` is a host adapter, and the lock is its business, law 4). The
   door's rate becomes the disk's, not one note per commit (§3.4).
6. **Large things in bounded memory**: chunks sealed and packed as they are
   read, the recording holding where they went rather than their bytes; a
   growing vessel grows at once by what a recording needs, not one step per try
   (§1.6).

None of these changes what a person can do or see; each removes a cost that
grows where nothing requires it. Together they remove the five bottlenecks of
§6 that are in this tree, and leave the ones that are in the design.

### 7.2 New generations

1. **A vessel of a PiB.** The slab count must grow past 2^22, and the inventory
   must stop being read whole: a tree of inventory segments with its root in
   the head (each segment named by its hash, as the segments are now), packs
   appended and never rewritten, small slabs for small records. A commit then
   writes its records, the segments on one path of the tree and a head; opening
   reads the head and what it walks. This is a new vessel format with a name of
   its own.
2. **Opening without verifying everything again.** A checkpoint: a record the
   owner's key signs, saying that the history up to certain heads was verified
   and what the authority sets were at them. An opening verifies from the last
   checkpoint it trusts; the whole walk stays possible, and is what a
   checkpoint is checked against, so one person offline can still verify
   everything (N2.6). This is what lets a history of a billion events be opened
   by a machine that cannot hold it.
3. **Readers by key per address, not per envelope.** An address sealed to one
   key for a period, the key given to its readers once, in the keyring; a
   change of readers starts a new period (and a tree of keys makes the change
   cost a logarithm of the readers). An envelope is then the same size for two
   readers and for two million; taking a reader back closes the next period and
   not the past, which is what revocation already is (N6.2, T6.2).
4. **A bond's leaf as a tree of its founders.** The name commits to the root;
   each founder accepts with the path from their anchor to the root, about 33
   hashes among eight billion, instead of holding the whole leaf, which at that
   size would be 536 GB.
5. **Seeds that carry only what they rest on.** A seed holds the events its own
   authority rests on and a checkpoint for the rest of its source's history,
   instead of every event of its source's system set.

### 7.3 Billions of people

A Rokh is one person's ledger. Its texts say so and draw the line themselves:
every ledger is the history of one person, and being common is the work of a
higher layer (N2.7); the claim is about the ledger of a person, not about every
possible institution (N2.8); shared, non-exclusive work is closed by each
stakeholder in their own ledger (N2.10); a lasting knot among many is a bond,
in which each keeps their own (T11). So billions of people are billions of
Rokhs, and what has to scale to them is the bond (§7.2.4) and the carrying of
events between ledgers, not one ledger. One Rokh, with 7.1 and 7.2 made, stays
what it is: the ledger of one person and of the few who read and write with
them, at any size of data and any length of history.


