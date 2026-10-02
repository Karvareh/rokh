---
id: W-24
title: "No product's name in the code; the engine by its manifest"
kind: mission
priority: p3
status: ready
needs: none
lands-in: source
evidence: "A-34"
---

# W-24. No product's name in the code; the engine by its manifest

## Why

- **A-34.** Names of products in code, against AGENTS.md: one engine's and its
  tools' in `rokh-home/native`, a sandbox program's in `rokh-home/gate`; the
  engine's settings are tied to that one engine, where contract B6 has a
  manifest replace it — *evidence: ◇*

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## The change

Names of products out of the code; the engine out of `rokh-home/native`,
replaced by its manifest (B6; the owner's order, item 9).

## Where

`rokh-home/native`, `rokh-home/gate`.

Paths are of the source, under `source/`, unless they say otherwise.

## Waits for

Nothing.

## Done when

A test finds no product's name; replacing the manifest replaces the engine.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
