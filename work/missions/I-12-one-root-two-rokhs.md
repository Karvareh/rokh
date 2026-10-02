---
id: I-12
title: "One root key founding two Rokhs"
kind: investigation
priority: p3
status: ready
needs: none
lands-in: lab
evidence: "Q-14"
---

# I-12. One root key founding two Rokhs

## Why

- **Q-14.** An anchor proves a ledger, not a person (U9); a full-name collision
  is refused by a provisional guard while T13.8 is open; whether one root key
  may found two Rokhs was not examined — *evidence: R, C*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## What to find out

One root key founding two Rokhs: what each sees, and what a bond makes of it.

What is found is written as a study of rokh-lab, with its raw data, its
commands, its host and the commit it ran against; benchmarks and probes that
stay land in rokh.

## Where

`rokh/ledger`, `rokh/bond`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

A test that shows it.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of
  rokh-lab.
