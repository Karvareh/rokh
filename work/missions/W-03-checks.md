---
id: W-03
title: "Checks that see formatting, other systems, skips and changed files"
kind: mission
priority: p1
status: blocked
needs: W-01
lands-in: source
evidence: "A-05, A-30, S-15"
---

# W-03. Checks that see formatting, other systems, skips and changed files

## Why

- **A-05.** The checks run on Linux alone (AGENTS.md asks Linux and macOS),
  with no format check, no build for another system, no count of skips and no
  look for files a run changed — *evidence: ✔; `.github/workflows/check.yml`*
- **A-30.** `rokh/build.sh` builds for neither Windows nor Android, and passes
  over a target that fails to build; the core cross-builds for both —
  *evidence: ✔ (the cross-build ◇); `rokh/build.sh:41-68`*
- **S-15.** Which conditional skips fired in the suites was not recorded —
  *evidence: R (§T)*

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

The checks: `gofmt -l` empty; `go vet`; both suites; `git diff --exit-code`
after them; skips counted from `go test -json` against a list kept in the tree;
a macOS run; builds for every target of `rokh/build.sh` and for Windows and
Android; what the gate's enclosure tests need.

## Where

`.github/workflows/check.yml`; a list of allowed skips; `rokh/build.sh`.

Paths are of rokh unless they say otherwise.

## Waits for

- [**W-01**](W-01-short-socket-folder.md): One short socket folder for every
  test that listens

## Done when

The checks fail on an unformatted file, on a file a run changed, on a skip not
in the list, on a target that does not build; they pass on the branch with only
the known skips listed.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
