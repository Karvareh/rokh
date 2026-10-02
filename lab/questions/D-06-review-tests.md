---
id: D-06
title: "Which of the two review tests of rokh/key holds"
priority: p1
status: open
blocks: none
evidence: "A-04, I-11"
ruling: none
---

# D-06. Which of the two review tests of rokh/key holds

## The question

The two review tests of `rokh/key` ask opposite things of one session: which
holds (the owner's order, item 1).

## Why it is asked

- **A-04.** The three skipped tests fail for three different reasons: the `key`
  test with `ErrOwnerUnknown`, since it gives no point while production always
  passes one; the delegate test in its setup (`no live key`), while the guard
  it tests stands (`rokh/cmd/rokh/bond.go:334-338`); the archive test held
  about 650 MB for 96 MB — *evidence: ✔ (the archive ◇)*
- See **I-11**.

The findings are kept, with their evidence, in [study
0002](../studies/0002-findings-of-1.0.0/README.md); the measurements in [study
0001](../studies/0001-scale/README.md).

## What changes once it is ruled

`rokh/key`.

## Waits for

- [**I-11**](https://github.com/Karvareh/rokh-work/blob/main/missions/I-11-why-the-key-test-fails.md)
  (rokh-work): Why the key test fails

## Ruled when

One test kept, the other removed, no skip.

## How it is ruled

The owner rules in rokh-docs, by a record in `rulings/` that answers D-06 and
changes the texts it changes. This record then says `status: ruled` and names
that ruling, and the missions it blocks no longer need it.
