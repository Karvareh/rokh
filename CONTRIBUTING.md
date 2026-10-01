# Contributing

Rokh has one owner who decides what enters `main`. Everyone else proposes.

This repository is the source: what runs. A question for the owner's ruling
is asked in [rokh-lab](https://github.com/Karvareh/rokh-lab); work to pick up,
each piece with the test that says it is done, is in
[rokh-work](https://github.com/Karvareh/rokh-work); the texts themselves are
kept in [rokh-docs](https://github.com/Karvareh/rokh-docs).

## How to propose

1. Fork the repository and make a branch for one task.
2. Read [AGENTS.md](AGENTS.md): the map, the law and the language. What is
   owed is kept in rokh-work, as missions in the owner's order. A change that
   settles an open ruling of the treatise is not a proposal; it needs the
   owner's ruling first: ask it in rokh-lab, as a question.
3. Build and check both modules; `gofmt` and `go vet` clean; no new
   dependency; no new skipped test.
4. Keep [STATE.md](STATE.md) true: strike what you finished, add what you
   found undone.
5. Open a pull request that says what changed, why, and how it was checked,
   and names the mission it carries out, if any. The checks must pass.
   Commits in English, present tense, one concern per commit.

## What is not accepted

- A change of a byte form that edits an old generation instead of naming a
  new one.
- Anything that makes the core act on its own: a timer, a watcher, a poller,
  a goroutine that writes.
- A dependency outside Go's standard library.
- A name of a person, a machine, a network address, a home directory or a
  product in code, comments, tests or documents.
- A rewrite of history.
- An edit of a copy in `rokh/texts/`. The texts change in rokh-docs, by a
  ruling, and come here whole.
- A study, a plan or a proposal. Those belong to rokh-lab and rokh-work.

## Reporting a problem

Open an issue with the defect form: what you did, what you expected and where
that is said, what happened, and the version (`rokh version`). For a problem
that touches what is sealed, use the private report described in
[SECURITY.md](SECURITY.md) instead of a public issue.
