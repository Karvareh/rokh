---
id: W-01
title: "One short socket folder for every test that listens"
kind: mission
priority: p1
status: blocked
needs: D-01
lands-in: source
evidence: "A-01, S-15"
---

# W-01. One short socket folder for every test that listens

## Why

- **A-01.** 33 tests skip without a word in a fresh clone and in the checks:
  each looks for a short socket folder six or seven levels above its package,
  outside the tree; 16 of the core's, 17 of the gate's, among them the tests
  behind "Two booths on one protocol" and "Several doors on one carrier at
  once". Given a folder of 77 bytes or less the core's pass; of the gate's,
  twelve pass and five want the enclosure's sandbox; at 87 bytes eight fail on
  the socket path limit of about 104 bytes — *evidence: ✔ (the limits ◇);
  `rokh/daemon/booth_test.go:145-154`, `rokh/daemon/daemon_test.go:360`,
  `rokh/cmd/rokh/harness_test.go:254-259`, `rokh/proof/world_test.go:359`,
  `rokh-home/gate/gate_test.go:384-399`*

  Since the source moved into `source/`, each of these tests climbs one level
  more, to the same folder; the finding is otherwise as it was.
- **S-15.** Which conditional skips fired in the suites was not recorded —
  *evidence: R (§T)*

The findings are kept, with their evidence, in `lab/`, [study
0002](../../lab/studies/0002-findings-of-1.0.0/README.md); the measurements in
[study 0001](../../lab/studies/0001-scale/README.md).

## The change

One helper for a short socket folder, made by the test and removed after it,
with a variable to override it; used by every test that listens; no look
outside the tree.

## Where

`rokh/daemon/booth_test.go`, `rokh/daemon/daemon_test.go`,
`rokh/cmd/rokh/harness_test.go`, `rokh/proof/world_test.go`,
`rokh-home/gate/gate_test.go`.

Paths are of the source, under `source/`, unless they say otherwise.

## Waits for

- [**D-01**](../../lab/questions/D-01-socket-folders-and-the-report.md)
  (`lab/`): May a test make its socket folder outside t.TempDir(), and may
  the conformance report rewrite its STATE.md?

## Done when

On a fresh clone on Linux, run by the superuser and by an unprivileged account,
`go test -json ./...` in both modules shows no skip for a missing short folder;
the 33 tests run; any that fails is written into STATE.md with its cause, not
skipped.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
