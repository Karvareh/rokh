---
id: I-10
title: "People and programs, each with keys of their own"
kind: investigation
priority: p2
status: ready
needs: none
lands-in: lab
evidence: "Q-12, Q-13"
---

# I-10. People and programs, each with keys of their own

## Why

- **Q-12.** People and programs with authority of their own are keys: grants
  take memory as the square of them; an address has at most 32 readers; 31 keys
  beside the owner's open a vessel with a passphrase; one commit at a time per
  carrier — *evidence: M (§M6, §M7, §M9), C (§C6)*
- **Q-13.** Booth sessions, requests per session, and keys that act through a
  booth without a cell of their own were not measured; every writer measured
  used one key — *evidence: R (§0, §F)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## What to find out

People and programs: several keys a person (generations, devices, programs)
writing at once, each with its own key.

What is found is written as a study of rokh-lab, with its raw data, its
commands, its host and the commit it ran against; benchmarks and probes that
stay land in rokh.

## Where

`rokh/bench`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

Tables of people by programs, measured.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of
  rokh-lab.
