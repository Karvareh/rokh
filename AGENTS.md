# Working in this repository

This file is for anyone who changes rokh-docs, person or program. What is
here is ruled: the texts of Rokh, the contract, and the register of the
owner's rulings.

## The law of this repository

1. **Nothing here changes meaning without a ruling.** A pull request that
   changes what a text says names the ruling that allows it, and records that
   ruling in `rulings/` in the same pull request, in the owner's words. Only
   the owner rules. No program decides a ruling, and nobody settles an open
   one by writing it down here.
2. **A byte form is never edited.** A change of one is a new generation with
   a name of its own, in a contract of its own; the old contract stays.
3. **Nothing is erased.** A ruling replaced stays, marked `replaced`, and
   names the ruling that replaces it, which names it in turn.
4. **The treatise is not translated here.** It is the original, in Persian,
   and the one Persian text of Rokh.
5. **An erratum changes no meaning**: a sentence said more plainly, a broken
   reference, a typing error. It says why it changes no meaning.
6. **Source takes a new version up by a mission.** Changing a text here does
   not change rokh. The record of the ruling names the mission of rokh-work
   that brings the copies in `rokh/texts/`, their pin and the code to the new
   version, or says that nothing in rokh changes.

## Language

As in rokh. English, but for the treatise. A person is a *person*, never a
*user*; the ledger is *an individual's event ledger*. Use the words the texts
use, and coin no word where one of them serves. No name of a person, machine,
network address, home directory, product, organisation or tool goes into a
text. Commits are in English, in the present tense, and say what changed and
why; no trailer names a tool. History is never rewritten.

## Recording a ruling

1. Copy [templates/ruling.md](templates/ruling.md) to
   `rulings/NNNN-words.md`, with the next free number.
2. Fill it: the question it answers (`answers: D-nn` of rokh-lab, or `none`),
   the ruling in the owner's words, what it changes, what it replaces.
3. Change the texts it changes, in the same pull request.
4. If it replaces a ruling, set that ruling's `status` to `replaced` and its
   `replaced-by` to the new number.
5. Run the check.

## Check

```sh
cd tools/records && go vet ./... && go test ./... && go run . -root ../.. check
cd tools/records && go run . -root ../.. index    # writes the table of rulings
```

`tools/records/main.go` is the same program in rokh-docs, rokh-lab and
rokh-work; a change to it is made in all three. `rules.go` is this
repository's own.
