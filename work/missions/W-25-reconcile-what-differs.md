---
id: W-25
title: "A reconcile that moves only what differs"
kind: mission
priority: p3
status: blocked
needs: W-15
lands-in: source
evidence: "S-12, Q-06"
---

# W-25. A reconcile that moves only what differs

## Why

- **S-12.** A reconcile reads both histories whole: 2.3 s at 100,000 events to
  move two records — *evidence: M (§M5)*
- **Q-06.** Synchronization runs only when asked, reads both histories whole,
  and a repeated reconcile changes bytes; R6 is not built — *evidence: M (§M5),
  C, R*

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## The change

A reconcile that compares the heads first and moves only what differs.

## Where

`rokh/cmd/rokh`, `rokh/lineage`, `rokh/bundle`.

Paths are of the source, under `source/`, unless they say otherwise.

## Waits for

- [**W-15**](W-15-keep-the-index.md): Keep the vessel's index across commits

## Done when

`BenchmarkScaleReconcile` at 100,000 events, two records moved: time follows
the records, not the history.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
