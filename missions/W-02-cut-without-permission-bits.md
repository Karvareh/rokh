---
id: W-02
title: "Cut a seed in two tests without permission bits"
kind: mission
priority: p1
status: ready
needs: none
lands-in: rokh
evidence: "A-03"
---

# W-02. Cut a seed in two tests without permission bits

## Why

- **A-03.** Two tests fail when the superuser runs them: they cut a seed with
  read-only permission bits, which the superuser writes through — *evidence: ✔;
  again in the scale runs (§T); `rokh/cmd/rokh/v1_seedresume_test.go:23-39`*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

Cut the seed in the two tests without permission bits: a medium that refuses to
write, or another cut the superuser cannot pass.

## Where

`rokh/cmd/rokh/v1_seedresume_test.go`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

Both pass for the superuser and for anyone else; their paragraph is struck from
STATE.md.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
