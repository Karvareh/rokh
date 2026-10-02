---
id: I-09
title: "Field results"
kind: investigation
priority: p3
status: blocked
needs: W-03, W-22
lands-in: lab
evidence: "S-14; the owner's order, item 11"
---

# I-09. Field results

## Why

- **S-14.** Not run: capacity over 16 GiB on a disk or over 1 TiB anywhere;
  content over 2 GiB; any booth session, socket, courier, gate or network; any
  person; solid-state, removable, FAT32 or exFAT media, synced folders, the
  chest; another host; more than one sample a size — *evidence: R (§F)*
- **[The owner's order](../README.md#the-owners-order), item 11:** Runs on
  Windows and Android; exFAT and a synced folder; a real engine; vessels of
  hundreds of megabytes. These are field results, not code.

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## What to find out

Field results: Windows, Android, macOS; exFAT; a synced folder; FAT32 on
removable media; vessels of hundreds of megabytes; a real engine; a solid-state
disk's bytes written for each note.

What is found is written as a study of `lab/`, with its raw data, its
commands, its host and the commit it ran against; benchmarks and probes that
stay land in `source/`.

## Waits for

- [**W-03**](W-03-checks.md): Checks that see formatting, other systems, skips
  and changed files
- [**W-22**](W-22-windows-prompt-and-builds.md): A passphrase prompt on
  Windows, and builds that stop at a failure

## Done when

Results in STATE.md, under Measured or Never run.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md), the [AGENTS.md of
  `lab/`](../../lab/AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
