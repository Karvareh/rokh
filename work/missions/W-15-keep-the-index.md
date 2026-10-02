---
id: W-15
title: "Keep the vessel's index across commits"
kind: mission
priority: p2
status: blocked
needs: I-01
lands-in: source
evidence: "S-04, S-13, Q-08"
---

# W-15. Keep the vessel's index across commits

## Why

- **S-04.** At a door a write, a status and a log grow with the history: a
  write in 25 ms at 1,000 events, 1.2 s at 300,000; 6.5 s of 7.0 s went to
  finding the branch references — *evidence: M (§M5, §M12), C (§C2)*
- **S-13.** A writer cuts the reads of a carrier from about a thousand a second
  to 32 to 107 — *evidence: M (§M9)*
- **Q-08.** Every opening, and the first answer of a door after every commit,
  reads, verifies and opens every pack, content included: a carrier holding
  700 GB of files is read whole each time, some 45 minutes to two hours on this
  host — *evidence: C (`(*Vessel).indexOnce`, `(*Tx).Commit`,
  `(*Server).behind`), M (§M3), X*

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## The change

§P1.2: keep the index across commits, adding what a commit wrote; find a branch
by its last pointer.

## Where

`rokh/vessel/tx.go`, `rokh/vessel/vessel.go`, `rokh/carrier/carrier.go`.

Paths are of the source, under `source/`, unless they say otherwise.

## Waits for

- [**I-01**](I-01-measure-a-library.md): Measure a library

I-01 measures the baseline this mission is measured against.

## Done when

`BenchmarkScaleDoor` at 300,000 events: a status in milliseconds; after a
commit, a door's next answer reads no pack it had read; I-01's library opened
again reads its heads and what they walk.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
