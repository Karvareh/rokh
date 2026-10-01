---
id: I-09
title: "Field results"
kind: investigation
priority: p3
status: blocked
needs: W-03, W-22
lands-in: rokh-lab
evidence: "S-14; AGENTS.md, owed 11"
---

# I-09. Field results

## Why

- **S-14.** Not run: capacity over 16 GiB on a disk or over 1 TiB anywhere;
  content over 2 GiB; any booth session, socket, courier, gate or network; any
  person; solid-state, removable, FAT32 or exFAT media, synced folders, the
  chest; another host; more than one sample a size — *evidence: R (§F)*
- **rokh's AGENTS.md, owed item 11:** runs on Windows and Android; exFAT and a
  synced folder; a real engine; vessels of hundreds of megabytes.

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## What to find out

Field results: Windows, Android, macOS; exFAT; a synced folder; FAT32 on
removable media; vessels of hundreds of megabytes; a real engine; a solid-state
disk's bytes written for each note.

What is found is written as a study of rokh-lab, with its raw data, its
commands, its host and the commit it ran against; benchmarks and probes that
stay land in rokh.

## Waits for

- [**W-03**](W-03-checks.md): Checks that see formatting, other systems, skips
  and changed files
- [**W-22**](W-22-windows-prompt-and-builds.md): A passphrase prompt on
  Windows, and builds that stop at a failure

## Done when

Results in STATE.md, under Measured or Never run.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of
  rokh-lab.
