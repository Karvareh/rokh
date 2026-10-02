# rokh-work

**Work: to be examined or done.** Missions that anyone, a person or a program,
can pick up. Each says what to change or find out, why, where, what it waits
for, and the test that says it is done.

| repository | standing |
|---|---|
| [rokh](https://github.com/Karvareh/rokh) | source: what runs |
| [rokh-docs](https://github.com/Karvareh/rokh-docs) | docs: what is ruled |
| [rokh-lab](https://github.com/Karvareh/rokh-lab) | lab: examined, not ruled |
| **rokh-work**, this one | work: to be examined or done |

A mission (`W-nn`) changes something: the code, a document, the checks. An
investigation (`I-nn`) finds something out, and what it finds becomes a study
of rokh-lab. Most of these were set from the findings of 1.0.0, in rokh-lab,
study 0002; a question of rokh-lab that a mission waits for is named `D-nn`.

## Missions

<!-- records:missions -->
| Mission | What | Priority | Status | Needs | Lands in |
|---|---|---|---|---|---|
| [W-00](missions/W-00-load-without-recursion.md) | Load a long history without a call per generation, and measure revocations newest first | p1 | done | — | source |
| [W-01](missions/W-01-short-socket-folder.md) | One short socket folder for every test that listens | p1 | blocked | D-01 | source |
| [W-02](missions/W-02-cut-without-permission-bits.md) | Cut a seed in two tests without permission bits | p1 | ready | — | source |
| [W-03](missions/W-03-checks.md) | Checks that see formatting, other systems, skips and changed files | p1 | blocked | W-01 | source |
| [W-04](missions/W-04-legacy-tests.md) | The twelve legacy test files in version 1, beginning with an idle daemon | p1 | blocked | W-01 | source |
| [W-05](missions/W-05-index-honours-build-tags.md) | A conformance index that honours build constraints | p1 | blocked | W-04 | source |
| [W-06](missions/W-06-rows-on-reached-paths.md) | Conformance rows that rest on reached paths | p2 | blocked | W-05 | source |
| [W-07](missions/W-07-courier-on-booth-1.md) | A courier that opens a session and says when it is refused | p2 | blocked | W-01 | source |
| [W-08](missions/W-08-bind-and-bound.md) | rokh bind and rokh bound on rokh.booth/1 | p2 | blocked | W-01 | source |
| [W-09](missions/W-09-booth-answers.md) | The booth's answers as the contract names them | p2 | ready | — | source |
| [W-10](missions/W-10-sentence-surface-defects.md) | Six defects of the sentence surface | p2 | ready | — | source |
| [W-11](missions/W-11-untested-commands.md) | Tests for the commands and the adapter that have none | p2 | blocked | W-01 | source |
| [W-12](missions/W-12-bounds-before-binding.md) | Bounds on what a booth reads before a session is bound | p2 | blocked | W-01 | source |
| [W-13](missions/W-13-key-passwd.md) | rokh key passwd | p1 | ready | — | source |
| [W-14](missions/W-14-changed-segments.md) | Write only the changed inventory segments | p2 | blocked | W-03 | source |
| [W-15](missions/W-15-keep-the-index.md) | Keep the vessel's index across commits | p2 | blocked | I-01 | source |
| [W-16](missions/W-16-ledger-order.md) | Extend the ledger's order, and read a log from its end | p2 | ready | — | source |
| [W-17](missions/W-17-shared-authority-sets.md) | Authority sets that share their structure | p2 | ready | — | source |
| [W-18](missions/W-18-fair-turn.md) | A fair turn, and one commit for the writers waiting | p2 | ready | — | source |
| [W-19](missions/W-19-bounded-content.md) | Content in bounded memory, both ways | p1 | ready | — | source |
| [W-20](missions/W-20-true-documents.md) | The documents of rokh made true | p2 | ready | — | source |
| [W-21](missions/W-21-cryptography-document.md) | The cryptography document of version 1 | p2 | blocked | W-20 | source |
| [W-22](missions/W-22-windows-prompt-and-builds.md) | A passphrase prompt on Windows, and builds that stop at a failure | p3 | blocked | W-03 | source |
| [W-23](missions/W-23-home-records-past-a-slab.md) | The home's records past one slab | p3 | blocked | I-08 | source |
| [W-24](missions/W-24-engine-by-manifest.md) | No product's name in the code; the engine by its manifest | p3 | ready | — | source |
| [W-25](missions/W-25-reconcile-what-differs.md) | A reconcile that moves only what differs | p3 | blocked | W-15 | source |
| [W-26](missions/W-26-delegate-test.md) | The delegate test, for the keys of version 1 | p1 | ready | — | source |
| [W-27](missions/W-27-english-of-the-surface.md) | The English of the sentence surface | p2 | blocked | W-10 | source |
| [W-28](missions/W-28-bring-on-the-command-line.md) | bring on the command line | p2 | blocked | W-19 | source |
| [W-29](missions/W-29-growing-seeds.md) | Seeds that grow, and a reconcile that changes no byte when nothing changed | p3 | ready | — | source |
| [W-30](missions/W-30-booth-content-keyring-seeding.md) | The booth's content, keyring and seeding | p3 | blocked | W-01, W-19 | source |
| [W-31](missions/W-31-check-the-pinned-texts.md) | Check the copies of the texts against rokh-docs in the checks | p2 | ready | — | source |
| [I-01](missions/I-01-measure-a-library.md) | Measure a library | p1 | ready | — | lab |
| [I-02](missions/I-02-sessions-at-scale.md) | Booth sessions at scale | p2 | blocked | W-01, W-12 | lab |
| [I-03](missions/I-03-wasteful-use.md) | Wasteful use of resources, as probes | p2 | ready | — | lab |
| [I-04](missions/I-04-closed-copies-over-time.md) | What closed copies show over time | p2 | ready | — | lab |
| [I-05](missions/I-05-plaintext-in-use.md) | Plaintext left behind in use | p3 | ready | — | lab |
| [I-06](missions/I-06-cold-seed-from-warm.md) | A cold seed from a warm source | p2 | blocked | W-13 | lab |
| [I-07](missions/I-07-spread-and-branches.md) | Spread, branches and merges in the measurements | p2 | ready | — | lab |
| [I-08](missions/I-08-home-one-slab-records.md) | The home's one-slab records, reproduced | p3 | ready | — | lab |
| [I-09](missions/I-09-field-results.md) | Field results | p3 | blocked | W-03, W-22 | lab |
| [I-10](missions/I-10-people-and-programs.md) | People and programs, each with keys of their own | p2 | ready | — | lab |
| [I-11](missions/I-11-why-the-key-test-fails.md) | Why the key test fails | p3 | ready | — | lab |
| [I-12](missions/I-12-one-root-two-rokhs.md) | One root key founding two Rokhs | p3 | ready | — | lab |
<!-- /records:missions -->

## How to pick one up

1. **Choose** a mission whose status is `ready`: everything it needs is done,
   and no ruling it waits for is open. Its issue carries the label `ready`.
2. **Claim it** on its issue: say that you take it, and when you expect to
   open a pull request. One claim at a time for a mission. A claim with no word
   for thirty days lapses, and anyone may then ask to take the mission over.
3. **Read** this repository's [AGENTS.md](AGENTS.md), and the AGENTS.md of
   the repository the mission lands in.
4. **Do it there**, in a branch of its own named for the mission
   (`w-13-key-passwd`): small commits, each buildable and tested, the checks of
   that repository passing.
5. **Open a pull request there**, as a draft until the mission's test passes.
   It names the mission (`W-13`), links its issue, and says how the test was
   run and what it gave.
6. **When the owner merges it**, the mission's record here becomes `done`,
   naming the commit, by a pull request here; whoever did the work may open
   it. The mission's issue then closes by itself.

If a mission meets an open ruling, a test that cannot be run as written, or a
fault it does not name, stop and say so on its issue; if a ruling is needed,
write a question in rokh-lab. Do not decide it.

## For programs

Every mission is written to be carried out from its record and the
repository it lands in. A program that takes one claims it as a person does,
says that it is a program, and works on a person's behalf, who answers for
what it opens. It never rules, never widens a mission, never merges and never
marks a mission done itself. It opens draft pull requests only, with the
output of the mission's test in the description, and it stops at the first
thing the record does not settle and asks on the issue.

The label `agent-ready` marks the missions whose test can be run by a program
from the command line alone.

## Statuses

| status | means |
|---|---|
| `ready` | can be taken up now |
| `blocked` | waits for a mission or a ruling, named in `needs` |
| `done` | merged; the record names the commit |
| `withdrawn` | no longer asked; the record says why |

Who has claimed a mission, and whether a pull request is open for it, is said
on its issue, with the labels `claimed` and `in-review`. The check refuses a
mission marked `ready` that waits for something, and one marked `blocked` that
waits for nothing.

## The owner's order

This is the owner's own list of what is owed, in the owner's order. Until Rokh
was set out in four repositories it was the section *What is owed, in order*
of rokh's AGENTS.md; it is kept here as the owner wrote it there (rokh,
ba7e695), unchanged to 45306df, and its history to then is in rokh. Whether the repair of the tests
and the checks comes before its first item is question D-02 of rokh-lab.

1. The three skipped tests: rewrite the delegate test for version 1 keys;
   obtain the ruling the two review tests contradict on, then keep one; make
   the home stream a large attachment in bounded memory.
2. The English of the sentence surface. The eight verbs were carried over
   from Persian one word at a time, and some sentences do not read as
   English: `go` closes the ledger where a person would say `leave`; `see
   the ledgers`, `bring the returned`, `carry the ledger` are the same.
   Rewrite every sentence a person types or reads until it reads as plain
   English, keeping the eight meanings, and change `docs/08-sentences.md`,
   the vectors in `rokh/shell/testdata/sentences.json`, the golden screens
   (`go test ./tui -update`) and the screen's header line together.
3. `rokh/docs/06-api.md`: rewrite against `rokh.booth/1` as built
   (`rokh/booth`, `rokh/daemon`, `rokh-home/gate`); then remove its note.
4. A cryptography document for version 1: what is sealed and what is not,
   every sentence with a `path:Symbol` that exists.
5. `rokh key passwd`.
6. `bring` in the command line: a file above 4,096 bytes through
   `rokh write`.
7. Seeds that grow; a reconcile that changes no byte when nothing changed.
8. The booth: `content.put`, `content.get`, the keyring, seeding between
   two booths.
9. The engine out of `rokh-home/native` into a module of its own; then the
   home on Windows.
10. The twelve test files behind build tags into a run that exercises them.
11. Runs on Windows and Android; exFAT and a synced folder; a real engine;
    vessels of hundreds of megabytes. These are field results, not code.

Every item, when done, is struck from rokh's STATE.md in the same change. The
missions that carry each item:

| item | missions |
|---|---|
| 1 | W-26; W-19; the second waits for D-06 |
| 2 | W-27, after W-10 |
| 3 | W-20 |
| 4 | W-21 |
| 5 | W-13 |
| 6 | W-28 |
| 7 | W-29 |
| 8 | W-30 |
| 9 | W-24, I-09 |
| 10 | W-04 |
| 11 | I-09 |

## A proposed order

For the owner to confirm (rokh-lab, D-02). The owner's order, above, stands
until then.

1. The rulings that unblock the rest: D-01, D-02, D-03, D-04, D-06.
2. The ground every later mission stands on: W-01, W-02, W-03, W-04, W-05,
   W-06.
3. Owed, and needing no ruling: W-13, W-26, W-19, then W-07 to W-12.
4. Scale within version 1, each measured before and after with the benchmark
   it names: I-01, then W-14, W-15, W-16, W-17, W-18, W-25.
5. Documents, once the code they describe stops moving: W-20, W-21, W-27.
6. The rest of the missions and the investigations: W-22 to W-24, W-28 to
   W-31, I-02 to I-12.
7. New generations, only after their rulings: D-10, D-12 to D-16.

## Proposing a mission

Open an issue with the mission form, or a pull request with a new record made
from [the template](templates/mission.md). A mission says why, with its
evidence; the change; where; what it needs; and the test that says it is done.
A mission that would settle an open ruling is not a mission yet: it is a
question for rokh-lab.

## Check

```sh
cd tools/records && go vet ./... && go test ./... && go run . -root ../.. check
cd tools/records && go run . -root ../.. index    # writes the table of missions
```

The `issues` workflow keeps one issue for every mission that is not done or
withdrawn, with its title and its labels.

## License

Copyright (c) 2026 The Karvareh authors. Free under the GNU Lesser General
Public License, version 3 or later, as rokh: [LICENSE](LICENSE), with the GNU
General Public License it rests on in [GPL-3.0.txt](GPL-3.0.txt). Whether the
missions stay under it is question
[D-23](https://github.com/Karvareh/rokh-lab/blob/main/questions/D-23-license-of-texts.md)
of rokh-lab, and it stays open until the owner rules.
