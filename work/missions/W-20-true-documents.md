---
id: W-20
title: "The documents of rokh made true"
kind: mission
priority: p2
status: ready
needs: none
lands-in: source
evidence: "A-40 to A-47"
---

# W-20. The documents of rokh made true

## Why

- **A-40.** `docs/01` describes RKH1, four reserved verbs and freshness by an
  oracle; version 1 has RKH3, six reserved verbs and `fresh` in the head —
  *evidence: ✔*
- **A-41.** `docs/04` gives sizes of 231, 347 and 383 bytes as measured by
  `oracle/size_test.go`, which measures 311, 431 and 436 — *evidence: ✔*
- **A-42.** `docs/05` describes `rokh.json`, `.rokh/objects`, temporary files
  and content outside the carrier, none of which version 1 has, and cites
  `carrier/inventory_test.go` and `TestOrphanTempIsIgnored`, which do not exist
  — *evidence: ✔*
- **A-43.** `docs/07` says no code path opens TCP, sealing is not built, and
  only the root discloses; version 1 does all three otherwise — *evidence: ✔*
- **A-44.** `docs/08` describes `seat.json`, the library and a berth, and calls
  sealing absent on purpose; `seat.json` is still read though the contract
  replaced it — *evidence: ✔*
- **A-45.** `docs/02` cites
  `proof/TestRevocationClosesTheFutureAndDoesNotUnsee`, which does not exist —
  *evidence: ✔; `rokh/docs/02-authority.md:103`*
- **A-46.** The README's `rokh seed SRC DST [--size …]` fails ("give the new
  folder"); the flags go before the new folder — *evidence: ✔*
- **A-47.** The README calls `rokh-forms` an adapter on the booth; it runs the
  command line and writes what it is given, as plaintext, to a folder outside
  the carrier, which contract E5 says is no longer written — *evidence: ✔; C,
  `rokh/cmd/rokh-forms/main.go:122-140` (`writeBlob`)*
- See **A-40**.
- See **A-41**.
- See **A-42**.
- See **A-43**.
- See **A-44**.
- See **A-45**.
- See **A-46**.
- See **A-47**.

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## The change

The documents made true: `docs/01`, `04`, `05`, `07`, `08` rewritten or
retired; `docs/02`'s citation; the README's `rokh seed` and `rokh-forms`;
`docs/06` (the owner's order, item 3).

## Where

`rokh/docs/*.md`, `README.md`.

Paths are of the source, under `source/`, unless they say otherwise.

## Waits for

Nothing.

The one sentence of `docs/07` about TCP waits for **D-03** (`lab/`): write
what is built, and that the ruling is open.

## Done when

Every test, path and symbol a document names exists; each says which version it
describes.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
