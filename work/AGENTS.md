# Working in this repository

This file is for anyone who changes rokh-work, or carries out one of its
missions, person or program.

## Carrying out a mission

- **The repository a mission lands in has its own law.** For rokh it is
  rokh's AGENTS.md: read it first. Its rules bind the work there, and nothing
  here relaxes them.
- **One mission, one branch**, from `main` of the repository it lands in.
  Small commits, each buildable and tested, each saying what changed and why,
  in English, in the present tense. No trailer names a tool.
- **A mission is done when its test passes as written.** If the test cannot
  be run as written, or would pass without the change, say so on the issue
  before going on.
- **Nothing here is a ruling.** A mission that meets an open ruling stops
  there, and a question goes to rokh-lab. No mission settles a ruling in code.
- **Ask before beginning** a mission that lands in rokh whether a private
  repair is under way in the same files. Anything of the kinds the security
  policy names goes to private reporting, never into a mission, a commit, a
  pull request or an issue.
- **A finding marked ◇** in rokh-lab is reproduced before it is repaired. If
  it does not reproduce, say so on the issue; the study is corrected by a
  later commit that says so.
- **A mission about scale is measured before and after**, with the same
  benchmark, command and host, and the raw outputs are kept in a study of
  rokh-lab with their commit and their SHA-256.
- **No mission changes a byte form.** If one turns out to, it stops and
  becomes a question: a byte form changes only by a new generation, after a
  ruling.
- **A test never opens the network, never sleeps for time to pass, and never
  writes outside `t.TempDir()`**, until a ruling says otherwise (D-01).
- **When a mission is done,** rokh's STATE.md changes in the same change as
  the work, where the work lands in rokh; the mission's record here and the
  state of its findings in rokh-lab change by a pull request of each.

## Changing the records

- A record changes by a pull request. Its identifier never changes and is
  never used again; a mission no longer wanted is `withdrawn`, and stays.
- A new mission takes the next free number of its kind, and says, as every
  mission does: why, with its evidence; the change; where; what it needs; and
  the test that says it is done.
- A question of rokh-lab that is ruled is struck from the `needs` of the
  missions it blocked, in the pull request that sets them `ready`, which names
  the ruling.

## Language

As in rokh. English. A person is a *person*, never a *user*; the ledger is
*an individual's event ledger*. Use the words the texts use, and coin no word
where one of them serves. No name of a person, machine, network address, home
directory, product, organisation or tool goes into a record. History is never
rewritten.

## Check

```sh
cd tools/records && go vet ./... && go test ./... && go run . -root ../.. check
cd tools/records && go run . -root ../.. index    # writes the table of missions
```

`tools/records/main.go` is the same program in rokh-docs, rokh-lab and
rokh-work; a change to it is made in all three. `rules.go` is this
repository's own.
