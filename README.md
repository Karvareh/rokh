# Rokh

An individual's event ledger, built as a graph of addressable data that grows
across independent copies.

Rokh is not a service, not an account, not a network and not a judge. It
records what was done, by whom, under what right, after what. Whether it was
true, or wise, is not its question.

## [Read the treatise →](docs/رساله.md)

**[رساله](docs/رساله.md)**, the treatise: the founding text of Rokh, in
Persian, and the original. With the ledger without consensus it specifies
Rokh. Beside it in `docs/` are [the ledger without
consensus](docs/without-consensus.md), an English rendering of its Persian
original, and [the contract of version 1](docs/contracts/v1.md).

## The four folders

Each folder holds one standing of what is said about Rokh, so that nobody has
to ask whether a sentence is a ruling, a finding or a wish.

| folder | standing | holds |
|---|---|---|
| [`source/`](source/README.md) | what runs | the code, its tests, its own documents, and [STATE.md](source/STATE.md): what is met, what is not, what was never run |
| [`docs/`](docs/README.md) | what is ruled | the treatise, the ledger without consensus, the contract, and the register of rulings |
| [`lab/`](lab/README.md) | examined, not ruled | studies with their evidence and raw data, and the questions that wait for the owner's ruling |
| [`work/`](work/README.md) | to be examined or done | missions anyone, a person or a program, can pick up, each with the test that says it is done |

A thing moves forward only by the act that defines its next standing: work is
examined into the lab, the lab's questions are ruled into the docs, and the
docs are built into the source; where the source and the docs differ, the
difference is work. Only the owner rules and merges.

## Where to begin

- **To run it:** [`source/`](source/README.md): build, check, and the screen
  without a ledger.
- **To help:** a mission of [`work/`](work/README.md) whose status is `ready`.
- **To ask the owner:** a question of [`lab/`](lab/README.md), with the
  question form.
- **To propose a change:** [CONTRIBUTING.md](.github/CONTRIBUTING.md). Anyone
  who changes this repository, person or program, reads
  [AGENTS.md](AGENTS.md) first.
- **A weakness in security** is reported privately, as [the security
  policy](.github/SECURITY.md) says; never in an issue.

## License

Copyright (c) 2026 The Karvareh authors. Rokh is free software under the GNU
Lesser General Public License, version 3 or later: [LICENSE](LICENSE), with
the GNU General Public License it rests on in [GPL-3.0.txt](GPL-3.0.txt).
Whether the texts, the studies and the missions stay under it is question
[D-23](lab/questions/D-23-license-of-texts.md), open until the owner rules.
