# The register of rulings

Every ruling of the owner, one record each: the question it answers, the
ruling in the owner's words, what it changes in the texts, and what it
replaces. A ruling in force is never edited; a ruling that changes it is a new
record, and the old one stays, marked replaced.

<!-- records:rulings -->
No ruling is recorded yet.
<!-- /records:rulings -->

## Cited, and not recorded

These are cited in the source, in comments or in documents, as rulings,
goals or decisions, and no record of them is kept anywhere in this repository
([study 0002](../../lab/studies/0002-findings-of-1.0.0/README.md) of `lab/`,
findings A-33 and A-37). Each is to be recorded here by the owner, or its
citation in the source replaced by what it rests on: that is question
[D-05](../../lab/questions/D-05-cited-texts.md) of `lab/`.

| cited as | where | said to settle |
|---|---|---|
| the owner's word of 2026-09-29 | `source/rokh/cmd/rokh/v1_vessel.go`, the comment above `cmdReconcile` | that nothing reconciles without being asked, and that R6 of the contract is not built; R6 says otherwise (question D-04) |
| the owner's words of 2026-09-29 | `source/rokh/cmd/rokh/runs_test.go` and `twodoors_test.go`, the comments at their head | that several continuous booths are not to be confused with several temporary runs of the command line: both must hold, and each is proven on its own |
| the owner's ruling of 10 Shahrivar 1405 | `source/rokh/docs/08-sentences.md`, its head | that the sentences of the command line and the terminal are English, and that Persian stays for the treatise, the charter written in it, and the site |
| `goal 7.1` | the contract, G1 and A2 | that a copy of the folder made at any moment opens to the last complete commit |
| `packet C.3`, `DEFECT K1`, and the like | comments in the source | about forty-five citations in all, of texts that are kept nowhere |

## Recording one

Copy [the template](../templates/ruling.md) to `rulings/NNNN-words.md` with
the next free number, fill it in the owner's words, change the texts it
changes in the same pull request, and run the check of `.github/records`,
whose `index` also writes the table above.
