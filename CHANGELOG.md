# Changelog

## Unreleased

- Rokh is kept in four repositories: this one, the source; rokh-docs, for what
  is ruled; rokh-lab, for what is examined and not ruled; rokh-work, for the
  missions that anyone can pick up.
- The texts move to rokh-docs, where they are kept and changed by ruling. This
  tree keeps copies of them in `rokh/texts/`, pinned by SHA-256, and a test of
  `rokh/conformance` refuses a copy that differs from its pin.
- The scale study and the findings of 1.0.0 are in rokh-lab, as studies 0001
  and 0002; the work they ask for is in rokh-work. STATE.md says what they
  found that this tree does not yet do.
- The owner's list of what is owed, in order, moves from AGENTS.md to
  rokh-work, unchanged, as the owner's order; AGENTS.md points to it.
- `ledger.Load` walks a long history without a call per generation; a history
  of a million events loads.
- Benchmarks of scale, in `rokh/bench` and `rokh/cmd/rokh`, run only when
  asked.

## 1.0.0 — 2026-09-30

First public release of the source. Two modules: the core, `rokh`, and the
home, `rokh-home`. What is built, what is not, and what was never run is in
[STATE.md](STATE.md); what the code conforms to is in
`rokh/conformance/STATE.md`.
