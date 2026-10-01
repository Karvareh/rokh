---
id: W-28
title: "bring on the command line"
kind: mission
priority: p2
status: blocked
needs: W-19
lands-in: rokh
evidence: "Q-10; AGENTS.md, owed 6"
---

# W-28. bring on the command line

## Why

- **Q-10.** A library is brought a thing at a time, each held four times in
  memory, through the sentence surface or the home (the command line has no
  `bring`); filling 700 GB through a door reads the content again after every
  commit: tens of terabytes — *evidence: M (§M3), C, X*
- **rokh's AGENTS.md, owed item 6:** bring in the command line.

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

`bring` on the command line.

## Where

`rokh/cmd/rokh`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**W-19**](W-19-bounded-content.md): Content in bounded memory, both ways

## Done when

A file over 4,096 bytes goes in and comes out whole.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
