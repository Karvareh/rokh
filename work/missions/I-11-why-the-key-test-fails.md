---
id: I-11
title: "Why the key test fails"
kind: investigation
priority: p3
status: ready
needs: none
lands-in: lab
evidence: "A-04"
---

# I-11. Why the key test fails

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

## What to find out

Whether the `key` test fails for the contradiction STATE.md names, or for the
point it does not give.

What is found is written as a study of rokh-lab, with its raw data, its
commands, its host and the commit it ran against; benchmarks and probes that
stay land in rokh.

## Where

`rokh/key`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

A note for D-06.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of
  rokh-lab.
