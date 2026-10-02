---
id: W-18
title: "A fair turn, and one commit for the writers waiting"
kind: mission
priority: p2
status: ready
needs: none
lands-in: source
evidence: "S-06"
---

# W-18. A fair turn, and one commit for the writers waiting

## Why

- **S-06.** A carrier records about 35 commits a second whatever the doors and
  writers; the turn goes to whoever tries first: among 128 writers on 32 doors,
  all with one key, 11 of 1,024 writes were refused after 15 s; no write
  answered `recorded` was lost — *evidence: M (§M9), C (§C5)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

§P1.5: a fair turn; one commit for the writers already waiting.

## Where

`rokh/turn`, `rokh/daemon/commit.go`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

`BenchmarkScaleWriters` at 32 × 4: nothing refused; the slowest within a small
multiple of the median; nothing recorded lost.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
