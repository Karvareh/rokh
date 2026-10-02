---
id: W-08
title: "rokh bind and rokh bound on rokh.booth/1"
kind: mission
priority: p2
status: blocked
needs: W-01
lands-in: source
evidence: "A-12"
---

# W-08. rokh bind and rokh bound on rokh.booth/1

## Why

- **A-12.** `rokh bind` and `rokh bound` speak the protocol before
  `rokh.booth/1` — *evidence: ✔; `rokh/cmd/rokh/harness.go:157, 286, 334`*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

`rokh bind` and `rokh bound` on `rokh.booth/1`.

## Where

`rokh/cmd/rokh/harness.go`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**W-01**](W-01-short-socket-folder.md): One short socket folder for every
  test that listens

## Done when

The harness tests run (W-01) and pass against `rokh daemon`.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
