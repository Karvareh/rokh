# The conformance map

This folder adds nothing to Rokh. **It measures.**

## What it measures against

The map reads the two specification texts, the treatise (`T`) and the ledger
without consensus (`N`), where they are kept: [`docs/`](../../../docs/README.md)
of this repository. `texts.tsv` pins the version of the texts this code is
measured against, by the SHA-256 of each, and a test fails while a text in
`docs/` differs from its pin. A text changes in `docs/` by a ruling; the code
takes the new version up by changing the pin, with the obligations and the
code the new version asks for, in one change.

## Why it was written again

The first version had two structural faults, and a review caught both:

1. **The denominator was chosen by hand.** Every proposition got one class
   (ruling, profile or report), and thirty-five rulings had been counted as
   "reports" by mistake. The number counted in its own favour.
2. **"The number appears in a test" stood in for "the behaviour is
   measured".** A test that named a ruling but did not exercise its
   behaviour passed the map.

## The new shape: obligations, not numbers

Every row of `obligations.tsv` is an **obligation**, not a number. A compound
proposition has several rows: `T13.1` is both the sealing of a bundle that is
not yet built and an open byte form, and reading it as one hides one of the
two.

Every obligation says: from which proposition · which part · what kind ·
what status · the **production path** (`path.go:Symbol`) · the **test**
(`pkg:TestName`).

## Six statuses

| | |
|---|---|
| `met` | a production path exists, and a test exists that **itself says which ruling it is for** |
| `owed` | the ruling is closed and the work is owed — **the one status that never closes** |
| `ruling-open` | band 13 of the treatise leaves the ruling itself open |
| `outside` | deliberately outside the design |
| `field` | not a question for the code; it is answered by use |
| `note` | a report, not an obligation |

## What it refuses

- a proposition with no obligation written for it, or an obligation that
  names no proposition
- `met` without a production path, or with a path that is not in the tree
- `met` with a test that does not exist
- **`met` with a test whose own comment does not say which ruling it is
  for** — otherwise the pairing lives only in this file and the code knows
  nothing of it
- **`met` that rests on a text-searching test alone** (`weak.tsv`)
- any evidence-bearing obligation that is `owed`

## Text-searching tests

`weak.tsv` names the tests that search the source text. They prove that a
**word** is absent, not that a **behaviour** is. They stay as alarms; they
are not evidence.

## State

`go test ./conformance` writes `STATE.md`. Nobody writes it by hand.
