# Working in lab/

This file is for anyone who changes `lab/`, person or program. The
[AGENTS.md of the repository](../AGENTS.md) holds for all of it; this file adds
what is particular to `lab/`.

## The law of this folder

1. **Nothing here is a ruling, and nothing here reads as one.** A proposal
   says it is proposed; a question says what it asks. Only the owner rules, and
   a ruling is recorded in `docs/rulings/`, never here.
2. **Every statement says its kind:** measured, read in the code,
   extrapolated, or proposed. A number names its command, its host and the
   commit it ran against, and its raw output is kept beside it.
3. **Evidence is kept as it was written.** A raw output is not edited. If
   something must be taken out of one, such as a product's name, the study
   says what and why, and gives the SHA-256 of the file as written and as
   kept.
4. **A weakness in security is never written here before it is repaired.** It
   is reported privately, as the security policy says. A study may say that
   findings were withheld, and how many, and nothing more.
5. **A study is not changed to say something else.** A correction is a later
   commit that says what it corrects; a study overtaken is marked
   `superseded`, and stays.
6. **A limit of a version is never called a limit of Rokh**, unless the texts
   make it one.
7. **A question names what it blocks**, and the missions of `work/` that wait
   for it name it in turn.

## Language

As in the rest of the repository. English. A person is a *person*, never a
*user*; the ledger is *an individual's event ledger*. Use the words the texts
use, and coin no word where one of them serves; a word a study must coin says
that it is the study's. No name of a person, machine, network address, home
directory, product, organisation or tool goes into a study. Commits are in
English, in the present tense; no trailer names a tool. History is never
rewritten.

## Check

From the root of the repository:

```sh
cd .github/records && go vet ./... && go test ./... && go run . -root ../.. check
cd .github/records && go run . -root ../.. index    # writes the tables, those of studies and questions among them
```

`.github/records` is the one program for the records of `docs/`, `lab/` and
`work/`; the rules of each kind of record are in its `rules.go`.
