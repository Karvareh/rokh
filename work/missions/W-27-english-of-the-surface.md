---
id: W-27
title: "The English of the sentence surface"
kind: mission
priority: p2
status: blocked
needs: W-10
lands-in: source
evidence: "the owner's order, item 2"
---

# W-27. The English of the sentence surface

## Why

- **[The owner's order](../README.md#the-owners-order), item 2:** The English
  of the sentence surface. The eight verbs were carried over from Persian one
  word at a time, and some sentences do not read as English: `go` closes the
  ledger where a person would say `leave`; `see the ledgers`,
  `bring the returned`, `carry the ledger` are the same. Rewrite every sentence
  a person types or reads until it reads as plain English, keeping the eight
  meanings, and change `docs/08-sentences.md`, the vectors in
  `rokh/shell/testdata/sentences.json`, the golden screens
  (`go test ./tui -update`) and the screen's header line together.

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## The change

The English of the sentence surface.

## Where

`rokh/shell`, `rokh/tui`, `rokh/docs/08-sentences.md`,
`rokh/shell/testdata/sentences.json`.

Paths are of the source, under `source/`, unless they say otherwise.

## Waits for

- [**W-10**](W-10-sentence-surface-defects.md): Six defects of the sentence
  surface

## Done when

As AGENTS.md says, the vectors and the golden screens changed together.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
