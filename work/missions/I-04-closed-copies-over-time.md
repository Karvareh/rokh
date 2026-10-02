---
id: I-04
title: "What closed copies show over time"
kind: investigation
priority: p2
status: ready
needs: none
lands-in: lab
evidence: "Q-15"
---

# I-04. What closed copies show over time

## Why

- **Q-15.** A closed carrier yields its capacity, when and how much it was
  written (U1), the clear bytes of its heads, the bond between seeds (Q-02),
  and one guess tried on 32 cells (A-08); what a series of copies shows was not
  examined — *evidence: R, C, M (§M7)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## What to find out

What closed copies show over time: which files change at each commit, file
times on ext4, FAT32 and exFAT, which head was written last; two seeds compared
by their first 64 bytes.

What is found is written as a study of rokh-lab, with its raw data, its
commands, its host and the commit it ran against; benchmarks and probes that
stay land in rokh.

## Where

a test in `rokh/vessel`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

A list of what an observer of closed copies learns, each item with its test.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of
  rokh-lab.
