---
id: W-05
title: "A conformance index that honours build constraints"
kind: mission
priority: p1
status: blocked
needs: W-04
lands-in: source
evidence: "A-21"
---

# W-05. A conformance index that honours build constraints

## Why

- **A-21.** The conformance map's index ignores build tags, so T2.2, T2.3,
  T11.10/enforced, T11.11/enforced and T13.5/covenant are met by tests that do
  not compile; the paths of T2.2 and T2.3 are not reached from `rokh daemon`
  (`unknown_op`) — *evidence: ✔ (the reach ◇);
  `rokh/conformance/obligations.go:157-223` (`Index`)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

The conformance index honours build constraints.

## Where

`rokh/conformance/obligations.go`, `rokh/conformance/obligations.tsv`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**W-04**](W-04-legacy-tests.md): The twelve legacy test files in version 1,
  beginning with an idle daemon

## Done when

A test indexes a file behind a tag and does not count it; the five rows become
owed or point at live tests; `conformance/STATE.md`, STATE.md and the README
give the same counts.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
