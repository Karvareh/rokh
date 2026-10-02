---
id: I-07
title: "Spread, branches and merges in the measurements"
kind: investigation
priority: p2
status: ready
needs: none
lands-in: lab
evidence: "S-14"
---

# I-07. Spread, branches and merges in the measurements

## Why

- **S-14.** Not run: capacity over 16 GiB on a disk or over 1 TiB anywhere;
  content over 2 GiB; any booth session, socket, courier, gate or network; any
  person; solid-state, removable, FAT32 or exFAT media, synced folders, the
  chest; another host; more than one sample a size — *evidence: R (§F)*

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## What to find out

Spread: §M1 at 64 GiB, §M5 at 100,000 events and §M9 five times each; histories
with many branches and merges; a reconcile of the 2,000-seed lineages.

What is found is written as a study of `lab/`, with its raw data, its
commands, its host and the commit it ran against; benchmarks and probes that
stay land in `source/`.

## Where

`rokh/bench`.

Paths are of the source, under `source/`, unless they say otherwise.

## Waits for

Nothing.

## Done when

The tables of §M with a spread.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md), the [AGENTS.md of
  `lab/`](../../lab/AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
