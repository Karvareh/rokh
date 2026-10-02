---
id: D-09
title: "T6.5: a grant that names its ending event in advance"
priority: p2
status: open
blocks: none
evidence: "A-24"
ruling: none
---

# D-09. T6.5: a grant that names its ending event in advance

## The question

T6.5: a grant that names its ending event in advance, given that a byte form
changes only by a new generation.

## Why it is asked

- **A-24.** T6.5 rests on `bond.Bound`, a type, while a grant names no ending
  event in advance (`event.Grant` has only revocation); T13.3 rests on
  `working.Folder`, which nothing in production uses; `selective` and most of
  `bond` have no caller in production — *evidence: ✔*

The findings are kept, with their evidence, in [study
0002](../studies/0002-findings-of-1.0.0/README.md); the measurements in [study
0001](../studies/0001-scale/README.md).

## What changes once it is ruled

`event.Grant`; the contract.

## Waits for

Nothing.

## Ruled when

T6.5 met by a reached path, or its row waits for the ruling.

## How it is ruled

The owner rules in rokh-docs, by a record in `rulings/` that answers D-09 and
changes the texts it changes. This record then says `status: ruled` and names
that ruling, and the missions it blocks no longer need it.
