---
id: W-31
title: "Check the copies of the texts against rokh-docs in the checks"
kind: mission
priority: p2
status: withdrawn
needs: none
lands-in: source
evidence: "rokh/texts/README.md of rokh, and the CHANGELOG of rokh-docs"
---

# W-31. Check the copies of the texts against rokh-docs in the checks

## Withdrawn

The texts came back into this repository, in `docs/`, and the source keeps no
copies of them: the conformance tests read the texts in `docs/`, and the test
of the pin, `source/rokh/conformance/texts.tsv`, holds those texts to their
SHA-256. No copy is left to check against the texts where they are ruled, so
this mission is no longer asked. What it asked is kept below, as it was
written.

## Why

rokh keeps copies of the texts in `rokh/texts/`, pinned by their SHA-256 in
`texts.tsv`, and a test of `rokh/conformance` proves that every copy is the one
its pin names. Nothing yet proves that the pin names a version of the texts
that exists in rokh-docs, where they are ruled: a pull request could change a
copy and its pin together. A test never opens the network, so only a job of
the checks, which may, can refuse that. See rokh's `rokh/texts/README.md` and
the CHANGELOG of rokh-docs.

## The change

A job in rokh's checks that reads rokh-docs, finds the version of the texts
whose SHA-256 `rokh/texts/texts.tsv` names, and fails when a copy in
`rokh/texts/` differs from it or when no version of rokh-docs has those
digests; and that says, without failing, when rokh-docs holds a newer version
than the pin.

## Where

`.github/workflows/check.yml` of rokh; `rokh/texts/texts.tsv`.

Paths are of the source, under `source/`, unless they say otherwise.

## Waits for

Nothing.

Take it up once rokh-docs is public, or with a token that can read it.

## Done when

The job fails on a pull request that edits a copy in `rokh/texts/` together
with its pin when rokh-docs holds no version with those digests; it passes on
`main`; it names a newer version of the texts when rokh-docs has one. Until
rokh-docs is public, the job reads it with a token kept as a secret of rokh,
and says so in its comment.

## Before beginning

- Read the [AGENTS.md of `work/`](../AGENTS.md) and the [AGENTS.md of the
  repository](../../AGENTS.md).
- Ask the owner, on this mission's issue, whether a private repair is under way
  in the same files.
