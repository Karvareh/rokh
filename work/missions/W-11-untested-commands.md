---
id: W-11
title: "Tests for the commands and the adapter that have none"
kind: mission
priority: p2
status: blocked
needs: W-01
lands-in: rokh
evidence: "A-14"
---

# W-11. Tests for the commands and the adapter that have none

## Why

- **A-14.** No tests at all in `rokh/transport`, `rokh/cmd/rokh-courier`,
  `rokh/cmd/rokh-forms`, `rokh/cmd/rokh-chest`, `rokh-home/cmd/rokh-home` —
  *evidence: ✔*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

Tests for the commands and the adapter that have none.

## Where

`rokh/transport`, `rokh/cmd/rokh-forms`, `rokh/cmd/rokh-chest`,
`rokh-home/cmd/rokh-home`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**W-01**](W-01-short-socket-folder.md): One short socket folder for every
  test that listens

## Done when

Each has tests that run in the checks.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
