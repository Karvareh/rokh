# Contributing

Rokh has one owner who decides what enters `main`. Everyone else proposes.

This repository keeps Rokh's four standings, a folder each: what runs in
[`source/`](../source/README.md); what is ruled in [`docs/`](../docs/README.md);
what is examined and not ruled in [`lab/`](../lab/README.md); and what is to be
examined or done in [`work/`](../work/README.md).

## Where to bring what

| you have | bring it as |
|---|---|
| a defect: the code does what the texts or the contract say it must not, or fails | an issue with the defect form |
| a change that carries out a mission | a pull request that names it (`W-13`) |
| a question only the owner can rule | an issue with the question form, or a record in `lab/questions/` |
| a study, a measurement, a proposal | an issue with the study form first, so that it is not done twice; then a pull request with its folder in `lab/studies/` |
| a piece of work you think is needed | an issue with the mission form, or a record in `work/missions/` |
| an erratum in a text | an issue with the erratum form, or a pull request |
| a weakness in security | nothing public: [report it privately](SECURITY.md) |

## How to propose

1. Fork the repository and make a branch for one task.
2. Read [AGENTS.md](../AGENTS.md): the map, the law and the language; and the
   AGENTS.md of the folder you change, if it has one. What is owed is kept in
   `work/`, as missions in the owner's order. A change that settles an open
   ruling of the treatise is not a proposal; it needs the owner's ruling
   first: ask it in `lab/`, as a question.
3. In `source/`: build and check both modules; `gofmt` and `go vet` clean; no
   new dependency; no new skipped test. Keep
   [source/STATE.md](../source/STATE.md) true: strike what you finished, add
   what you found undone.
4. In `docs/`, `lab/` and `work/`: the check of the records passes,
   `cd .github/records && go run . -root ../.. check`.
5. Open a pull request that says what changed, why, and how it was checked,
   and names the mission it carries out, if any. The checks must pass.
   Commits in English, present tense, one concern per commit.

## In each folder

- **`docs/`.** What is there is ruled. It is not changed by argument; it is
  changed by the owner's ruling, recorded in `docs/rulings/` in the same pull
  request, and argued for in `lab/`. An erratum, a sentence said more plainly,
  a broken reference, a typing error, says why it changes no meaning.
- **`lab/`.** A question says what is asked, why, with the evidence, what
  changes once it is ruled, and what it blocks. A question opened with the
  form takes its number when its record is written in `lab/questions/`; its
  issue then takes the record's title, `D-nn: …`, and follows the record. A
  study is corrected by a pull request that says what it corrects and why; the
  study's history keeps what it said before.
- **`work/`.** To take up a mission, claim it on its issue and follow [How to
  pick one up](../work/README.md#how-to-pick-one-up). A mission opened with the
  form takes its number when its record is written in `work/missions/`; its
  issue then follows the record. To mark a mission done after the owner merged
  its work: a pull request that sets `status: done` and names the commit.

## What is not accepted

- A change of a byte form that edits an old generation instead of naming a
  new one.
- Anything that makes the core act on its own: a timer, a watcher, a poller,
  a goroutine that writes.
- A dependency outside Go's standard library.
- A name of a person, a machine, a network address, a home directory or a
  product in code, comments, tests or documents.
- A rewrite of history.
- A change of what a text in `docs/` says without the owner's ruling, or of
  a text without its pin.
- A study, a plan or a proposal in `source/`. Those belong to `lab/` and
  `work/`.

## Reporting a problem

Open an issue with the defect form: what you did, what you expected and where
that is said, what happened, and the version (`rokh version`). For a problem
that touches what is sealed, use the private report described in
[SECURITY.md](SECURITY.md) instead of a public issue.
