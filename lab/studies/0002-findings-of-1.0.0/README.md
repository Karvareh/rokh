---
id: "0002"
title: "The findings of 1.0.0"
status: examined
of: "rokh 1.0.0 (ba7e695), and the branch of the scale study to b8f909a"
date: 2026-10-01
---

# The findings of 1.0.0

> The audit of the release and the scale study, reconciled: every finding of
> both that may be published, where it is written, what shows it, and whether
> it was only found, repaired and run again, is still open, or waits for a
> ruling (§3). Three requirements of the owner's are set beside the code and
> the documents (§4). What was measured, read, extrapolated and proposed are
> told apart, each with its evidence (§5). Nothing here is a ruling, and a
> limit of version 1 is never called a limit of Rokh. The work these findings
> ask for is in `work/`, and the questions they put to the owner are in
> `lab/questions/` (§6).

*This study was `WORKPLAN.md` in rokh at commit b8f909a, where it was written
as a plan of work; its plan is now the missions of `work/` and the questions of
`lab/`, and the study keeps the findings. Paths named here are paths of the
source as they were then, now under `source/`; `STATE.md` is the source's, and
`AGENTS.md` the repository's. Where a finding says "AGENTS.md, owed n", that
list is now the owner's order, kept in the README of `work/`.*

## 0. How to read this study

**Identifiers.** A finding of the audit is `A-nn`, of the scale study `S-nn`,
of its page on seeds by role `R-nn`, and of the comparison with the three
requirements `Q-nn`. A mission is `W-nn` and an investigation `I-nn`, both in
`work/missions/`; a question for the owner is `D-nn`, in `lab/questions/`.
Section numbers such as §M5 are those of
[study 0001](../0001-scale/README.md).

**The state of a finding.**

| state | meaning |
|---|---|
| open | found; not repaired; no ruling stands in the way of repairing it |
| fixed | repaired in rokh: code with its test, or a document made true; not run again beyond that |
| retested | repaired in rokh, and the run or measurement that found it was made again and no longer shows it |
| needs ruling | repairing it, or saying that it is no fault, waits for the owner; it stays open until the owner rules in the texts |

A finding with no commit in the last column of §3 was only found.

**Kinds of evidence.**

| mark | meaning |
|---|---|
| ✔ | reproduced by a run of the audit |
| ◇ | read in the code, or seen in a run made apart from the audit's own and not reproduced by it |
| M | measured; the raw output is in study 0001, `data/` |
| C | read in the code, at the place named |
| R | read in a reference text (T, N, the contract) or in a document of rokh |
| X | extrapolated from M and C; not run |
| P | proposed; nothing proposed is built |

**Where the details are.** The scale study: [study 0001](../0001-scale/README.md)
and its raw data. The state of rokh: its `STATE.md`; the lines this study added
to it are in commit 00980a9. The audit's own report was written for the owner
and is in no repository; every finding of it that may be published is in §3.1.

## 1. What was done

**The audit** read the release, commit ba7e695, and changed nothing in it. It
ran `go vet` and both suites, the race detector on nine packages, built the
commands and ran them from end to end, and probed what it suspected. Three
separate readings, of the home, of the sentence surface and of the booth,
were checked against the code; what they claimed is marked ✔ where the audit
reproduced it and ◇ where it did not. It found the core's byte discipline,
judgement, vessel and use of cryptography strong, and the evidence around them
(the tests, the conformance map, STATE.md) and the doors facing outwards
(booth, courier, home) weaker than they are said to be.

**The scale measurements** ran fifteen benchmarks, in `rokh/bench` and
`rokh/cmd/rokh`, on one host: four virtual processors at 2.10 GHz, 15.7 GiB of
memory, ext4, Linux 6.18, Go 1.26.4, as the superuser. The production code of
every run is that of commit 4111458. Every number is in
study 0001, tied to a raw output in its `data/`.

**The commits of this branch**, after the release:

| commit | what it does |
|---|---|
| 4111458 | `ledger.Load` walks a history with a stack of its own, not a call per generation (S-01); `ledger/deep_test.go` |
| a72bc74 | the benchmarks of `rokh/bench`, the sparse medium, the version 1 fixture |
| b2c436a | `BenchmarkScaleSeeds`, through the command line |
| f459779 | the revocations of the authority measurement taken back newest first (S-02) |
| 6e65793 | `BenchmarkScaleSeedLedger` |
| 617ce7b | `BenchmarkScaleContent` |
| 91f6101 | `BenchmarkScaleReaders`, and more shapes of writers |
| 086b386 | `rokh/docs/11-scale.md` (now study 0001), and STATE.md, the README and the map of AGENTS.md pointing to it |
| 6abdc97 | the raw outputs, in `rokh/docs/11-scale/` (now study 0001, `data/`) |
| 5b015a4 | the report with measured, read, extrapolated and proposed apart |
| 00980a9 | STATE.md: what the audit found and STATE.md did not say |
| b8f909a | this study, as `WORKPLAN.md` |

Of the production code, only `rokh/ledger/load.go` changed. Everything else is
benchmarks, one test and documents.

**The tests**, on the tree of 91f6101 (study 0001, §T): `gofmt`
and `go vet` clean in both modules; the core's suite as the superuser, 35
packages ok, 5 without tests, `rokh/cmd/rokh` failing in the two tests that
fail for the superuser (A-03); the home's suite, 8 ok, 2 without tests;
`rokh/cmd/rokh` run by an unprivileged account, ok. No skip was added. The
commits after 91f6101 change documents only.

## 2. Findings withheld

Three findings of the audit are of the kinds `SECURITY.md` asks to be reported
privately. They are not described in this study, in STATE.md, in a commit or
in a pull request. They were given to the owner apart from this tree, and
their repair is not ordered here.

## 3. Every finding, where it is written, and its state

### 3.1 The audit

| id | finding | evidence | written in | state | commit |
|---|---|---|---|---|---|
| A-01 | 33 tests skip without a word in a fresh clone and in the checks: each looks for a short socket folder six or seven levels above its package, outside the tree; 16 of the core's, 17 of the gate's, among them the tests behind "Two booths on one protocol" and "Several doors on one carrier at once". Given a folder of 77 bytes or less the core's pass; of the gate's, twelve pass and five want the enclosure's sandbox; at 87 bytes eight fail on the socket path limit of about 104 bytes | ✔ (the limits ◇); `rokh/daemon/booth_test.go:145-154`, `rokh/daemon/daemon_test.go:360`, `rokh/cmd/rokh/harness_test.go:254-259`, `rokh/proof/world_test.go:359`, `rokh-home/gate/gate_test.go:384-399` | STATE.md (Known to fail) | open | — |
| A-02 | The twelve `legacy09` test files do not compile against version 1; among them is the only test that an idle daemon writes nothing, so law 3 has no live test at a door | ✔ | STATE.md (Not built); AGENTS.md, owed 10 | open | — |
| A-03 | Two tests fail when the superuser runs them: they cut a seed with read-only permission bits, which the superuser writes through | ✔; again in the scale runs (§T); `rokh/cmd/rokh/v1_seedresume_test.go:23-39` | STATE.md (Known to fail); study 0001, §T | open | — |
| A-04 | The three skipped tests fail for three different reasons: the `key` test with `ErrOwnerUnknown`, since it gives no point while production always passes one; the delegate test in its setup (`no live key`), while the guard it tests stands (`rokh/cmd/rokh/bond.go:334-338`); the archive test held about 650 MB for 96 MB | ✔ (the archive ◇) | STATE.md (Known to fail); AGENTS.md, owed 1 | open; the `key` test needs ruling (D-06) | — |
| A-05 | The checks run on Linux alone (AGENTS.md asks Linux and macOS), with no format check, no build for another system, no count of skips and no look for files a run changed | ✔; `.github/workflows/check.yml` | STATE.md (Not built) | open | — |
| A-06 | A booth reads and parses a message of up to 8 MiB before its session is bound, and its listener takes any number of connections; one run held about 2.3 GB for twelve connections | ◇; `rokh/booth/booth.go:230-252` (`readLine`, the loop of a session), `rokh/transport/transport.go:124-140` (`Serve`) | STATE.md (Not built) | open | — |
| A-07 | `rokh key passwd` is not built: a passphrase that leaked cannot be replaced | ✔; `rokh/cmd/rokh/main.go:1957` | STATE.md (Not built); AGENTS.md, owed 5 | open | — |
| A-08 | The salt and cost of the passphrase are one per Rokh, in every seed and for every slot cell: one derivation from a guess is tried on all 32 cells of a vessel at once (0.13 ms against 128 ms for the derivation) | R (contract 2.3, 4.6); M (§M7) | STATE.md (What is built, first line) | needs ruling (D-10) | — |
| A-09 | PBKDF2 asks no memory of a guesser; a derivation that does is not in Go's standard library, and law 7 allows nothing else | R (AGENTS.md law 7; contract 2.3) | this study | needs ruling (D-10) | — |
| A-10 | Nothing asks a passphrase to be strong | C | this study | needs ruling (D-10) | — |
| A-11 | `rokh-courier apply` sends `status` and `append` without `hello`; `rokh daemon` refuses with `hello_first`; the courier reports "0 accepted, 1 rejected" and exits 0 | ✔; `rokh/cmd/rokh-courier/main.go:279-305, 388` | STATE.md (Not built) | open | — |
| A-12 | `rokh bind` and `rokh bound` speak the protocol before `rokh.booth/1` | ✔; `rokh/cmd/rokh/harness.go:157, 286, 334` | STATE.md (Not built) | open | — |
| A-13 | The old `Server.Serve` has no caller in production; `capabilities` names the protocol `rokh.daemon/3`; the booth's `seed` leaves out the vessel id, seed id, size and free space contract B2 lists, and gives the count of accepted events as its generation | ✔; `rokh/daemon/commit.go:33`; C, `rokh/daemon/booth.go:246-254` | STATE.md (Not built: the protocol's name); this study (`seed`) | open | — |
| A-14 | No tests at all in `rokh/transport`, `rokh/cmd/rokh-courier`, `rokh/cmd/rokh-forms`, `rokh/cmd/rokh-chest`, `rokh-home/cmd/rokh-home` | ✔ | STATE.md (Not built) | open | — |
| A-15 | `see the grants` ends in a panic, exit 2, on a ledger that holds an open grant | ✔; `rokh/shell/ops.go:431` (`g.Subject[:4]`) | STATE.md (Not built) | open | — |
| A-16 | After a waiting sentence is rewritten, the answer says `write` records it; `write` records another waiting sentence, and the rewritten one is let go when the session closes (T9.2) | ✔; `rokh/shell/ops.go:108-128` | STATE.md (Not built) | open | — |
| A-17 | The guides lose every `key: `, taken for the name of a layer by the expression that tidies errors | ✔; `rokh/shell/plain.go:197-209` (`packagePrefix`) | STATE.md (Not built) | open | — |
| A-18 | Some refusals end with exit 0 and no code, a failed `bring` among them | ◇ | STATE.md (Not built) | open | — |
| A-19 | `see the ledgers` prints names without escaping them | ◇ | STATE.md (Not built) | open | — |
| A-20 | `-library` changes nothing | ◇ | STATE.md (Not built) | open | — |
| A-21 | The conformance map's index ignores build tags, so T2.2, T2.3, T11.10/enforced, T11.11/enforced and T13.5/covenant are met by tests that do not compile; the paths of T2.2 and T2.3 are not reached from `rokh daemon` (`unknown_op`) | ✔ (the reach ◇); `rokh/conformance/obligations.go:157-223` (`Index`) | STATE.md (Not built) | open | — |
| A-22 | Sixteen rows rest on one of eight constants nothing uses: `Measure`, `Witness`, `SeatOfJudgement`, `ClosedAndAlive` (`rokh/ledger/ledger.go:1187-1247`), `Complete`, `Custom`, `Position` (`rokh/arch/arch.go`), `generation.Profile` | ✔; `rokh/conformance/obligations.tsv` | STATE.md (Not built) | open | — |
| A-23 | A comment that belongs to `Event` stands above `FreshSize`, so T1.1, T5.1 and N4.3 point at `FreshSize` | ✔; `rokh/event/event.go:134-146` | STATE.md (Not built) | open | — |
| A-24 | T6.5 rests on `bond.Bound`, a type, while a grant names no ending event in advance (`event.Grant` has only revocation); T13.3 rests on `working.Folder`, which nothing in production uses; `selective` and most of `bond` have no caller in production | ✔ | STATE.md (Not built) | open; T6.5's ending event needs ruling (D-09) | — |
| A-25 | T4.1 rests on a search of the source that `weak.tsv` does not list | ✔; `rokh/arch/profile_test.go:199-214` (`TestOnlyOneWritingPathDrawsFreshness`) | STATE.md (Not built) | open | — |
| A-26 | A note of ten bytes writes three files, 576 KiB with slabs of 256 KiB, about 2 MiB with the default 1 MiB | ✔; measured again, M (§M1: 0.56 MiB and 2.06 MiB) | STATE.md (Not promised: whole slabs); study 0001, §M1 | open (whole slabs are by contract; the segments are S-03) | — |
| A-27 | Every commit writes every inventory segment, where contract 2.6, step 3, writes the changed ones | ✔; C, `rokh/vessel/tx.go:341, 389`; measured as S-03 | STATE.md (Not built); study 0001, §C1 | open | — |
| A-28 | The home keeps a catalog, a journal or an import's preview in one pointer record that must fit one slab: about 2,000 items, or 6,000 files previewed | ◇ | STATE.md (Not built) | open | — |
| A-29 | The comment on `BytesWithHash` says its memory does not grow with the file; it does, since the store assembles the whole object first | ◇, then C; `rokh-home/home/items.go:792-797`, `rokh-home/vesselstore/vesselstore.go:190-200` (`Store.Get`) | STATE.md (Not built) | open | — |
| A-30 | `rokh/build.sh` builds for neither Windows nor Android, and passes over a target that fails to build; the core cross-builds for both | ✔ (the cross-build ◇); `rokh/build.sh:41-68` | STATE.md (Not built) | open | — |
| A-31 | The prompt for a passphrase opens `/dev/tty` and runs `/bin/stty`: on Windows nothing is asked | C; `rokh/passphrase/passphrase.go:122, 151` | STATE.md (Not built) | open | — |
| A-32 | Tests go against AGENTS.md's rules: 19 sleeps in 14 test files; `TestReport` rewrites `conformance/STATE.md` in its package folder, as AGENTS.md itself says `go test ./conformance` does; tests open TCP on the loopback interface | ✔ (counted again for this study) | STATE.md (Not built) | open (the sleeps); needs ruling (D-01 for the report, D-03 for TCP) | — |
| A-33 | About 45 comments cite texts that are not in the tree (`goal 7.1`, `packet C.3`, `DEFECT K1` and the like) | C | STATE.md (Not built) | needs ruling (D-05) | — |
| A-34 | Names of products in code, against AGENTS.md: one engine's and its tools' in `rokh-home/native`, a sandbox program's in `rokh-home/gate`; the engine's settings are tied to that one engine, where contract B6 has a manifest replace it | ◇ | AGENTS.md, owed 9 (the engine) | open | — |
| A-35 | An offer of hosting with an end arms a timer in the gate; when it fires, the gate takes the hosting back, stops the programs and seals the engine's last state, at a moment nobody asked for | ◇, then C; `rokh-home/gate/server.go:89-100`, `rokh-home/gate/hosting.go:191` (`RevokeHosting`) | STATE.md (Not built) | needs ruling (D-11) | — |
| A-36 | A booth may listen on TCP at the loopback interface: the contract allows it (B1); N does not ("not one code path that opens a network socket", the requirement under Axiom 5; a door for programs on a Unix socket, the base profile of §4, which 4.10 changes only by a new generation); no ruling settles it | ✔; `rokh/transport/transport.go:52-60` (`Listen`) | STATE.md (Not built) | needs ruling (D-03) | — |
| A-37 | Contract R6 asks seeds that meet to reconcile without being asked; the owner's word of 2026-09-29, that nothing reconciles without being asked and R6 is not built, is only in a comment | ✔; `rokh/cmd/rokh/v1_vessel.go:255-258` | STATE.md (Not built); study 0001, §S4 | needs ruling (D-04) | — |
| A-38 | Contract B2 and B7: a full vessel is answered `storage_failed`, not `vessel_full`; a recording op refused with `hello_first` carries no `record` | ◇ | this study | open | — |
| A-39 | STATE.md called a live copy beyond three commits "Not promised, by design"; the contract calls it a gap, G1, red until closed, not a limit of nature (U3) | R | STATE.md (Not promised, with the contract's word beside it) | needs ruling (D-07) | 00980a9 (the contract's word set beside it) |
| A-40 | `docs/01` describes RKH1, four reserved verbs and freshness by an oracle; version 1 has RKH3, six reserved verbs and `fresh` in the head | ✔ | STATE.md (Not built) | open | — |
| A-41 | `docs/04` gives sizes of 231, 347 and 383 bytes as measured by `oracle/size_test.go`, which measures 311, 431 and 436 | ✔ | STATE.md (Not built) | open | — |
| A-42 | `docs/05` describes `rokh.json`, `.rokh/objects`, temporary files and content outside the carrier, none of which version 1 has, and cites `carrier/inventory_test.go` and `TestOrphanTempIsIgnored`, which do not exist | ✔ | STATE.md (Not built) | open | — |
| A-43 | `docs/07` says no code path opens TCP, sealing is not built, and only the root discloses; version 1 does all three otherwise | ✔ | STATE.md (Not built) | open; its sentence on TCP waits for D-03 | — |
| A-44 | `docs/08` describes `seat.json`, the library and a berth, and calls sealing absent on purpose; `seat.json` is still read though the contract replaced it | ✔ | STATE.md (Not built); AGENTS.md, owed 2 | open | — |
| A-45 | `docs/02` cites `proof/TestRevocationClosesTheFutureAndDoesNotUnsee`, which does not exist | ✔; `rokh/docs/02-authority.md:103` | STATE.md (Not built) | open | — |
| A-46 | The README's `rokh seed SRC DST [--size …]` fails ("give the new folder"); the flags go before the new folder | ✔ | STATE.md (Not built) | open | — |
| A-47 | The README calls `rokh-forms` an adapter on the booth; it runs the command line and writes what it is given, as plaintext, to a folder outside the carrier, which contract E5 says is no longer written | ✔; C, `rokh/cmd/rokh-forms/main.go:122-140` (`writeBlob`) | STATE.md (Not built) | open | — |
| A-48 | STATE.md named three skipped tests and called booth views and concurrent doors built, while their tests do not run (A-01) | ✔ | STATE.md (Known to fail; What is built) | fixed | 00980a9 |
| A-49 | The status clauses of T13 and N9.5 say sealing, bonds and a real folder are not built; the code builds them | R, C | this study | needs ruling (D-08) | — |

### 3.2 The scale measurements

| id | finding | evidence | written in | state | commit |
|---|---|---|---|---|---|
| S-01 | Loading a history recursed once per generation and ended the process with a stack overflow at a million events, 645,365 generations deep | M (a probe kept outside the tree), C | study 0001, §C7 | retested: `ledger/deep_test.go` loads 3,000 events under a stack of 512 KiB; §M4 loads a million | 4111458 |
| S-02 | The first measurement of revocations took grants back in the order they were made, so the ledger's pool shared the sets and showed about 2 KB a key; taken back newest first, revocations grow as the square, as grants do | M | study 0001, §M6 | retested: study 0001, `data/revokes.txt` | f459779 |
| S-03 | One note writes (1 + ⌈N/4096⌉) slabs, at least 1/4096 of the capacity, and takes about 5 µs for every slab of it: 256 MiB and 23 s at 1 TiB | M (§M1), C (§C1) | STATE.md (Not built); §M1, §C1, §X1 | open (= A-27) | — |
| S-04 | At a door a write, a status and a log grow with the history: a write in 25 ms at 1,000 events, 1.2 s at 300,000; 6.5 s of 7.0 s went to finding the branch references | M (§M5, §M12), C (§C2) | STATE.md (Not built); §M5, §M12, §C2 | open | — |
| S-05 | Grants, revocations and keyring changes take memory as the square of the keys named: 2 GiB for grants to 8,000 keys, as much again to revoke them; a lineage of 2,000 seeds holds 1.3 GiB | M (§M6, §M11), C (§C3) | STATE.md (Not built); §M6, §M11, §C3 | open | — |
| S-06 | A carrier records about 35 commits a second whatever the doors and writers; the turn goes to whoever tries first: among 128 writers on 32 doors, all with one key, 11 of 1,024 writes were refused after 15 s; no write answered `recorded` was lost | M (§M9), C (§C5) | STATE.md (Not built); §M9, §C5 | open | — |
| S-07 | A large thing takes four times its size in memory, 8 GiB for 2 GiB, and bringing it slows as it grows, 78 MiB/s to 26; contract E8 asks bounded memory, one chunk at a time | M (§M3), C (§C4; `rokh/content/vessel.go:37-48`, `Fetch`), R (E8) | STATE.md (Not built); §M3, §C4 | open | — |
| S-08 | Every opening verifies the whole history and holds it in memory: 86 µs an event in a bare ledger, 146 at a door, 210 for each command; a million events in 86 s and 5.4 GiB | M (§M4, §M5), C (§C8) | STATE.md (Not built); §M4, §C8 | open (the memory an event takes); needs ruling for an opening from a checkpoint (D-12) | — |
| S-09 | Version 1 fixes 32 readers to an envelope (the 33rd refused) and 32 slot cells to a vessel; 2^22 slabs of at most 2^26 bytes, 256 TiB; 4 KiB of payload; 16 parents. An address read by more than 32 keys cannot be written | C (§C6), M (§M7) | STATE.md (Not promised); §C6 | needs ruling (D-13, D-14) | — |
| S-10 | A bond's leaf lists every founder and each holds all of it: 67 bytes a founder, 67 MB at a million | M (§M10) | §M10 | needs ruling (D-15) | — |
| S-11 | A seed holds every system event before it: 3 + 4n events for the n-th of a line; a lineage's memory grows as the square, and some 7,000 seeds in a line cannot be opened on the host | M (§M11), X (§X4) | §M11, §X4 | open (the square, W-17); needs ruling for seeds that carry less (D-16) | — |
| S-12 | A reconcile reads both histories whole: 2.3 s at 100,000 events to move two records | M (§M5) | §M5, §X5 | open | — |
| S-13 | A writer cuts the reads of a carrier from about a thousand a second to 32 to 107 | M (§M9) | §M9 | open (the cause is S-04's) | — |
| S-14 | Not run: capacity over 16 GiB on a disk or over 1 TiB anywhere; content over 2 GiB; any booth session, socket, courier, gate or network; any person; solid-state, removable, FAT32 or exFAT media, synced folders, the chest; another host; more than one sample a size | R (§F) | study 0001, §F; STATE.md (Never run) | open: I-01, I-02, I-07, I-09 | — |
| S-15 | Which conditional skips fired in the suites was not recorded | R (§T) | §T, §F | open: W-03 counts skips | — |

### 3.3 Seeds by role

Rows 1 to 8 of study 0001, §S5, are meetings of the owner's
direction with the texts, written down for decision; none is a fault.

| id | the direction asks | evidence | written in | state | commit |
|---|---|---|---|---|---|
| R-01 | readers that differ by seed | R (contract 4.4) | study 0001, §S5, row 1 | needs ruling (D-13) | — |
| R-02 | a cost of the passphrase that differs by role | R (contract 2.3; T8.7) | study 0001, §S5, row 2 | needs ruling (D-10) | — |
| R-03 | a seed others can reach | R (N, the requirement under Axiom 5; T4.1) | study 0001, §S5, row 3 | needs ruling (D-03) | — |
| R-04 | an always-on seed that serves | R (N5.2; T13.2) | study 0001, §S5, row 4 | needs ruling (D-18) | — |
| R-05 | a serving seed that is a mirror | R (T13.4; N9.5; docs/07 §11) | study 0001, §S5, row 5 | needs ruling (D-08) | — |
| R-06 | a seed that keeps less over time | R (T3.7; T6.3) | study 0001, §S5, row 6 | needs ruling (D-19) | — |
| R-07 | synchronization that runs by itself | R (T4; AGENTS.md law 3; contract R6) | study 0001, §S5, row 7 | needs ruling (D-04) | — |
| R-08 | three named roles | R (AGENTS.md, Language) | study 0001, §S5, row 8 | needs ruling (D-20) | — |
| R-09 | making a cold seed from a warm source was not examined | R (§S2) | §S2 | open (I-06) | — |
| R-10 | how far R6 is built was said not examined; a comment says it is not built, by the owner's word | C (`rokh/cmd/rokh/v1_vessel.go:255-258`) | §S4, §F; STATE.md | fixed in the report; R6 itself needs ruling (D-04) | 00980a9 |

### 3.4 The three requirements

From §4. Only what is missing or unsettled is listed; what version 1 already
meets is in §4's tables.

| id | finding | evidence | written in | state | commit |
|---|---|---|---|---|---|
| Q-01 | Confidentiality cannot follow a seed's role: the readers of an address are a fold of the causal past, the same in every seed; a role chooses which bodies a seed holds, never who opens them | R (contract 4.4, 4.7 S3), C | §4.1; §S5 row 1 | needs ruling (D-13) | — |
| Q-02 | Closed seeds of one Rokh can be told to belong together: the salt and the cost are in the clear and the same in every seed | R (contract 2.3) | §4.1, §4.3; STATE.md (What is built) | needs ruling (D-10) | — |
| Q-03 | A seed served to people over a network is not in version 1: a booth listens on a Unix socket or on TCP at the loopback interface | C (`transport.Listen`), R (N) | §4.1; A-36 | needs ruling (D-03) | — |
| Q-04 | Keys by role are half there: cold custody exists per seed; `rokh key passwd` is not built; the root key cannot be rotated (T13.2 open); an old copy keeps its cells (U6); a cold seed from a warm one was not examined | R, C | §4.1 | open (W-13, I-06); needs ruling (D-18) | — |
| Q-05 | Backup in version 1 is a seed, or a copy of the folder made while no more than three commits start (U3, G1); nothing else is built | R | §4.1 | needs ruling (D-07) | — |
| Q-06 | Synchronization runs only when asked, reads both histories whole, and a repeated reconcile changes bytes; R6 is not built | M (§M5), C, R | §4.1; STATE.md | open (W-25, W-29); needs ruling (D-04) | — |
| Q-07 | On a solid-state disk every note writes at least two slabs and a head, and past 4,096 slabs 1/4096 of the capacity: slab size and capacity set the wear | M (§M1), C (§C1) | §4.1 | open (W-14; I-09 for wear itself) | — |
| Q-08 | Every opening, and the first answer of a door after every commit, reads, verifies and opens every pack, content included: a carrier holding 700 GB of files is read whole each time, some 45 minutes to two hours on this host | C (`(*Vessel).indexOnce`, `(*Tx).Commit`, `(*Server).behind`), M (§M3), X | §4.2; STATE.md (Not built) | open (W-15; I-01) | — |
| Q-09 | One note in a carrier of 1 TB writes 257 MiB with slabs of 1 MiB, or 320 MiB with slabs of 64 MiB, and takes about 7 s or 1.7 s in memory, twice that on a disk | X (§X1 from §M1, §M2) | §4.2 | open (W-14) | — |
| Q-10 | A library is brought a thing at a time, each held four times in memory, through the sentence surface or the home (the command line has no `bring`); filling 700 GB through a door reads the content again after every commit: tens of terabytes | M (§M3), C, X | §4.2 | open (W-19, W-15, W-28) | — |
| Q-11 | A petabyte is beyond the version 1 format, which ends at 256 TiB | C (§C6), X (§X1) | §4.2 | needs ruling (D-14) | — |
| Q-12 | People and programs with authority of their own are keys: grants take memory as the square of them; an address has at most 32 readers; 31 keys beside the owner's open a vessel with a passphrase; one commit at a time per carrier | M (§M6, §M7, §M9), C (§C6) | §4.2 | open (W-17, W-18); needs ruling (D-13) | — |
| Q-13 | Booth sessions, requests per session, and keys that act through a booth without a cell of their own were not measured; every writer measured used one key | R (§0, §F) | §4.2 | open (I-02, I-10) | — |
| Q-14 | An anchor proves a ledger, not a person (U9); a full-name collision is refused by a provisional guard while T13.8 is open; whether one root key may found two Rokhs was not examined | R, C | §4.3 | needs ruling (D-21); open (I-12) | — |
| Q-15 | A closed carrier yields its capacity, when and how much it was written (U1), the clear bytes of its heads, the bond between seeds (Q-02), and one guess tried on 32 cells (A-08); what a series of copies shows was not examined | R, C, M (§M7) | §4.3 | open (I-04, W-21); needs ruling (D-10) | — |
| Q-16 | Leakage in use: the host sees all while a carrier is open (U8, T12.6); `rokh-forms` writes plaintext outside the carrier (A-47); a revoked key keeps what it opened (U6); temporary files, swap and crash dumps were not examined | R, C | §4.3 | open (I-05) | — |
| Q-17 | Resource consumption: a booth parses 8 MiB before binding and takes any number of connections; one small write makes every door read all content again; grants cost memory as the square, also a delegate's; an open address keeps every key's events; a large thing takes four times its size; a local process can hold the turn; none of it was run as an attack | C, M (§M3, §M6, §M8, §M9) | §4.3 | open (W-12, W-15, W-17, W-19, I-03); needs ruling (D-22) | — |

## 4. The three requirements, set beside the code and the documents

### 4.1 One Rokh in seeds of different roles

The owner's direction names three roles: an archive kept offline on a hard
disk, a working seed on a solid-state disk, and a serving seed that people
reach over a network. Study 0001, §S, calls them mother, working
and frontier. These are the owner's words, not the documents' (R-08).

**Authenticity and authority are not confidentiality.** Who wrote an event,
where it stands and that it was not changed are proved by its signed head,
its id and its parents. Whether it was granted is judged in its causal past,
with its body. These are the same in every seed that holds those bytes, and no
role may change them. Who can read an event is a separate matter: the
envelope of its body names its readers, and the vessel is sealed to keys of
its own. A seed that holds only an ancestor's head, a slice, proves that
ancestor's author and lineage, not that it obeyed its grant (contract 3.3,
U7). Version 1 keeps the two matters apart, and so does this study.

**The same in every seed, by the contract (R, C):**

| what | why it cannot follow the role | where |
|---|---|---|
| the anchor, and that no event of another anchor is accepted | it names the Rokh | contract 4.7 S2; T8.4 |
| every event's bytes, id and signature | an event is its bytes | contract 3; T3.7 |
| every verdict | the same bytes in the same causal past are judged alike in every seed | T6.2; contract C4, C8 |
| the lineage: every system event and the head of every ancestor | a seed proves its place | contract 4.7 S2, S3 |
| the keyring, and the readers of an address at a point | a fold of the causal past | contract 4.4 |
| that only the root gives a seed | | contract 4.7 S1 |
| the salt and cost of the passphrase | one per Rokh (Q-02) | contract 2.3 |
| the byte forms, four heads, R = 3, 4 KiB of payload, nothing erased | | contract 1, 2; T3.4, T3.7, T6.3 |

**What follows the role, and what version 1 does with it:**

| concern | version 1 lets it differ by seed | the direction asks | gap | belongs to |
|---|---|---|---|---|
| which bodies a seed holds | yes: whole, or a slice of scopes (S3) | the serving seed holds what may be shown | none for holding less; a seed cannot later hold less (R-06) | a reference text (T3.7, T6.3) |
| who can open a body | no: the readers of the address, in every seed (Q-01) | encryption chosen by role | a role cannot seal otherwise | the v1 contract (4.4), or a new generation (D-13) |
| the vessel's own keys | yes: VK and its keys per seed (S2) | | none | |
| which keys open the seed with a passphrase | yes: its slot cells, 32 (4.6) | few on the serving seed, the owner's on the archive | at most 31 keys beside the owner's | the v1 contract (4.6) |
| where the root key lives | yes: in the owner's cell, or out of the vessel (`rokh init --cold FILE`, 4.6) | the root key with the archive, never on the serving seed | a cold seed from a warm source not examined (R-09) | |
| the cost of the passphrase | no: one per Rokh (A-08, Q-02) | higher on the archive, or a salt per seed | a new generation (2.3) | the v1 contract, D-10 |
| changing a passphrase; rotating keys | `rokh key rotate` for keys; no `rokh key passwd` (A-07); no rotation of the root key | per role | `passwd` owed; root rotation open | this implementation (A-07); a reference text (T13.2) |
| medium, slab, capacity, growth | yes (contract 1) | large on the archive, small slabs on the solid-state disk | every note writes whole slabs, and past 4,096 slabs 1/4096 of the capacity (Q-07) | this implementation (A-27), and whole slabs by contract |
| backup | a seed; or a copy of the folder while no more than three commits start (U3, G1); an old copy keeps its cells (U6); a lost key is not recovered (U11); what the host copies is outside Rokh (T8) | the archive as the backup of the others | G1 open (D-07) | the v1 contract |
| synchronization | `rokh reconcile`, when asked: the union, judged by each side, commutative (T5.4, 4.8) | the archive and the working seed kept close, the serving seed fed | R6 not built (A-37); a reconcile reads both histories whole (S-12) and changes bytes when repeated (AGENTS.md, owed 7) | this implementation (S-12); a ruling (D-04) |
| reach | a booth on a Unix socket, or TCP at the loopback interface (C6) | people reach the serving seed over a network | no reach beyond the host in Rokh; a courier carries bundles, keyless and untrusted (N4.9) | a reference text (N), D-03 |
| what may be shown | covenants, events at `peer`, read when a bundle is closed (docs/07) | the serving seed carries what its covenants permit | the runtime of an application's covenant is not built (T11.10, N9.5) | this implementation |

**By role, in version 1 as it stands:**

| | archive, offline, hard disk | working, solid-state disk | serving, reached over a network |
|---|---|---|---|
| can be made today | yes: a whole seed, fixed or growing, with the root key in its cell | yes: a whole seed or a slice; small slabs; the root key cold | a slice can be made; serving it beyond the host cannot |
| keys | the owner's cell; few others | the owner's working keys; delegates' cells | only keys meant to be served; at most 31 cells |
| what it risks | old cells in an old copy (U6); a host that sees it while open (U8) | wear (Q-07); a commit cut by power on a disk that ignores a flush (U4) | an always-on machine with the ledger and a key (N5.2, open); everything of §4.3 |
| what it needs first | D-07 (a copy beyond R commits), W-13 (`passwd`) | W-14 (write the changed segments), W-15 (keep the index) | D-03 (reach), D-18 (always-on), D-13 (readers by role), W-12 (bounds before a session) |

### 4.2 Real data and real use

**An event is not a carried file.** An event is a signed head of about 180
bytes and a body, sealed in an envelope to the readers of its address, with a
payload of at most 4 KiB (T3.4); one costs some 380 to 560 bytes stored
(§M4, §M5). A file larger than that is content: chunks of at most S − 4096
bytes, sealed to the readers of the event that describes it (contract E5),
kept as records in packs, and named by one `content.put` event. A library's
history counts its files; its vessel counts their bytes; its carrier's files
show its capacity whatever it holds (contract 1, U1). The command line writes
events only (`rokh write`, up to 4,096 bytes); content goes in through the
sentence surface's `bring` or through the home (STATE.md; AGENTS.md, owed 6).

**A carrier of 1 TB holding 700 GB of real files.** Nothing of this size was
run. What follows is read in the code (C) and extrapolated (X) from rates that
were measured (M). The format holds it: 1 TiB is 2^20 slabs of 1 MiB, or
16,384 of 64 MiB, within 2^22 slabs (§C6); `rokh init` takes `--size`,
`--slab` and `--growth`.

| step | what version 1 does | estimate | rests on |
|---|---|---|---|
| making a fixed carrier of 1 TiB | writes every slab with randomness | 1.7 to 3.2 hours | M: 91 to 167 MiB/s (§M2) |
| opening it | verifies the slabs of the last three generations, then rebuilds the index by reading, hashing and opening every pack, content included | some 45 minutes to two hours, and then 146 µs for each event of the history | C: `(*Vessel).indexOnce` (`rokh/vessel/vessel.go:1134-1152`), `(*Carrier).Refs` (`rokh/carrier/carrier.go:645-669`); M: a carrier holding 2,112 MiB opened again in 8.1 s, about 260 MiB/s, its files likely still in the host's memory (§M3); the disk was written at 91 to 173 MiB/s (§M2), and its reading was not measured |
| one note | writes the last pack, every segment and a head; drops the index | 257 MiB written and 515 MiB read with slabs of 1 MiB, about 7 s in memory; 320 MiB and 704 MiB with slabs of 64 MiB, about 1.7 s; about twice on a disk | X1 (§X1), from §M1 and §M2 |
| the first answer after that note, at any door | `behind` asks for the references before every answer (`rokh/daemon/daemon.go:534-550`); the index is gone, so it reads every pack again | the same 45 minutes to two hours | C, M as above |
| bringing 700 GB | one thing a recording, held four times in memory, so at most some 3.5 GiB a thing on this host | 2.4 to 7 hours of bringing; and, if each commit is followed by an answer, the content brought so far read again each time: in the fewest commits memory allows, about 190, some 65 TB of reading, days; a file a commit is far worse | M (§M3: 78 to 26 MiB/s; four times the thing), C |
| fetching one file | gathers the whole, four times its size in memory, after the index is rebuilt if a commit came before | 37 s for 2 GiB (§M3), plus the index | M, C |

So version 1 as built holds 700 GB in such a carrier but cannot be used as a
library of it. This is a limit of this implementation, not of the contract or
the texts. Writing only the changed segments and keeping the index across
commits (W-14, W-15; §P1.1, §P1.2) remove the two costs that grow with
capacity and content without changing a byte. Bringing in bounded memory
(W-19), which contract E8 already asks, removes the third. I-01 measures all
of it before and after.

**The path from megabytes to petabytes**, by capacity and by content:

| range | one note writes (M, X) | an opening reads (C, X) | first limit | removed by |
|---|---|---|---|---|
| MiB to 1 GiB | 0.56 to 2 MiB, milliseconds (M) | what it holds, milliseconds (M) | none met | |
| 1 to 64 GiB | 2 to 17 MiB, 27 ms to 1.3 s (M) | what it holds: seconds a GiB (M, §M3) | a large thing in memory, four times over (S-07) | W-19 |
| 64 GiB to 1 TiB | 16 to 256 MiB, 1.3 to 23 s in memory (M) | up to hours (X) | the segments (S-03); the index (Q-08) | W-14, W-15 |
| 1 to 256 TiB | 256 MiB to 64 GiB a note (X); making a fixed vessel takes hours to weeks (X) | hours to weeks (X) | the inventory read and written whole; a fixed vessel written whole when made (contract 1) | W-14, W-15; a growing vessel, which version 1 has |
| beyond 256 TiB, a petabyte | not expressible in version 1 (C, §C6) | | the format's end | a new generation (D-14, §P2.1) |

Content grows a library; history grows a ledger. A ledger of notes meets its
limits at some 10^5 events (a door's answers, S-04) and 10^6 to 10^7 events
(every opening verifies everything, S-08). A library meets them at tens of
gigabytes of content (Q-08, X). Neither limit is of the texts.

**People and programs with authority of their own.** In version 1 each acts
with a key. Its authority is a grant from the owner, or from a delegate who
may delegate and only narrows. An open grant, made by the root alone, lets any
key write at one address (contract 4.5). A program in the home has a key of
its own and sees that key's view only (contract B4, B6). The ledger knows keys,
not people (U9). Keys, requests and concurrent operations were measured
apart; sessions were not:

| what | measured to (M) | first limit seen | not measured |
|---|---|---|---|
| keys granted | 8,000 keys, 2 GiB (§M6) | memory as the square: some 20,000 keys fill this host (X) | grants made by delegates |
| keys at an open address | a million keys, 2.1 to 2.5 KB each (§M8) | none met; memory grows with every key, forever | |
| readers of an address | 32 reader keys in an envelope; the 33rd refused (§M7) | 32, the owner's generations among them | readers through a booth |
| keys that open a vessel by passphrase | read in the code: 32 cells (§C6) | 31 beside the owner's | filled to 32 |
| operations at a door | ten writes, statuses and logs at up to 300,000 events (§M5) | an answer grows with the history | requests over time, per key |
| writers at once | 128 writers on 32 doors, all with one key (§M9) | about 35 commits a second; unfair waits; refusals after 15 s | many keys writing at once |
| readers at once | 16 at one door, 4 × 4 at four (§M9) | a writer cuts them to 32 to 107 a second | |
| booth sessions | none | | everything (I-02) |

The table of study 0001, §X3, counts one key a person. A person
with k programs of their own is k + 1 keys, and meets the same limits sooner:
five people with three programs each are twenty keys, far from any limit; a
household of thirty with three each is 120 keys, more readers than one address
may have; a thousand with three each is 4,000 keys, half a gigabyte of
authority sets (§M6). With authority sets that share their structure (W-17;
§P1.4), a million keys granted would hold about a gigabyte, not 33 TB. Being
many is, by a reading the owner is to confirm (D-17), many Rokhs and the bonds
between them.

### 4.3 Practical security

**The anchor's uniqueness (R, C).** The anchor is the id of the genesis, the
SHA-256 of a head the root key signed with a fresh value. A ledger has one,
and refuses a second genesis (`rokh/ledger/ledger.go:599-603`;
`rokh/event/event.go:454`). Every seed carries the same anchor (contract 4.7
S2). A bond refuses two founders with one root (`bond.ErrSharedRoot`). Two
different byte strings under one name are refused by a guard that is
provisional while T13.8 is open (`ledger.ErrNameCollision`). An anchor proves
a ledger, not a person: anyone with a key can found as many as they like
(U9). Not examined: whether one root key may found two Rokhs, and what each
then sees of the other (I-12).

**What a closed carrier yields (R, C, M).** Without a passphrase: that it is
a vessel, its capacity (N and S, from its files), and when and how much it
was written, from file times and from which files change between two copies
(contract 1, U1). In the clear, the first 64 bytes of each head file: the
magic, the derivation's id, its iterations and its salt (contract 2.3). These
are the same in every seed of one Rokh, so two closed seeds show that they
belong together (Q-02). No name, address, count or content is in the clear;
slabs and cells are sealed, and a free slab is randomness (contract 2.2). One
guess costs one derivation, 128 ms on this host at 600,000 rounds, and is
tried on all 32 cells at once in 0.13 ms (§M7). The derivation asks no memory
(A-09), and nothing asks a passphrase to be strong (A-10). A cell rewritten
in a vessel stays as it was in every copy made before, so a key taken back
still opens such a copy with its old passphrase (contract 4.6, U6). Not examined: what a series of copies shows over time
(I-04).

**Leakage in use (R, C).** While a carrier is open, a compromised host sees
whatever is on the screen and in memory (U8, T12.6; N's table of who can do
what). `rokh-forms` writes what it is given, as plaintext, to a folder outside
the carrier (A-47). A key taken back keeps what it opened and, holding VK and
the system reader, sees the shape and system layer of any later copy of that
vessel it obtains; closing that needs a new vessel made by seed (U6). Not
examined: plaintext in temporary files, swap, crash dumps or a terminal's
history, and what a program in the home's enclosure can reach (I-05).

**Resistance to wasteful use of resources (C, M; none run as an attack).**

| who | what | cost to the carrier or the door | evidence | work |
|---|---|---|---|---|
| any local account that can connect | messages of up to 8 MiB parsed before a session is bound; any number of connections | memory; one run, about 2.3 GB for twelve connections | ◇, A-06 | W-12, I-03 |
| any key that may write anywhere, an open address included | one small write | every door reads every pack again at its next answer: the whole content (Q-08) | C, M (§M3) | W-15 |
| the owner, or a delegate who may delegate | many grants | memory as the square in every opening of every seed that holds them: 2 GiB at 8,000 | M (§M6), C | W-17, D-22 |
| any key, at an open address | many events, each from a key of its own | 2.1 to 2.5 KB each in memory, held forever (nothing is erased) | M (§M8) | D-22 |
| a key that may bring content | one large thing | four times its size in the door's memory | M (§M3) | W-19 |
| a local process that can open the folder | holds the lock on `rokh/head0.rkh` | every door refuses to write after 15 s (`turn_busy`) | C (§C5), M (§M9) | W-18, I-03 |
| a recording, in a growing carrier | content larger than the free slabs | the vessel grows by itself to its maximum | C (contract 1) | I-03 |

## 5. Measured, extrapolated, proposed

### 5.1 Measured (M)

On one host (§1 above; study 0001, §1), against the production code of
4111458, each size once unless said otherwise. The raw outputs are in study
0001, `data/`.

| what | number | raw output | benchmark |
|---|---|---|---|
| one note, 1 TiB of capacity, in memory | 256.3 MiB written, 23.2 s | `data/vessel.txt` | `BenchmarkScaleVessel` |
| one note, 16 GiB on the disk, slabs of 1 MiB | 163 ms | `data/vesseldisk.txt` | `BenchmarkScaleVesselDisk` |
| a thing of 2 GiB | brought in 80 s; opened again in 8.1 s; fetched in 37 s; 8.0 GiB at most | `data/content.txt` | `BenchmarkScaleContent` |
| a million events | loaded in 86 s; 5.4 GiB at most | `data/chain.txt` | `BenchmarkScaleChain` |
| a door at 300,000 events | opened in 43.8 s; a write 1,175 ms; a status 752 ms | `data/door.txt` | `BenchmarkScaleDoor` |
| where a door's time goes at 100,000 events | 6.5 s of 7.0 s finding the references | `data/door-profile.txt` | the processor profile |
| reconcile at 100,000 events | 2.3 s to move two records | `data/reconcile.txt` | `BenchmarkScaleReconcile` |
| grants to 8,000 keys; their revocation | 2,027 MiB; 2,026 MiB more | `data/authority.txt`, `data/revokes.txt` | `BenchmarkScaleAuthority` |
| a million keys at an open address | 2,447 B a key; 3.7 GiB at most | `data/openaddress.txt` | `BenchmarkScaleOpenAddress` |
| an envelope of 32 readers; of 33 | 2,540 B; refused | `data/envelope.txt` | `BenchmarkScaleEnvelope` |
| the passphrase; all 32 cells | 128 ms; 132 µs | `data/passphrase.txt` | `BenchmarkScalePassphrase` |
| 128 writers on 32 doors, one key | 36.7 writes a second; 11 of 1,024 refused (two runs within 15 per cent) | `data/writers2.txt`, `data/writers.txt` | `BenchmarkScaleWriters` |
| readers at once | to 1,064 reads a second; 32 to 107 with a writer | `data/readers.txt` | `BenchmarkScaleReaders` |
| a bond's leaf of a million founders | 67 MB | `data/leaf.txt` | `BenchmarkScaleLeaf` |
| seeds | 24 through the command line; 2,000 in one ledger, 1,295 MiB | `data/seeds.txt`, `data/seedledger.txt` | `BenchmarkScaleSeeds`, `BenchmarkScaleSeedLedger` |
| the suites | §T | `data/suite-rokh.txt`, `data/suite-home.txt` | `go test` |

### 5.2 Extrapolated (X)

None of it was run. Each estimate says what it rests on and how far the runs
reached.

| estimate | rests on | runs reached |
|---|---|---|
| a note at 256 TiB writes 64 GiB and reads 128 GiB | §M1; §C1 | 1 TiB |
| a petabyte is beyond the format | contract 1 | |
| a billion events: a day of one processor to open, 2 to 3 TB of memory | §M4 | 10^6 events |
| grants to some 20,000 keys fill this host; 33 TB for a million | §M6, fit 33·n² | 8,000 keys |
| the billionth seed of a line holds four billion events | §M11 | 2,000 seeds |
| a carrier holding 700 GB is read whole at each opening and after each commit: 45 minutes to two hours | C (§4.2); §M3, §M2 | 2 GiB of content |
| a note in a carrier of 1 TB writes 257 to 320 MiB and takes 1.7 to 7 s in memory | §X1, from §M1 | 1 TiB, thirteen notes |
| filling 700 GB through a door: 2.4 to 7 hours of bringing and some 65 TB read again | §M3; C | 2 GiB |
| people with programs: the limits of keys come k + 1 times sooner | §M6, §X3 | 8,000 keys |

### 5.3 Proposed (P)

| proposal | answers | kind | decision |
|---|---|---|---|
| §P1.1 write only the changed segments | S-03, Q-07, Q-09 | within version 1 | none: W-14 |
| §P1.2 keep the index across commits; find a branch by its last pointer | S-04, S-13, Q-08 | within version 1 | none: W-15 |
| §P1.3 extend the ledger's order; read a log from its end | S-04 | within version 1 | none: W-16 |
| §P1.4 authority sets that share their structure | S-05, S-11, Q-12 | within version 1 | none: W-17 |
| §P1.5 a fair turn; one commit for writers already waiting | S-06 | within version 1 | none: W-18 |
| §P1.6 content in bounded memory, as E8 asks | S-07, Q-10 | within version 1 | none: W-19 |
| §P2.1 a vessel beyond 256 TiB: an inventory as a tree, packs appended | S-09, Q-11, G1 | a new generation | D-14 |
| §P2.2 opening from a checkpoint the owner's key signed | S-08 | a new generation | D-12 |
| §P2.3 readers by a key for each address and period | S-09, Q-01 | a new generation | D-13 |
| §P2.4 a bond's leaf as a tree of its founders | S-10 | a new generation | D-15 |
| §P2.5 seeds that carry what they rest on | S-11 | a new generation | D-16 |
| §P3 many people as many Rokhs and the bonds between them | Q-12 | a reading of the texts | D-17 |
| a small bound on a message before a session is bound, and on connections; 8 MiB stays the bound of a session | A-06, Q-17 | within version 1 (B1 is "at most") | none: W-12 |
| a salt for each seed, or each cell | A-08, Q-02, Q-15 | a new generation (2.3) | D-10 |
| bounds on what one key may add at an open address, and on how many grants one delegate may make | Q-17 | changes what a key may do | D-22 |

## 6. The work these findings ask for

The plan this study was written with is kept as records, so that each item can
be taken up, claimed and closed on its own:

- **Missions and investigations**, `W-nn` and `I-nn`: in
  [`work/missions/`](../../../work/missions/), each with its priority,
  evidence, change, files, what it needs and the test that says it is done.
  W-00 is done (4111458, f459779). W-31, a check of the copies of the texts,
  was added while the texts were kept in a repository of their own, and
  withdrawn when they came back into `docs/`.
- **Questions for the owner**, `D-nn`: in [`lab/questions/`](../../questions/),
  each with what it blocks. D-23 to D-25 were added when Rokh was set out in
  four repositories, before the four became folders of one.
- **The order** proposed for them, and how they meet the owed list of the
  repository's AGENTS.md: in the README of [`work/`](../../../work/README.md).

When a mission is done or a question ruled, the state of its findings in §3
changes here, by a commit that says so.
