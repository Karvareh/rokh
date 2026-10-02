---
id: D-02
title: "Does the repair of the tests and the checks come before the first item of the owner's order?"
priority: p1
status: open
blocks: none
evidence: "study 0002, §3.1: A-01, A-02, A-03, A-05"
ruling: none
---

# D-02. Does the repair of the tests and the checks come before the first item of the owner's order?

## The question

Does the repair of the tests and the checks (W-01 to W-06) come before the
first item of the owner's order (rokh-work's README), which does not name it?

## Why it is asked

The evidence of 1.0.0 rests on tests and checks that measure less than they
seem to: thirty-three tests skip without a word (A-01), twelve test files do
not compile (A-02), two tests fail for the superuser (A-03), and the checks run
on Linux alone and count no skip (A-05). Every later mission is checked by
them. The findings are in [study
0002](../studies/0002-findings-of-1.0.0/README.md), §3.1.

## What changes once it is ruled

The owner's order, which rokh-work's README keeps; it was the list of what is
owed in rokh's AGENTS.md.

## Waits for

Nothing.

## Ruled when

The list says it.

## How it is ruled

The owner rules in rokh-docs, by a record in `rulings/` that answers D-02 and
changes the texts it changes. This record then says `status: ruled` and names
that ruling, and the missions it blocks no longer need it.
