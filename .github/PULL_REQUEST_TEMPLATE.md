## What changed

<!-- The mission of work/ it carries out, if any: W-nn or I-nn. -->

## Why

## How it was checked

<!-- Keep the lines of the folders the change touches. -->

**Every change**
- [ ] `cd .github/records && go run . -root ../.. check` passes
- [ ] no name, machine, address or path that is not the code's own

**source/**
- [ ] `cd source/rokh && go vet ./... && go test ./...`
- [ ] `cd source/rokh-home && go vet ./... && go test ./...`
- [ ] `source/STATE.md` still tells the truth about the source

**docs/**
- [ ] It changes no meaning (an erratum), or it records the ruling that allows it in `docs/rulings/`, in the owner's words.
- [ ] No byte form is edited; a new generation has a name of its own.
- [ ] A ruling it replaces is marked `replaced` and names this one.
- [ ] A text it changes is taken up in the same change: its pin, `source/rokh/conformance/texts.tsv`, and the obligations and the code the new version asks for.

**lab/**
- [ ] Every statement says whether it was measured, read in the code, extrapolated or proposed.
- [ ] Every measurement names its command, its host and the commit it ran against, and its raw output is kept beside it with its SHA-256.
- [ ] Nothing reads as a ruling; what needs one is a question.
- [ ] Nothing describes a weakness in security that is not yet repaired.

**work/**
- [ ] Every mission it adds or changes says why, with its evidence; the change; where; what it needs; and the test that says it is done.
- [ ] A status set to `done` names the merged commit; one set to `ready` names the ruling or the mission that unblocked it.
