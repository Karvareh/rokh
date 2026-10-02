---
id: I-02
title: "Booth sessions at scale"
kind: investigation
priority: p2
status: blocked
needs: W-01, W-12
lands-in: rokh-lab
evidence: "Q-13, S-14"
---

# I-02. Booth sessions at scale

## Why

- **Q-13.** Booth sessions, requests per session, and keys that act through a
  booth without a cell of their own were not measured; every writer measured
  used one key — *evidence: R (§0, §F)*
- **S-14.** Not run: capacity over 16 GiB on a disk or over 1 TiB anywhere;
  content over 2 GiB; any booth session, socket, courier, gate or network; any
  person; solid-state, removable, FAT32 or exFAT media, synced folders, the
  chest; another host; more than one sample a size — *evidence: R (§F)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## What to find out

Booth sessions at scale: many sessions, each with its own key, over a Unix
socket; requests per session; memory per session; keys without cells through
one booth.

What is found is written as a study of rokh-lab, with its raw data, its
commands, its host and the commit it ran against; benchmarks and probes that
stay land in rokh.

## Where

`rokh/bench`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**W-01**](W-01-short-socket-folder.md): One short socket folder for every
  test that listens
- [**W-12**](W-12-bounds-before-binding.md): Bounds on what a booth reads
  before a session is bound

## Done when

A table of sessions, keys and requests with raw data.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of
  rokh-lab.
