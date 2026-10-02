---
id: D-01
title: "May a test make its socket folder outside t.TempDir(), and may the conformance report rewrite its STATE.md?"
priority: p1
status: open
blocks: W-01
evidence: "A-01, A-32"
ruling: none
---

# D-01. May a test make its socket folder outside t.TempDir(), and may the conformance report rewrite its STATE.md?

## The question

May a test make its socket folder outside `t.TempDir()`, whose paths pass the
socket limit of about 104 bytes? may `go test ./conformance` keep rewriting
`conformance/STATE.md`, as AGENTS.md says it does, though a test writes only
inside `t.TempDir()`?

## Why it is asked

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
- **A-32.** Tests go against AGENTS.md's rules: 19 sleeps in 14 test files;
  `TestReport` rewrites `conformance/STATE.md` in its package folder, as
  AGENTS.md itself says `go test ./conformance` does; tests open TCP on the
  loopback interface — *evidence: ✔ (counted again for this plan)*

The findings are kept, with their evidence, in [study
0002](../studies/0002-findings-of-1.0.0/README.md); the measurements in [study
0001](../studies/0001-scale/README.md).

## What changes once it is ruled

AGENTS.md's rules for tests; W-01.

## What it blocks in work/

- [**W-01**](../../work/missions/W-01-short-socket-folder.md)
  (work/): One short socket folder for every test that listens

## Waits for

Nothing.

## Ruled when

AGENTS.md says it.

## How it is ruled

The owner rules by a record in `docs/rulings/` that answers D-01 and
changes the texts it changes. This record then says `status: ruled` and names
that ruling, and the missions it blocks no longer need it.
