---
id: I-01
title: "Measure a library"
kind: investigation
priority: p1
status: ready
needs: none
lands-in: lab
evidence: "Q-08, Q-09, Q-10, S-14"
---

# I-01. Measure a library

## Why

- **Q-08.** Every opening, and the first answer of a door after every commit,
  reads, verifies and opens every pack, content included: a carrier holding
  700 GB of files is read whole each time, some 45 minutes to two hours on this
  host — *evidence: C (`(*Vessel).indexOnce`, `(*Tx).Commit`,
  `(*Server).behind`), M (§M3), X*
- **Q-09.** One note in a carrier of 1 TB writes 257 MiB with slabs of 1 MiB,
  or 320 MiB with slabs of 64 MiB, and takes about 7 s or 1.7 s in memory,
  twice that on a disk — *evidence: X (§X1 from §M1, §M2)*
- **Q-10.** A library is brought a thing at a time, each held four times in
  memory, through the sentence surface or the home (the command line has no
  `bring`); filling 700 GB through a door reads the content again after every
  commit: tens of terabytes — *evidence: M (§M3), C, X*
- **S-14.** Not run: capacity over 16 GiB on a disk or over 1 TiB anywhere;
  content over 2 GiB; any booth session, socket, courier, gate or network; any
  person; solid-state, removable, FAT32 or exFAT media, synced folders, the
  chest; another host; more than one sample a size — *evidence: R (§F)*

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## What to find out

A library measured: a carrier on a disk holding tens of GiB of incompressible
files of mixed sizes, brought through a door; the time to open, the first
answer after a commit, a note's commit, the fetch of one file; on a hard disk
and on a solid-state disk; again after W-14 and W-15.

What is found is written as a study of `lab/`, with its raw data, its
commands, its host and the commit it ran against; benchmarks and probes that
stay land in `source/`.

## Where

a benchmark in `rokh/bench`.

Paths are of the source, under `source/`, unless they say otherwise.

## Waits for

Nothing.

## Done when

Raw outputs in a study of `lab/`, with host, commit and SHA-256; §4.2's
estimates confirmed or replaced.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md), the [AGENTS.md of
  `lab/`](../../lab/AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
