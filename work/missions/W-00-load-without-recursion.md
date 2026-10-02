---
id: W-00
title: "Load a long history without a call per generation, and measure revocations newest first"
kind: mission
priority: p1
status: done
needs: none
lands-in: source
evidence: "S-01, S-02"
---

# W-00. Load a long history without a call per generation, and measure revocations newest first

## Why

- **S-01.** Loading a history recursed once per generation and ended the
  process with a stack overflow at a million events, 645,365 generations deep —
  *evidence: M (a probe kept outside the tree), C*
- **S-02.** The first measurement of revocations took grants back in the order
  they were made, so the ledger's pool shared the sets and showed about 2 KB a
  key; taken back newest first, revocations grow as the square, as grants do —
  *evidence: M*

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## The change

The load without recursion; the measurement of revocations.

## Where

`rokh/ledger/load.go`, `rokh/ledger/deep_test.go`,
`rokh/bench/scale_ledger_test.go`.

Paths are of the source, under `source/`, unless they say otherwise.

## Waits for

Nothing.

## Done

In rokh, by commits 4111458 (the load) and f459779 (the order of revocations);
`ledger/deep_test.go` loads 3,000 events under a stack of 512 KiB, study 0001
§M4 loads a million, and `data/revokes.txt` of study 0001 holds the revocations
measured newest first.
