---
id: I-06
title: "A cold seed from a warm source"
kind: investigation
priority: p2
status: blocked
needs: W-13
lands-in: rokh-lab
evidence: "R-09, Q-04"
---

# I-06. A cold seed from a warm source

## Why

- **R-09.** Making a cold seed from a warm source was not examined — *evidence:
  R (§S2)*
- **Q-04.** Keys by role are half there: cold custody exists per seed;
  `rokh key passwd` is not built; the root key cannot be rotated (T13.2 open);
  an old copy keeps its cells (U6); a cold seed from a warm one was not
  examined — *evidence: R, C*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## What to find out

A cold seed made from a warm source; what a working seed needs of the root key;
rotation and `passwd` across seeds.

What is found is written as a study of rokh-lab, with its raw data, its
commands, its host and the commit it ran against; benchmarks and probes that
stay land in rokh.

## Where

`rokh/cmd/rokh`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**W-13**](W-13-key-passwd.md): rokh key passwd

## Done when

A test makes a cold seed from a warm one and finds no root key in the new
vessel.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of
  rokh-lab.
