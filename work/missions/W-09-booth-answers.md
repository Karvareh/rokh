---
id: W-09
title: "The booth's answers as the contract names them"
kind: mission
priority: p2
status: ready
needs: none
lands-in: rokh
evidence: "A-13, A-38"
---

# W-09. The booth's answers as the contract names them

## Why

- **A-13.** The old `Server.Serve` has no caller in production; `capabilities`
  names the protocol `rokh.daemon/3`; the booth's `seed` leaves out the vessel
  id, seed id, size and free space contract B2 lists, and gives the count of
  accepted events as its generation — *evidence: ✔; `rokh/daemon/commit.go:33`;
  C, `rokh/daemon/booth.go:246-254`*
- **A-38.** Contract B2 and B7: a full vessel is answered `storage_failed`, not
  `vessel_full`; a recording op refused with `hello_first` carries no `record`
  — *evidence: ◇*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

`capabilities` names `rokh.booth/1`; the old `Server.Serve` removed or given a
caller and a test; a full vessel answered `vessel_full`; a refused recording op
carries `record` (B2, B7).

## Where

`rokh/daemon/commit.go`, `rokh/daemon/daemon.go`, `rokh/booth/booth.go`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

One test for each answer.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
