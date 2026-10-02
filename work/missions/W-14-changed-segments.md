---
id: W-14
title: "Write only the changed inventory segments"
kind: mission
priority: p2
status: blocked
needs: W-03
lands-in: source
evidence: "S-03, A-27, Q-07, Q-09"
---

# W-14. Write only the changed inventory segments

## Why

- **S-03.** One note writes (1 + ⌈N/4096⌉) slabs, at least 1/4096 of the
  capacity, and takes about 5 µs for every slab of it: 256 MiB and 23 s at
  1 TiB — *evidence: M (§M1), C (§C1)*
- **A-27.** Every commit writes every inventory segment, where contract 2.6,
  step 3, writes the changed ones — *evidence: ✔; C,
  `rokh/vessel/tx.go:341, 389`; measured as S-03*
- **Q-07.** On a solid-state disk every note writes at least two slabs and a
  head, and past 4,096 slabs 1/4096 of the capacity: slab size and capacity set
  the wear — *evidence: M (§M1), C (§C1)*
- **Q-09.** One note in a carrier of 1 TB writes 257 MiB with slabs of 1 MiB,
  or 320 MiB with slabs of 64 MiB, and takes about 7 s or 1.7 s in memory,
  twice that on a disk — *evidence: X (§X1 from §M1, §M2)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

§P1.1: write the changed segments; keep a list of free slabs; verify a
generation whole only when the heads moved.

## Where

`rokh/vessel/tx.go`, `rokh/vessel/vessel.go`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**W-03**](W-03-checks.md): Checks that see formatting, other systems, skips
  and changed files

## Done when

`BenchmarkScaleVessel` at 1 TiB: a note writes a pack, a few segments and a
head, and takes milliseconds; the vessel's tests, the cuts and interleavings
and the byte vectors pass unchanged.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
