# Contributing

Rokh has one owner who decides what enters `main`. Everyone else proposes.

## How to propose

1. Fork the repository and make a branch for one task.
2. Read [AGENTS.md](AGENTS.md): the map, the law, the language, and what is
   owed. A change that settles an open ruling of the treatise is not a
   proposal; it needs the owner's ruling first. Open an issue and ask.
3. Build and check both modules; `gofmt` and `go vet` clean; no new
   dependency; no new skipped test.
4. Keep [STATE.md](STATE.md) true: strike what you finished, add what you
   found undone.
5. Open a pull request that says what changed, why, and how it was checked.
   The checks must pass. Commits in English, present tense, one concern per
   commit.

## What is not accepted

- A change of a byte form that edits an old generation instead of naming a
  new one.
- Anything that makes the core act on its own: a timer, a watcher, a poller,
  a goroutine that writes.
- A dependency outside Go's standard library.
- A name of a person, a machine, a network address, a home directory or a
  product in code, comments, tests or documents.
- A rewrite of history.

## Reporting a problem

Open an issue with what you did, what you expected, what happened, and the
version (`rokh version`). For a problem that touches what is sealed, use the
private report described in [SECURITY.md](SECURITY.md) instead of a public
issue.
