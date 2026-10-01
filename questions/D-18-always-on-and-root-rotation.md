---
id: D-18
title: "An always-on seed, and rotating the root key"
priority: p2
status: open
blocks: none
evidence: "R-04, Q-04"
ruling: none
---

# D-18. An always-on seed, and rotating the root key

## The question

An always-on seed holding the ledger and a key (N5.2), and rotating the root
key (T13.2): the open rulings.

## Why it is asked

- **R-04.** An always-on seed that serves — *evidence: R (N5.2; T13.2)*
- **Q-04.** Keys by role are half there: cold custody exists per seed;
  `rokh key passwd` is not built; the root key cannot be rotated (T13.2 open);
  an old copy keeps its cells (U6); a cold seed from a warm one was not
  examined — *evidence: R, C*

The findings are kept, with their evidence, in [study
0002](../studies/0002-findings-of-1.0.0/README.md); the measurements in [study
0001](../studies/0001-scale/README.md).

## What changes once it is ruled

T, N.

## Waits for

Nothing.

## Ruled when

The rulings.

## How it is ruled

The owner rules in rokh-docs, by a record in `rulings/` that answers D-18 and
changes the texts it changes. This record then says `status: ruled` and names
that ruling, and the missions it blocks no longer need it.
