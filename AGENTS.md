# Working in this repository

This file is for anyone who changes this repository, person or program. It
says where things are, what the law of the code is, how to check a change,
and where what is owed is kept. It does not repeat the texts it points to;
read them. `docs/`, `lab/` and `work/` each have an AGENTS.md of their own,
which adds to this one for what is there.

## The four folders

Rokh keeps four standings of what is said about it, one folder each:

| folder | standing | holds |
|---|---|---|
| [`source/`](source/README.md) | source: what runs | the code, its tests, its own documents, STATE.md |
| [`docs/`](docs/README.md) | docs: what is ruled | the treatise, the ledger without consensus, the contract, the register of rulings |
| [`lab/`](lab/README.md) | lab: examined, not ruled | studies, measurements, proposals, the questions that wait for a ruling |
| [`work/`](work/README.md) | work: to be examined or done | missions, each with the test that says it is done |

A study, a plan or a proposal does not enter `source/`: it goes to `lab/`, or
to `work/`, and the source points to it where it is evidence. A document stays
in `source/` when it must change in the same change as the code it describes.

## The map

```
README.md            what Rokh is, the treatise, and the four folders
AGENTS.md            this file; CLAUDE.md points to it
LICENSE              the GNU Lesser General Public License, version 3 or later; GPL-3.0.txt beside it
source/              what runs
  README.md          what Rokh is, in one page; build; check; where things are
  STATE.md           what is met, what is not, what was never run — keep it true
  CHANGELOG.md       what changed, release by release
  rokh/              the core module (Go, standard library only)
    docs/            grammar, authority, carrier, bandwidth, inventory, booth API, peering, sentences, screen
    conformance/     the code measured against T and N; obligations.tsv is the map, STATE.md is generated,
                     texts.tsv pins the version of the texts it is measured against
    <package>/       one layer each; rokh/README.md lists them, and arch/ tests that layers point downwards only
    cmd/             rokh, rokh-shell, rokh-courier, rokh-forms, rokh-chest
  rokh-home/         the home module: a home behind its gate, for programs
  go.work            names both modules
docs/                what is ruled
  رساله.md           the treatise (Persian): the founding text, cited as T
  without-consensus.md   the ledger without consensus (English rendering), cited as N
  conformance.md     how the two texts are read and cited
  contracts/v1.md    the contract of version 1: bytes, vessel, envelope, keyring, seed, booth
  rulings/           the register of the owner's rulings
lab/                 examined, not ruled: studies/, and questions/ for the owner (D-nn)
work/                to be examined or done: missions/ (W-nn, I-nn), and the owner's order
.github/             the checks, the issue forms and labels, CONTRIBUTING.md, SECURITY.md,
                     and records/, the program that checks the records of docs/, lab/ and work/
```

## The law

1. **The two texts bind.** The treatise (T) and the ledger without consensus
   (N) are the specification; `conformance.md` says how their propositions are
   read and cited. They are kept in `docs/`, and the source is measured
   against them at the SHA-256 that `source/rokh/conformance/texts.tsv`
   names. A design ruling in them is binding: the code implements it or the
   obligation is owed. Nothing in the code settles an open ruling quietly; an
   open ruling stays open until the owner rules, and a ruling is recorded in
   `docs/rulings/`.
2. **The contract binds the bytes.** `docs/contracts/v1.md` fixes the byte
   forms, the vessel, the envelope, the keyring, the seed and the booth
   protocol. A change of a byte form is a new generation with its own name,
   never an edit of the old one.
3. **The core never acts on its own.** No timer, no watcher, no poller, no
   background goroutine that writes. An event exists only because a person or
   a program asked for it through a door.
4. **The core knows no clock, no lock, no permission bit, no symlink flag,
   no atomic rename.** Time is testimony from a witness; the host adapters
   (`medium`, `turn`, `transport`) hold what touches the machine.
5. **Nothing is erased.** Correction, revocation, merge, refusal: later
   events that answer earlier ones. History only grows.
6. **Authority is judged in the causal past of an event**, never against
   the whole ledger; verdicts are final and acceptance is monotonic.
7. **Zero external modules, `CGO_ENABLED=0`, no hand-written primitive.**
   SHA-256, HMAC, HKDF, PBKDF2, AES-256-GCM, Ed25519 and X25519, all from
   Go's standard library.

## Language

- Code, identifiers, comments, documents, commit messages and pull requests
  are in English. The one Persian file is the treatise; it is the original
  and is not translated in this tree.
- A person is a *person*, never a *user*; the ledger is *an individual's
  event ledger*, never a *personal* one. Use the words the documents use:
  event, address, addressable, ledger, graph, lineage, concurrent, carrier,
  vessel, seal, envelope, key, view, grant, revoke, seed, reconcile, booth,
  gate, home, courier, receipt, covenant, bond, attestation, application.
  Do not coin a new word where one of these serves.
- Persian words in Latin letters are refused by a test (`shell` and `tui`
  have a language guard). What a person writes into a sentence, an address,
  a payload, is their content and is never folded to the surface's language.
- No name of a person, machine, network address, home directory, product,
  organisation or tool goes into code, comments, tests or documents. A test
  key is `clerk`, a test address is `home/journal`, a machine is *the host*.

## Build and check

```sh
cd source/rokh      && go vet ./... && go test -timeout 45m ./...
cd source/rokh-home && go vet ./... && go test ./...
cd source/rokh      && go test ./conformance      # rewrites conformance/STATE.md
cd .github/records  && go vet ./... && go test ./... && go run . -root ../.. check
```

- `gofmt` every Go file. `go vet` clean. No new dependency, ever.
- A change to a ruling's production path or its test must keep
  `conformance/obligations.tsv` true: the row names the path and the test,
  and the test's own comment says which ruling it is for.
- Three tests are skipped because they fail; `source/STATE.md` names them.
  Do not add a fourth skip; repair or leave the failure visible and recorded.
- The texts in `docs/` change only by a ruling, and a test of the source fails
  while a text differs from the SHA-256 that `texts.tsv` names. A new version
  of the texts is taken up whole, in one change: the texts with the ruling
  that allows them, their pin, and the obligations and code they ask for.
- A test never opens the network, never sleeps for time to pass, never
  writes outside `t.TempDir()`.

## What is owed

What is owed is kept in [`work/`](work/README.md): the owner's order of work,
item by item, as this file held it until it moved there, and the missions that
carry each item, every one with the test that says it is done. A mission that
lands in `source/` strikes its line from `source/STATE.md` in the same change
as the work.

## How a change is made

- One branch per task, from `main`, named for the task. Small commits,
  each one buildable and tested, each with a message that says what changed
  and why, in the present tense, in English. No trailer names a tool.
- A pull request carries: what changed, why, how it was checked, and the
  `source/STATE.md` line it strikes or adds; a pull request that carries out a mission
  names it (`W-13`). The checks in `.github/workflows` must pass. The owner of
  the repository merges; nobody else does.
- History is never rewritten. No force push, no amend of a pushed commit,
  no rebase of `main`.
- A change is done when: it builds on Linux and macOS, `go vet` is clean,
  the suites pass with no new skip, the conformance map still holds,
  `source/STATE.md` tells the truth about the source, the records check
  passes, and no name, address or path that is not the code's own entered the
  tree.
