---
id: D-25
title: "Who merges, besides the owner?"
priority: p3
status: open
blocks: none
evidence: "the AGENTS.md of this repository: the owner merges; nobody else does"
ruling: none
---

# D-25. Who merges, besides the owner?

## The question

Only the owner merges. As the work grows, may others merge some changes: the
status of a mission, an erratum, the correction of a study? And who may set
the labels `claimed` and `in-review` on an issue?

## Why it is asked

The [AGENTS.md of this repository](../../AGENTS.md): the owner of the
repository merges; nobody else does.

## What changes once it is ruled

The AGENTS.md of this repository, its `CODEOWNERS`, and the protection of
`main`.

## Waits for

Nothing.

## Ruled when

The AGENTS.md of this repository names who merges what.

## How it is ruled

The owner rules by a record in `docs/rulings/` that answers D-25 and
changes the texts it changes. This record then says `status: ruled` and names
that ruling, and the missions it blocks no longer need it.
