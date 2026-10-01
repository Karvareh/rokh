---
id: W-23
title: "The home's records past one slab"
kind: mission
priority: p3
status: blocked
needs: I-08
lands-in: rokh
evidence: "A-28, A-29"
---

# W-23. The home's records past one slab

## Why

- **A-28.** The home keeps a catalog, a journal or an import's preview in one
  pointer record that must fit one slab: about 2,000 items, or 6,000 files
  previewed — *evidence: ◇*
- **A-29.** The comment on `BytesWithHash` says its memory does not grow with
  the file; it does, since the store assembles the whole object first —
  *evidence: ◇, then C; `rokh-home/home/items.go:792-797`,
  `rokh-home/vesselstore/vesselstore.go:190-200` (`Store.Get`)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

The home's catalog and preview past one slab; `BytesWithHash`'s comment made
true, or its memory bounded.

## Where

`rokh-home/home`, `rokh-home/vesselstore`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**I-08**](I-08-home-one-slab-records.md): The home's one-slab records,
  reproduced

## Done when

A catalog of 10,000 items; the comment agrees with a measurement.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
