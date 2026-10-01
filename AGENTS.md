# Working in this tree

This file is for anyone who changes this repository, person or program. It
says where things are, what the law of the code is, how to check a change,
and what is owed. It does not repeat the texts it points to; read them.

## The map

```
README.md          what Rokh is, in one page; build; check; where things are
STATE.md           what is met, what is not, what was never run — keep it true
WORKPLAN.md        every finding of the audit and the scale measurements, and the work they ask for
rokh/              the core module (Go, standard library only)
  texts/           copies of the texts of rokh-docs, pinned by SHA-256 in texts.tsv; never edited here
    رساله.md       the treatise (Persian): the founding text, cited as T
    without-consensus.md   the ledger without consensus (English rendering), cited as N
    conformance.md how the two texts are read and cited
    contracts/v1.md  the contract of version 1: bytes, vessel, envelope, keyring, seed, booth
  docs/            grammar, authority, carrier, bandwidth, inventory, booth API, peering, sentences, screen, scale
  conformance/     the code measured against T and N; obligations.tsv is the map, STATE.md is generated
  <package>/       one layer each; rokh/README.md lists them, and arch/ tests that layers point downwards only
  cmd/             rokh, rokh-shell, rokh-courier, rokh-forms, rokh-chest
rokh-home/         the home module: a home behind its gate, for programs
go.work            names both modules
```

## The law

1. **The two texts bind.** The treatise (T) and the ledger without consensus
   (N) are the specification; `conformance.md` says how their propositions are
   read and cited. They are kept in rokh-docs, and this tree is measured
   against their copies in `rokh/texts/`, at the SHA-256 that
   `rokh/texts/texts.tsv` names. A design ruling in them is binding: the code
   implements it or the obligation is owed. Nothing in the code settles an
   open ruling quietly; an open ruling stays open until the owner rules, and a
   ruling is recorded in rokh-docs.
2. **The contract binds the bytes.** `contracts/v1.md` fixes the byte forms,
   the vessel, the envelope, the keyring, the seed and the booth protocol. A
   change of a byte form is a new generation with its own name, never an edit
   of the old one.
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
cd rokh      && go vet ./... && go test -timeout 45m ./...
cd rokh-home && go vet ./... && go test ./...
cd rokh      && go test ./conformance      # rewrites conformance/STATE.md
```

- `gofmt` every Go file. `go vet` clean. No new dependency, ever.
- A change to a ruling's production path or its test must keep
  `conformance/obligations.tsv` true: the row names the path and the test,
  and the test's own comment says which ruling it is for.
- Three tests are skipped because they fail; STATE.md names them. Do not
  add a fourth skip; repair or leave the failure visible and recorded.
- The copies in `rokh/texts/` are never edited here; a test refuses a copy
  that differs from its pin. A new version of the texts is taken up from
  rokh-docs whole, by a mission: the copies, `texts.tsv`, and the obligations
  and code it asks for, in one change.
- A test never opens the network, never sleeps for time to pass, never
  writes outside `t.TempDir()`.

## What is owed, in order

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

Every item, when done, is struck from STATE.md in the same change.

## How a change is made

- One branch per task, from `main`, named for the task. Small commits,
  each one buildable and tested, each with a message that says what changed
  and why, in the present tense, in English. No trailer names a tool.
- A pull request carries: what changed, why, how it was checked, and the
  STATE.md line it strikes or adds. The checks in `.github/workflows` must
  pass. The owner of the repository merges; nobody else does.
- History is never rewritten. No force push, no amend of a pushed commit,
  no rebase of `main`.
- A change is done when: it builds on Linux and macOS, `go vet` is clean,
  the suites pass with no new skip, the conformance map still holds, STATE.md
  tells the truth about the tree, and no name, address or path that is not
  the code's own entered the tree.
