---
id: D-11
title: "May the home act at an offer's end without being asked then?"
priority: p3
status: open
blocks: none
evidence: "A-35"
ruling: none
---

# D-11. May the home act at an offer's end without being asked then?

## The question

May the home act at an offer's end without being asked at that moment (T4,
T4.1, law 3), or does the end take effect at the next request?

## Why it is asked

- **A-35.** An offer of hosting with an end arms a timer in the gate; when it
  fires, the gate takes the hosting back, stops the programs and seals the
  engine's last state, at a moment nobody asked for — *evidence: ◇, then C;
  `rokh-home/gate/server.go:89-100`, `rokh-home/gate/hosting.go:191`
  (`RevokeHosting`)*

The findings are kept, with their evidence, in [study
0002](../studies/0002-findings-of-1.0.0/README.md); the measurements in [study
0001](../studies/0001-scale/README.md).

## What changes once it is ruled

`rokh-home/gate`.

## Waits for

Nothing.

## Ruled when

The gate does what the ruling says, with a test.

## How it is ruled

The owner rules in rokh-docs, by a record in `rulings/` that answers D-11 and
changes the texts it changes. This record then says `status: ruled` and names
that ruling, and the missions it blocks no longer need it.
