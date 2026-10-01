# Working in this repository

This file is for anyone who changes rokh-lab, person or program.

## The law of this repository

1. **Nothing here is a ruling, and nothing here reads as one.** A proposal
   says it is proposed; a question says what it asks. Only the owner rules, and
   a ruling is recorded in rokh-docs, never here.
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
7. **A question names what it blocks**, and the missions of rokh-work that
   wait for it name it in turn.

## Language

As in rokh. English. A person is a *person*, never a *user*; the ledger is
*an individual's event ledger*. Use the words the texts use, and coin no word
where one of them serves; a word a study must coin says that it is the
study's. No name of a person, machine, network address, home directory,
product, organisation or tool goes into a study. Commits are in English, in
the present tense; no trailer names a tool. History is never rewritten.

## Check

```sh
cd tools/records && go vet ./... && go test ./... && go run . -root ../.. check
cd tools/records && go run . -root ../.. index    # writes the tables of studies and questions
```

`tools/records/main.go` is the same program in rokh-docs, rokh-lab and
rokh-work; a change to it is made in all three. `rules.go` is this
repository's own.
