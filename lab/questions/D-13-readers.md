---
id: D-13
title: "Readers beyond 32, readers by seed, and a key for each address and period"
priority: p2
status: open
blocks: none
evidence: "S-09, R-01, Q-01, Q-12"
ruling: none
---

# D-13. Readers beyond 32, readers by seed, and a key for each address and period

## The question

Readers beyond 32 an address; readers that differ by seed; a key for each
address and period (§P2.3).

## Why it is asked

- **S-09.** Version 1 fixes 32 readers to an envelope (the 33rd refused) and 32
  slot cells to a vessel; 2^22 slabs of at most 2^26 bytes, 256 TiB; 4 KiB of
  payload; 16 parents. An address read by more than 32 keys cannot be written —
  *evidence: C (§C6), M (§M7)*
- **R-01.** Readers that differ by seed — *evidence: R (contract 4.4)*
- **Q-01.** Confidentiality cannot follow a seed's role: the readers of an
  address are a fold of the causal past, the same in every seed; a role chooses
  which bodies a seed holds, never who opens them — *evidence: R (contract 4.4,
  4.7 S3), C*
- **Q-12.** People and programs with authority of their own are keys: grants
  take memory as the square of them; an address has at most 32 readers; 31 keys
  beside the owner's open a vessel with a passphrase; one commit at a time per
  carrier — *evidence: M (§M6, §M7, §M9), C (§C6)*

The findings are kept, with their evidence, in [study
0002](../studies/0002-findings-of-1.0.0/README.md); the measurements in [study
0001](../studies/0001-scale/README.md).

## What changes once it is ruled

Contract 3.1, 4.4 (a new generation).

## Waits for

Nothing.

## Ruled when

The ruling.

## How it is ruled

The owner rules by a record in `docs/rulings/` that answers D-13 and
changes the texts it changes. This record then says `status: ruled` and names
that ruling, and the missions it blocks no longer need it.
