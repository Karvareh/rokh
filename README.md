# rokh-lab

**Lab: examined, not ruled.** Studies of Rokh as it is built and as its texts
describe it, measurements with their raw data, proposals, and the questions
that wait for the owner's ruling. Nothing here binds. A proposal here is not a
plan of source; a finding here is not a ruling; a number here holds for the
commit and the host it was measured on.

| repository | standing |
|---|---|
| [rokh](https://github.com/Karvareh/rokh) | source: what runs |
| [rokh-docs](https://github.com/Karvareh/rokh-docs) | docs: what is ruled |
| **rokh-lab**, this one | lab: examined, not ruled |
| [rokh-work](https://github.com/Karvareh/rokh-work) | work: to be examined or done |

## Studies

<!-- records:studies -->
| Study | What | Of | Status | Date |
|---|---|---|---|---|
| [0001](studies/0001-scale/) | How far one Rokh goes | rokh at 4111458, the release 1.0.0 with the repair of ledger.Load | examined | 2026-10-01 |
| [0002](studies/0002-findings-of-1.0.0/) | The findings of 1.0.0 | rokh 1.0.0 (ba7e695), and the branch of the scale study to b8f909a | examined | 2026-10-01 |
<!-- /records:studies -->

## Questions waiting for a ruling

<!-- records:questions -->
| Question | What is asked | Priority | Status | Blocks | Ruling |
|---|---|---|---|---|---|
| [D-01](questions/D-01-socket-folders-and-the-report.md) | May a test make its socket folder outside t.TempDir(), and may the conformance report rewrite its STATE.md? | p1 | open | W-01 | — |
| [D-02](questions/D-02-order-of-work.md) | Does the repair of the tests and the checks come before the first item of the owner's order? | p1 | open | — | — |
| [D-03](questions/D-03-tcp-and-reach.md) | TCP at the loopback interface, and how a seed is reached from outside the host | p1 | open | — | — |
| [D-04](questions/D-04-r6.md) | R6: the owner's word of 2026-09-29 written into the contract, or R6 built | p1 | open | — | — |
| [D-05](questions/D-05-cited-texts.md) | The texts the comments cite: brought into a repository, or the citations replaced | p2 | open | — | — |
| [D-06](questions/D-06-review-tests.md) | Which of the two review tests of rokh/key holds | p1 | open | — | — |
| [D-07](questions/D-07-live-copy.md) | A live copy beyond three commits: by design, or a gap to close | p2 | open | — | — |
| [D-08](questions/D-08-status-clauses-and-mirrors.md) | The status clauses of T13 and N9.5, and mirrors | p2 | open | — | — |
| [D-09](questions/D-09-grant-ending-event.md) | T6.5: a grant that names its ending event in advance | p2 | open | — | — |
| [D-10](questions/D-10-passphrase.md) | The passphrase: salt, cost, strength, and a derivation that asks memory | p2 | open | — | — |
| [D-11](questions/D-11-offer-end.md) | May the home act at an offer's end without being asked then? | p3 | open | — | — |
| [D-12](questions/D-12-checkpoint-opening.md) | May an opening rest on a checkpoint the owner signed? | p2 | open | — | — |
| [D-13](questions/D-13-readers.md) | Readers beyond 32, readers by seed, and a key for each address and period | p2 | open | — | — |
| [D-14](questions/D-14-vessel-beyond-256-tib.md) | A vessel beyond 256 TiB | p2 | open | — | — |
| [D-15](questions/D-15-leaf-as-a-tree.md) | A bond's leaf as a tree of its founders | p3 | open | — | — |
| [D-16](questions/D-16-seeds-that-carry-less.md) | Seeds that carry what they rest on | p3 | open | — | — |
| [D-17](questions/D-17-many-people.md) | Many people as many Rokhs and the bonds between them | p3 | open | — | — |
| [D-18](questions/D-18-always-on-and-root-rotation.md) | An always-on seed, and rotating the root key | p2 | open | — | — |
| [D-19](questions/D-19-forgetting.md) | May a seed keep less over time? | p3 | open | — | — |
| [D-20](questions/D-20-role-words.md) | Do the roles of seeds become words of the documents? | p3 | open | — | — |
| [D-21](questions/D-21-name-collision.md) | T13.8: what follows a full-name collision | p3 | open | — | — |
| [D-22](questions/D-22-bounds-per-key.md) | Bounds on what one key may add | p3 | open | — | — |
| [D-23](questions/D-23-license-of-texts.md) | Under what license are the texts, the studies and the missions? | p2 | open | — | — |
| [D-24](questions/D-24-conduct.md) | The code of conduct: its words, and where a report of conduct goes | p2 | open | — | — |
| [D-25](questions/D-25-who-merges.md) | Who merges, besides the owner? | p3 | open | — | — |
<!-- /records:questions -->

Each open question has an issue, where it is discussed; its record in
[`questions/`](questions/) is what it asks.

## How a study is written

A study examines one thing, and keeps its evidence beside it.

- **Every statement says its kind:** measured (M), read in the code (C),
  extrapolated (X) or proposed (P). A finding that was reproduced says so, and
  one that was not says that too.
- **Every measurement names its command, its host and the commit** of rokh it
  ran against, and its raw output is kept in the study's folder, with its
  SHA-256. A size that ran once says so.
- **What was not run is written down**, as plainly as what was.
- **A limit of a version is called a limit of that version.** Nothing found in
  an implementation is called a limit of Rokh unless the texts make it one.
- **A study proposes; it never rules.** What needs a ruling becomes a
  question.

A study has a folder of its own, `studies/NNNN-words/`, with its `README.md`
and its data; [the template](templates/study.md) shows its front matter.

## How a question reaches a ruling

1. It is written as `questions/D-nn-words.md`, from
   [the template](templates/question.md), or opened as an issue with the
   question form: what is asked, why, with the evidence, what changes once it
   is ruled, and what it blocks in rokh-work.
2. Anyone may examine it: on its issue, or by a study.
3. The owner rules, in rokh-docs: a record in `rulings/` that answers it.
4. Its record here becomes `ruled`, naming that ruling, and its issue closes.
   The missions it blocked in rokh-work no longer wait for it.

## Statuses

| record | status | means |
|---|---|---|
| study | `draft` | being written; not yet to be cited |
| | `examined` | its evidence is kept, and it says what it rests on |
| | `superseded` | a later study replaces it; it stays, and says which |
| question | `open` | waits for the owner |
| | `ruled` | answered by the ruling it names |
| | `withdrawn` | no longer asked; it says why |

## Check

```sh
cd tools/records && go vet ./... && go test ./... && go run . -root ../.. check
cd tools/records && go run . -root ../.. index    # writes the two tables above
```

The `issues` workflow keeps one issue for every open question, labelled
`needs-ruling`, and closes it when the question is ruled or withdrawn.

## License

Copyright (c) 2026 The Karvareh authors. Free under the GNU Lesser General
Public License, version 3 or later, as rokh: [LICENSE](LICENSE), with the GNU
General Public License it rests on in [GPL-3.0.txt](GPL-3.0.txt).
