---
id: W-19
title: "Content in bounded memory, both ways"
kind: mission
priority: p1
status: ready
needs: none
lands-in: rokh
evidence: "S-07, A-04, Q-10"
---

# W-19. Content in bounded memory, both ways

## Why

- **S-07.** A large thing takes four times its size in memory, 8 GiB for 2 GiB,
  and bringing it slows as it grows, 78 MiB/s to 26; contract E8 asks bounded
  memory, one chunk at a time — *evidence: M (§M3), C (§C4;
  `rokh/content/vessel.go:37-48`, `Fetch`), R (E8)*
- **A-04.** The three skipped tests fail for three different reasons: the `key`
  test with `ErrOwnerUnknown`, since it gives no point while production always
  passes one; the delegate test in its setup (`no live key`), while the guard
  it tests stands (`rokh/cmd/rokh/bond.go:334-338`); the archive test held
  about 650 MB for 96 MB — *evidence: ✔ (the archive ◇)*
- **Q-10.** A library is brought a thing at a time, each held four times in
  memory, through the sentence surface or the home (the command line has no
  `bring`); filling 700 GB through a door reads the content again after every
  commit: tens of terabytes — *evidence: M (§M3), C, X*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

§P1.6 and contract E8: content in bounded memory, both ways, in the core and
the home.

## Where

`rokh/carrier/carrier.go`, `rokh/content/vessel.go`, `rokh/vessel/tx.go`,
`rokh-home/archive`, `rokh-home/vesselstore`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

`BenchmarkScaleContent` at 2 GiB stays under a bound that does not follow the
size; the archive test passes with its skip removed (the owner's order, item
1).

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
