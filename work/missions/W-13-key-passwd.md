---
id: W-13
title: "rokh key passwd"
kind: mission
priority: p1
status: ready
needs: none
lands-in: rokh
evidence: "A-07, Q-04"
---

# W-13. rokh key passwd

## Why

- **A-07.** `rokh key passwd` is not built: a passphrase that leaked cannot be
  replaced — *evidence: ✔; `rokh/cmd/rokh/main.go:1957`*
- **Q-04.** Keys by role are half there: cold custody exists per seed;
  `rokh key passwd` is not built; the root key cannot be rotated (T13.2 open);
  an old copy keeps its cells (U6); a cold seed from a warm one was not
  examined — *evidence: R, C*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

`rokh key passwd`, with the cells laid out as contract 4.6 says (the owner's
order, item 5).

## Where

`rokh/cmd/rokh/main.go`, `rokh/key`, `rokh/vessel`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

In this vessel the old passphrase opens nothing and the new one opens; every
other cell is unchanged; the same for a key's own passphrase.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
