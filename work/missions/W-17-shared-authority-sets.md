---
id: W-17
title: "Authority sets that share their structure"
kind: mission
priority: p2
status: ready
needs: none
lands-in: source
evidence: "S-05, S-11, Q-12"
---

# W-17. Authority sets that share their structure

## Why

- **S-05.** Grants, revocations and keyring changes take memory as the square
  of the keys named: 2 GiB for grants to 8,000 keys, as much again to revoke
  them; a lineage of 2,000 seeds holds 1.3 GiB — *evidence: M (§M6, §M11), C
  (§C3)*
- **S-11.** A seed holds every system event before it: 3 + 4n events for the
  n-th of a line; a lineage's memory grows as the square, and some 7,000 seeds
  in a line cannot be opened on the host — *evidence: M (§M11), X (§X4)*
- **Q-12.** People and programs with authority of their own are keys: grants
  take memory as the square of them; an address has at most 32 readers; 31 keys
  beside the owner's open a vessel with a passphrase; one commit at a time per
  carrier — *evidence: M (§M6, §M7, §M9), C (§C6)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

§P1.4: authority sets that share their structure.

## Where

`rokh/ledger/set.go`, `rokh/ledger/ledger.go`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

`BenchmarkScaleAuthority` at 8,000 keys holds tens of MiB, not 2 GiB; every
verdict of the tests and vectors is the same.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
