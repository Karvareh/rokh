---
id: W-06
title: "Conformance rows that rest on reached paths"
kind: mission
priority: p2
status: blocked
needs: W-05
lands-in: source
evidence: "A-22, A-23, A-24, A-25"
---

# W-06. Conformance rows that rest on reached paths

## Why

- **A-22.** Sixteen rows rest on one of eight constants nothing uses:
  `Measure`, `Witness`, `SeatOfJudgement`, `ClosedAndAlive`
  (`rokh/ledger/ledger.go:1187-1247`), `Complete`, `Custom`, `Position`
  (`rokh/arch/arch.go`), `generation.Profile` — *evidence: ✔;
  `rokh/conformance/obligations.tsv`*
- **A-23.** A comment that belongs to `Event` stands above `FreshSize`, so
  T1.1, T5.1 and N4.3 point at `FreshSize` — *evidence: ✔;
  `rokh/event/event.go:134-146`*
- **A-24.** T6.5 rests on `bond.Bound`, a type, while a grant names no ending
  event in advance (`event.Grant` has only revocation); T13.3 rests on
  `working.Folder`, which nothing in production uses; `selective` and most of
  `bond` have no caller in production — *evidence: ✔*
- **A-25.** T4.1 rests on a search of the source that `weak.tsv` does not list
  — *evidence: ✔; `rokh/arch/profile_test.go:199-214`
  (`TestOnlyOneWritingPathDrawsFreshness`)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

Each row named there either names a path production reaches and a test that
exercises it, or is listed in `weak.tsv` with its reason; the comment of
`Event` goes back above `Event`.

## Where

`rokh/event/event.go`, `rokh/conformance/obligations.tsv`,
`rokh/conformance/weak.tsv`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**W-05**](W-05-index-honours-build-tags.md): A conformance index that
  honours build constraints

## Done when

`go test ./conformance` passes; every such row is met by a reached path, or
listed weak, or waits for D-09.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
