---
id: D-10
title: "The passphrase: salt, cost, strength, and a derivation that asks memory"
priority: p2
status: open
blocks: none
evidence: "A-08, A-09, A-10, R-02, Q-02, Q-15"
ruling: none
---

# D-10. The passphrase: salt, cost, strength, and a derivation that asks memory

## The question

The passphrase: a salt per seed or per cell (contract 2.3, a new generation); a
cost by role; a rule of strength; law 7 and a derivation that asks memory.

## Why it is asked

- **A-08.** The salt and cost of the passphrase are one per Rokh, in every seed
  and for every slot cell: one derivation from a guess is tried on all 32 cells
  of a vessel at once (0.13 ms against 128 ms for the derivation) — *evidence:
  R (contract 2.3, 4.6); M (§M7)*
- **A-09.** PBKDF2 asks no memory of a guesser; a derivation that does is not
  in Go's standard library, and law 7 allows nothing else — *evidence: R
  (AGENTS.md law 7; contract 2.3)*
- **A-10.** Nothing asks a passphrase to be strong — *evidence: C*
- **R-02.** A cost of the passphrase that differs by role — *evidence: R
  (contract 2.3; T8.7)*
- **Q-02.** Closed seeds of one Rokh can be told to belong together: the salt
  and the cost are in the clear and the same in every seed — *evidence: R
  (contract 2.3)*
- **Q-15.** A closed carrier yields its capacity, when and how much it was
  written (U1), the clear bytes of its heads, the bond between seeds (Q-02),
  and one guess tried on 32 cells (A-08); what a series of copies shows was not
  examined — *evidence: R, C, M (§M7)*

The findings are kept, with their evidence, in [study
0002](../studies/0002-findings-of-1.0.0/README.md); the measurements in [study
0001](../studies/0001-scale/README.md).

## What changes once it is ruled

Contract 2.3, 4.6; `rokh/key`, `rokh/vessel`, `rokh/passphrase`.

## Waits for

Nothing.

## Ruled when

The contract names the generation, or says the present one stands; W-21 says
which.

## How it is ruled

The owner rules by a record in `docs/rulings/` that answers D-10 and
changes the texts it changes. This record then says `status: ruled` and names
that ruling, and the missions it blocks no longer need it.
