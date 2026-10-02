---
id: W-30
title: "The booth's content, keyring and seeding"
kind: mission
priority: p3
status: blocked
needs: W-01, W-19
lands-in: source
evidence: "the owner's order, item 8"
---

# W-30. The booth's content, keyring and seeding

## Why

- **[The owner's order](../README.md#the-owners-order), item 8:** The booth:
  `content.put`, `content.get`, the keyring, seeding between two booths.

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

The booth's `content.put`, `content.get`, the keyring, and seeding between two
booths.

## Where

`rokh/daemon`, `rokh/booth`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**W-01**](W-01-short-socket-folder.md): One short socket folder for every
  test that listens
- [**W-19**](W-19-bounded-content.md): Content in bounded memory, both ways

## Done when

The ops contract B2 lists for them, tested between two booths.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
