---
id: W-21
title: "The cryptography document of version 1"
kind: mission
priority: p2
status: blocked
needs: W-20
lands-in: rokh
evidence: "A-08, Q-15"
---

# W-21. The cryptography document of version 1

## Why

- **A-08.** The salt and cost of the passphrase are one per Rokh, in every seed
  and for every slot cell: one derivation from a guess is tried on all 32 cells
  of a vessel at once (0.13 ms against 128 ms for the derivation) — *evidence:
  R (contract 2.3, 4.6); M (§M7)*
- **Q-15.** A closed carrier yields its capacity, when and how much it was
  written (U1), the clear bytes of its heads, the bond between seeds (Q-02),
  and one guess tried on 32 cells (A-08); what a series of copies shows was not
  examined — *evidence: R, C, M (§M7)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

The cryptography document of version 1 (AGENTS.md, owed 4), with what a closed
carrier shows (U1; the head's first 64 bytes; the salt in every seed) and what
one guess costs.

## Where

`rokh/docs/`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**W-20**](W-20-true-documents.md): The documents of rokh made true

## Done when

Every sentence names a `path:Symbol` that exists, checked by a test.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
