---
id: W-04
title: "The twelve legacy test files in version 1, beginning with an idle daemon"
kind: mission
priority: p1
status: blocked
needs: W-01
lands-in: rokh
evidence: "A-02"
---

# W-04. The twelve legacy test files in version 1, beginning with an idle daemon

## Why

- **A-02.** The twelve `legacy09` test files do not compile against version 1;
  among them is the only test that an idle daemon writes nothing, so law 3 has
  no live test at a door — *evidence: ✔*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

The twelve `legacy09` files brought to version 1, or each removed with a line
in STATE.md saying what it tested; first a live test that an idle daemon writes
nothing (law 3), by counting a medium's writes while the daemon answers reads.

## Where

the files `grep -l legacy09` finds; `rokh/daemon`; `rokh-home`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**W-01**](W-01-short-socket-folder.md): One short socket folder for every
  test that listens

## Done when

No `legacy09` file remains, or every one left compiles in a check; a daemon
that answers only reads leaves no write on its medium.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
