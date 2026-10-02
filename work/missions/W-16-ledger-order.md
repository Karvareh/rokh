---
id: W-16
title: "Extend the ledger's order, and read a log from its end"
kind: mission
priority: p2
status: ready
needs: none
lands-in: source
evidence: "S-04"
---

# W-16. Extend the ledger's order, and read a log from its end

## Why

- **S-04.** At a door a write, a status and a log grow with the history: a
  write in 25 ms at 1,000 events, 1.2 s at 300,000; 6.5 s of 7.0 s went to
  finding the branch references — *evidence: M (§M5, §M12), C (§C2)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

§P1.3: extend the ledger's order; read the last lines of a log from its end.

## Where

`rokh/ledger/ledger.go`, `rokh/daemon/daemon.go`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

`BenchmarkScaleDoor`: the log's time flat in the history; the ledger's tests
pass.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
