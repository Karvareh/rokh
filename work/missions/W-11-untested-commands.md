---
id: W-11
title: "Tests for the commands and the adapter that have none"
kind: mission
priority: p2
status: blocked
needs: W-01
lands-in: source
evidence: "A-14"
---

# W-11. Tests for the commands and the adapter that have none

## Why

- **A-14.** No tests at all in `rokh/transport`, `rokh/cmd/rokh-courier`,
  `rokh/cmd/rokh-forms`, `rokh/cmd/rokh-chest`, `rokh-home/cmd/rokh-home` —
  *evidence: ✔*

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## The change

Tests for the commands and the adapter that have none.

## Where

`rokh/transport`, `rokh/cmd/rokh-forms`, `rokh/cmd/rokh-chest`,
`rokh-home/cmd/rokh-home`.

Paths are of the source, under `source/`, unless they say otherwise.

## Waits for

- [**W-01**](W-01-short-socket-folder.md): One short socket folder for every
  test that listens

## Done when

Each has tests that run in the checks.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
