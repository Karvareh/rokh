---
id: W-22
title: "A passphrase prompt on Windows, and builds that stop at a failure"
kind: mission
priority: p3
status: blocked
needs: W-03
lands-in: source
evidence: "A-30, A-31"
---

# W-22. A passphrase prompt on Windows, and builds that stop at a failure

## Why

- **A-30.** `rokh/build.sh` builds for neither Windows nor Android, and passes
  over a target that fails to build; the core cross-builds for both —
  *evidence: ✔ (the cross-build ◇); `rokh/build.sh:41-68`*
- **A-31.** The prompt for a passphrase opens `/dev/tty` and runs `/bin/stty`:
  on Windows nothing is asked — *evidence: C;
  `rokh/passphrase/passphrase.go:122, 151`*

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## The change

A passphrase prompt for Windows; `build.sh` builds Windows and Android and
stops at a failed target.

## Where

`rokh/passphrase/passphrase.go`, `rokh/build.sh`.

Paths are of the source, under `source/`, unless they say otherwise.

## Waits for

- [**W-03**](W-03-checks.md): Checks that see formatting, other systems, skips
  and changed files

## Done when

The builds run in the checks; the prompt is tested through what the host
adapter fills.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
