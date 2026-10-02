---
id: D-03
title: "TCP at the loopback interface, and how a seed is reached from outside the host"
priority: p1
status: open
blocks: none
evidence: "A-36, A-43, R-03, Q-03"
ruling: none
---

# D-03. TCP at the loopback interface, and how a seed is reached from outside the host

## The question

TCP at the loopback interface: kept, with a ruling that the requirement under
Axiom 5 allows it and a base profile that names it (a new generation, 4.10); or
taken out of B1 and `transport`, and the tests moved to Unix sockets. And
whether a seed people reach lives inside Rokh or beside it (a courier, a
service of the host).

## Why it is asked

- **A-36.** A booth may listen on TCP at the loopback interface: the contract
  allows it (B1); N does not ("not one code path that opens a network socket",
  the requirement under Axiom 5; a door for programs on a Unix socket, the base
  profile of §4, which 4.10 changes only by a new generation); no ruling
  settles it — *evidence: ✔; `rokh/transport/transport.go:52-60` (`Listen`)*
- **A-43.** `docs/07` says no code path opens TCP, sealing is not built, and
  only the root discloses; version 1 does all three otherwise — *evidence: ✔*
- **R-03.** A seed others can reach — *evidence: R (N, the requirement under
  Axiom 5; T4.1)*
- **Q-03.** A seed served to people over a network is not in version 1: a booth
  listens on a Unix socket or on TCP at the loopback interface — *evidence: C
  (`transport.Listen`), R (N)*

The findings are kept, with their evidence, in [study
0002](../studies/0002-findings-of-1.0.0/README.md); the measurements in [study
0001](../studies/0001-scale/README.md).

## What changes once it is ruled

N, contract B1, `rokh/transport`, `docs/07`.

One sentence of `docs/07`, in
[**W-20**](../../work/missions/W-20-true-documents.md)
(work/): The documents of rokh made true, also waits for it; the rest of
that mission does not.

## Waits for

Nothing.

## Ruled when

The texts say it; code and tests follow.

## How it is ruled

The owner rules by a record in `docs/rulings/` that answers D-03 and
changes the texts it changes. This record then says `status: ruled` and names
that ruling, and the missions it blocks no longer need it.
