---
id: W-07
title: "A courier that opens a session and says when it is refused"
kind: mission
priority: p2
status: blocked
needs: W-01
lands-in: source
evidence: "A-11, A-14"
---

# W-07. A courier that opens a session and says when it is refused

## Why

- **A-11.** `rokh-courier apply` sends `status` and `append` without `hello`;
  `rokh daemon` refuses with `hello_first`; the courier reports "0 accepted, 1
  rejected" and exits 0 — *evidence: ✔;
  `rokh/cmd/rokh-courier/main.go:279-305, 388`*
- **A-14.** No tests at all in `rokh/transport`, `rokh/cmd/rokh-courier`,
  `rokh/cmd/rokh-forms`, `rokh/cmd/rokh-chest`, `rokh-home/cmd/rokh-home` —
  *evidence: ✔*

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## The change

`rokh-courier apply` opens a session (`hello`, `prove`) and exits non-zero,
with the code, when anything is refused.

## Where

`rokh/cmd/rokh-courier/main.go`, a test beside it.

Paths are of the source, under `source/`, unless they say otherwise.

## Waits for

- [**W-01**](W-01-short-socket-folder.md): One short socket folder for every
  test that listens

## Done when

Against `rokh daemon` on a short socket: events recorded; a refused one gives a
non-zero exit and its code.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
