---
id: W-10
title: "Six defects of the sentence surface"
kind: mission
priority: p2
status: ready
needs: none
lands-in: rokh
evidence: "A-15 to A-20"
---

# W-10. Six defects of the sentence surface

## Why

- **A-15.** `see the grants` ends in a panic, exit 2, on a ledger that holds an
  open grant — *evidence: ✔; `rokh/shell/ops.go:431` (`g.Subject[:4]`)*
- **A-16.** After a waiting sentence is rewritten, the answer says `write`
  records it; `write` records another waiting sentence, and the rewritten one
  is let go when the session closes (T9.2) — *evidence: ✔;
  `rokh/shell/ops.go:108-128`*
- **A-17.** The guides lose every `key: `, taken for the name of a layer by the
  expression that tidies errors — *evidence: ✔; `rokh/shell/plain.go:197-209`
  (`packagePrefix`)*
- **A-18.** Some refusals end with exit 0 and no code, a failed `bring` among
  them — *evidence: ◇*
- **A-19.** `see the ledgers` prints names without escaping them — *evidence:
  ◇*
- **A-20.** `-library` changes nothing — *evidence: ◇*
- See **A-15**.
- See **A-16**.
- See **A-17**.
- See **A-18**.
- See **A-19**.
- See **A-20**.

The findings are kept, with their evidence, in rokh-lab, [study
0002](https://github.com/Karvareh/rokh-lab/blob/main/studies/0002-findings-of-1.0.0/README.md);
the measurements in [study
0001](https://github.com/Karvareh/rokh-lab/blob/main/studies/0001-scale/README.md).

## The change

The six defects of the sentence surface.

## Where

`rokh/shell/ops.go`, `rokh/shell/plain.go`, `rokh/cmd/rokh-shell`,
`rokh/docs/08-sentences.md`.

Paths are of rokh unless they say otherwise.

## Waits for

Nothing.

## Done when

`see the grants` prints an open grant; after a rewrite, `write` records the
rewritten sentence (T9.2); the guides keep `key: `; every refusal exits
non-zero with a code; names are escaped; `-library` works or is gone from the
code and the document.

## Before beginning

- Read this repository's [AGENTS.md](../AGENTS.md), and the AGENTS.md of rokh.
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
