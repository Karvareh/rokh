---
id: W-29
title: "Seeds that grow, and a reconcile that changes no byte when nothing changed"
kind: mission
priority: p3
status: ready
needs: none
lands-in: source
evidence: "Q-06; the owner's order, item 7"
---

# W-29. Seeds that grow, and a reconcile that changes no byte when nothing changed

## Why

- **Q-06.** Synchronization runs only when asked, reads both histories whole,
  and a repeated reconcile changes bytes; R6 is not built — *evidence: M (§M5),
  C, R*
- **[The owner's order](../README.md#the-owners-order), item 7:** Seeds that
  grow; a reconcile that changes no byte when nothing changed.

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

Seeds that grow; a reconcile that changes no byte when nothing changed.

## Where

`rokh/cmd/rokh`, `rokh/vessel`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

A repeated reconcile leaves every file's bytes as they were.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
