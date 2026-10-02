---
id: D-22
title: "Bounds on what one key may add"
priority: p3
status: open
blocks: none
evidence: "Q-17"
ruling: none
---

# D-22. Bounds on what one key may add

## The question

Bounds on what one key may add: an open address keeps every key's events
forever; a delegate may grant without a count.

## Why it is asked

- **Q-17.** Resource consumption: a booth parses 8 MiB before binding and takes
  any number of connections; one small write makes every door read all content
  again; grants cost memory as the square, also a delegate's; an open address
  keeps every key's events; a large thing takes four times its size; a local
  process can hold the turn; none of it was run as an attack — *evidence: C, M
  (§M3, §M6, §M8, §M9)*

The findings are kept, with their evidence, in [study
0002](../studies/0002-findings-of-1.0.0/README.md); the measurements in [study
0001](../studies/0001-scale/README.md).

## What changes once it is ruled

Contract 4.5; T6.

## Waits for

- [**I-03**](../../work/missions/I-03-wasteful-use.md)
  (work/): Wasteful use of resources, as probes

## Ruled when

The ruling.

## How it is ruled

The owner rules by a record in `docs/rulings/` that answers D-22 and
changes the texts it changes. This record then says `status: ruled` and names
that ruling, and the missions it blocks no longer need it.
