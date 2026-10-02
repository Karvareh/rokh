---
id: I-03
title: "Wasteful use of resources, as probes"
kind: investigation
priority: p2
status: ready
needs: none
lands-in: lab
evidence: "Q-17, A-06"
---

# I-03. Wasteful use of resources, as probes

## Why

- **Q-17.** Resource consumption: a booth parses 8 MiB before binding and takes
  any number of connections; one small write makes every door read all content
  again; grants cost memory as the square, also a delegate's; an open address
  keeps every key's events; a large thing takes four times its size; a local
  process can hold the turn; none of it was run as an attack — *evidence: C, M
  (§M3, §M6, §M8, §M9)*
- **A-06.** A booth reads and parses a message of up to 8 MiB before its
  session is bound, and its listener takes any number of connections; one run
  held about 2.3 GB for twelve connections — *evidence: ◇;
  `rokh/booth/booth.go:230-252` (`readLine`, the loop of a session),
  `rokh/transport/transport.go:124-140` (`Serve`)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## What to find out

Wasteful use, as probes on a scratch carrier: messages before binding;
connections; a writer that makes every door read again; grants by a delegate;
an open address filled by many keys; a process holding the turn; growth to the
maximum.

What is found is written as a study of rokh-lab, with its raw data, its
commands, its host and the commit it ran against; benchmarks and probes that
stay land in rokh.

## Where

probes, then tests.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

A table of what each costs the one who does it and the door, before and after
W-12.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of
  rokh-lab.
