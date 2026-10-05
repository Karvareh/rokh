# Changelog

## Unreleased

- The sentence that ends the conversation is `leave`. It was `go`, carried
  over from Persian one word at a time, and in English a bare `go` reads as a
  start rather than an end; it is no longer a sentence. The reply begins
  `left.`, and the greeting, the hints and the list of sentences say `leave`.
- Rokh keeps its four standings as four folders of its repository: the
  source, which is this folder, `source/`; `docs/`, for what is ruled; `lab/`,
  for what is examined and not ruled; and `work/`, for the missions that
  anyone can pick up. For a day they were four repositories; the three were
  brought back as folders with their whole history.
- The texts are kept in `docs/`, and changed there by ruling. The source keeps
  no copies of them: the conformance tests read them in `docs/`, and a test of
  `rokh/conformance` holds them to the SHA-256 that `conformance/texts.tsv`
  names, which changes with them.
- The scale study and the findings of 1.0.0 are in `lab/`, as studies 0001
  and 0002; the work they ask for is in `work/`. STATE.md says what they found
  that the source does not yet do.
- The owner's list of what is owed, in order, moves from AGENTS.md to
  `work/`, unchanged, as the owner's order; AGENTS.md points to it.
- `ledger.Load` walks a long history without a call per generation; a history
  of a million events loads.
- Benchmarks of scale, in `rokh/bench` and `rokh/cmd/rokh`, run only when
  asked.

## 1.0.0 — 2026-09-30

First public release of the source. Two modules: the core, `rokh`, and the
home, `rokh-home`. What is built, what is not, and what was never run is in
[STATE.md](STATE.md); what the code conforms to is in
`rokh/conformance/STATE.md`.
