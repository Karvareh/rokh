---
id: W-26
title: "The delegate test, for the keys of version 1"
kind: mission
priority: p1
status: ready
needs: none
lands-in: source
evidence: "A-04"
---

# W-26. The delegate test, for the keys of version 1

## Why

- **A-04.** The three skipped tests fail for three different reasons: the `key`
  test with `ErrOwnerUnknown`, since it gives no point while production always
  passes one; the delegate test in its setup (`no live key`), while the guard
  it tests stands (`rokh/cmd/rokh/bond.go:334-338`); the archive test held
  about 650 MB for 96 MB — *evidence: ✔ (the archive ◇)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

The delegate test rewritten for the keys of version 1 (the owner's order, item
1).

## Where

`rokh/cmd/rokh/bond_guard_test.go`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

It passes with its skip removed.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
