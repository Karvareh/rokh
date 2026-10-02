# Working in docs/

This file is for anyone who changes `docs/`, person or program. What is here
is ruled: the texts of Rokh, the contract, and the register of the owner's
rulings. The [AGENTS.md of the repository](../AGENTS.md) holds for all of it;
this file adds what is particular to `docs/`.

## The law of this folder

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
6. **Source takes a new version up with its pin.** The source is measured
   against the texts here, at the SHA-256 that
   `source/rokh/conformance/texts.tsv` names, and a test fails while a text
   here differs from its pin. So a text changes together with its pin, and
   with the obligations and the code the new version asks for; the record of
   the ruling says what in the source changes with it, or that nothing does.

## Language

As in the rest of the repository. English, but for the treatise. A person is
a *person*, never a *user*; the ledger is *an individual's event ledger*. Use
the words the texts use, and coin no word where one of them serves. No name of
a person, machine, network address, home directory, product, organisation or
tool goes into a text. Commits are in English, in the present tense, and say
what changed and why; no trailer names a tool. History is never rewritten.

## Recording a ruling

1. Copy [templates/ruling.md](templates/ruling.md) to
   `rulings/NNNN-words.md`, with the next free number.
2. Fill it: the question it answers (`answers: D-nn` of `lab/`, or `none`),
   the ruling in the owner's words, what it changes, what it replaces.
3. Change the texts it changes, in the same pull request.
4. If it replaces a ruling, set that ruling's `status` to `replaced` and its
   `replaced-by` to the new number.
5. Run the check.

## Check

From the root of the repository:

```sh
cd .github/records && go vet ./... && go test ./... && go run . -root ../.. check
cd .github/records && go run . -root ../.. index    # writes the tables, the rulings' among them
```

`.github/records` is the one program for the records of `docs/`, `lab/` and
`work/`; the rules of each kind of record are in its `rules.go`.
