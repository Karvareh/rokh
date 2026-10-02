---
id: W-12
title: "Bounds on what a booth reads before a session is bound"
kind: mission
priority: p2
status: blocked
needs: W-01
lands-in: source
evidence: "A-06, Q-17"
---

# W-12. Bounds on what a booth reads before a session is bound

## Why

- **A-06.** A booth reads and parses a message of up to 8 MiB before its
  session is bound, and its listener takes any number of connections; one run
  held about 2.3 GB for twelve connections — *evidence: ◇;
  `rokh/booth/booth.go:230-252` (`readLine`, the loop of a session),
  `rokh/transport/transport.go:124-140` (`Serve`)*
- **Q-17.** Resource consumption: a booth parses 8 MiB before binding and takes
  any number of connections; one small write makes every door read all content
  again; grants cost memory as the square, also a delegate's; an open address
  keeps every key's events; a large thing takes four times its size; a local
  process can hold the turn; none of it was run as an attack — *evidence: C, M
  (§M3, §M6, §M8, §M9)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

Before a session is bound, a booth reads at most a small message; a listener
serves a bounded number of connections at once; a bound session keeps B1's
8 MiB.

## Where

`rokh/booth/booth.go`, `rokh/transport/transport.go`, `rokh-home/gate`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**W-01**](W-01-short-socket-folder.md): One short socket folder for every
  test that listens

## Done when

A line of 1 MiB before `prove` is refused unparsed; connections past the bound
wait or are refused; a bound session still sends 8 MiB.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
