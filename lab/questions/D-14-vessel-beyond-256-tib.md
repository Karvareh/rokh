---
id: D-14
title: "A vessel beyond 256 TiB"
priority: p2
status: open
blocks: none
evidence: "S-09, Q-11"
ruling: none
---

# D-14. A vessel beyond 256 TiB

## The question

A vessel beyond 256 TiB: an inventory as a tree, packs appended, which also
closes G1 (§P2.1).

## Why it is asked

- **S-09.** Version 1 fixes 32 readers to an envelope (the 33rd refused) and 32
  slot cells to a vessel; 2^22 slabs of at most 2^26 bytes, 256 TiB; 4 KiB of
  payload; 16 parents. An address read by more than 32 keys cannot be written —
  *evidence: C (§C6), M (§M7)*
- **Q-11.** A petabyte is beyond the version 1 format, which ends at 256 TiB —
  *evidence: C (§C6), X (§X1)*

The findings are kept, with their evidence, in [study
0002](../studies/0002-findings-of-1.0.0/README.md); the measurements in [study
0001](../studies/0001-scale/README.md).

## What changes once it is ruled

Contract 1, 2 (a new generation).

## Waits for

- [**D-07**](D-07-live-copy.md): A live copy beyond three commits: by design,
  or a gap to close

## Ruled when

The ruling.

## How it is ruled

The owner rules by a record in `docs/rulings/` that answers D-14 and
changes the texts it changes. This record then says `status: ruled` and names
that ruling, and the missions it blocks no longer need it.
