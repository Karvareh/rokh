---
id: D-12
title: "May an opening rest on a checkpoint the owner signed?"
priority: p2
status: open
blocks: none
evidence: "S-08"
ruling: none
---

# D-12. May an opening rest on a checkpoint the owner signed?

## The question

May an opening rest on a checkpoint the owner's key signed, the whole walk
still possible (N2.6, §P2.2)?

## Why it is asked

- **S-08.** Every opening verifies the whole history and holds it in memory:
  86 µs an event in a bare ledger, 146 at a door, 210 for each command; a
  million events in 86 s and 5.4 GiB — *evidence: M (§M4, §M5), C (§C8)*

The findings are kept, with their evidence, in [study
0002](../studies/0002-findings-of-1.0.0/README.md); the measurements in [study
0001](../studies/0001-scale/README.md).

## What changes once it is ruled

The contract (a new generation); `rokh/ledger`, `rokh/carrier`.

## Waits for

Nothing.

## Ruled when

The ruling.

## How it is ruled

The owner rules by a record in `docs/rulings/` that answers D-12 and
changes the texts it changes. This record then says `status: ruled` and names
that ruling, and the missions it blocks no longer need it.
