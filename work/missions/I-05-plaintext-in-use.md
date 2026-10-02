---
id: I-05
title: "Plaintext left behind in use"
kind: investigation
priority: p3
status: ready
needs: none
lands-in: lab
evidence: "Q-16"
---

# I-05. Plaintext left behind in use

## Why

- **Q-16.** Leakage in use: the host sees all while a carrier is open (U8,
  T12.6); `rokh-forms` writes plaintext outside the carrier (A-47); a revoked
  key keeps what it opened (U6); temporary files, swap and crash dumps were not
  examined — *evidence: R, C*

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## What to find out

Plaintext left in use: temporary files, the folder `rokh-forms` writes, swap,
crash dumps, a terminal; what a program in the home's enclosure can reach.

What is found is written as a study of `lab/`, with its raw data, its
commands, its host and the commit it ran against; benchmarks and probes that
stay land in `source/`.

## Waits for

Nothing.

## Done when

A list of every path that writes plaintext, with what writes it.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md), the [AGENTS.md of
  `lab/`](../../lab/AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
