# Rokh — a graph of addressable data

An individual's event ledger, built as a graph of addressable data that grows
across independent copies.

```
write at home/journal: I walked to the river.
```

Rokh holds the sentence in working state. Nothing is recorded yet; what the
record would be is shown.

```
write
```

That records an event in an individual's ledger: a graph of addressable data.
Each event names what it concerns, who recorded it, under whose authority, and
which events it follows. The graph grows without rewriting its past. Copies of
the same ledger can develop independently and be brought together when their
owner chooses. Updates can travel in stages, without keeping every copy in
lockstep or requiring all devices to be online together. Rights are
asymmetric: permission in one direction implies no permission in the other.
Causal links establish order; meaning and judgement remain with the person.

Rokh is not a service, not an account, not a network and not a judge. It
records what was done, by whom, under what right, after what. Whether it was
true, or wise, is not its question.

## Implementation in version 1

- **A carrier.** An ordinary folder holding a sealed vessel: fixed in size or
  growing in whole slabs. Opened without Rokh it shows no name, no address
  and no count. Copy it, carry it, put it on a stick.
- **One owner.** One passphrase, which has no recovery. Keys beside it, each
  with its own passphrase, read or write where the owner said; each key opens
  its own view and nothing more. A key is taken back or rotated by the owner.
- **Seeds.** An independently usable, recorded copy of the same ledger, whole
  or a slice. Two seeds are reconciled only when the owner commands it; where
  both changed the same thing, both are shown and nothing is chosen.
- **Booths.** An access point through which a program, or a person with a
  key, works with the ledger over one protocol, `rokh.booth/1`: the booth of
  a carrier (`rokh daemon`) and the gate of a home (`rokh-home`), where
  programs sit behind the gate and hold only what the owner granted them.
- **Complete without a network.** Nothing in the core opens a listener or
  needs a peer. A courier carries signed bytes between two ledgers; it holds
  no key and is trusted with nothing.

## Build

Go 1.26 or later, and nothing else. From this folder, `source/`:

```sh
cd rokh      && go build -trimpath -o ../dist/ ./cmd/...
cd rokh-home && go build -trimpath -o ../dist/ ./cmd/...
```

The commands are then in `dist/`: `rokh`, `rokh-shell`, `rokh-courier`,
`rokh-forms`, `rokh-chest` (Linux only) and `rokh-home`. The core builds for
Linux, macOS, Windows and Android; the home for Linux, macOS and Android. To
see the screen without making anything:

```sh
cd rokh && go run ./cmd/rokh-shell -demo
```

## Check

```sh
cd rokh      && go vet ./... && go test -timeout 45m ./...
cd rokh-home && go vet ./... && go test ./...
```

Three tests are skipped in this tree because they fail; they are named in
[STATE.md](STATE.md), which says what is met, what is not, and what was never
run. `go test ./conformance` in `rokh/` measures the code against the two
texts that specify it, as [`docs/`](../docs/README.md) holds them at the
version `rokh/conformance/texts.tsv` pins, and writes
`rokh/conformance/STATE.md`.

## The other three folders

This folder is the source: what runs. The repository keeps Rokh's other three
standings beside it, as [its README](../README.md) says: what is ruled in
[`docs/`](../docs/README.md), what is examined and not ruled in
[`lab/`](../lab/README.md), and what is to be examined or done in
[`work/`](../work/README.md).

## Where things are

| | |
|---|---|
| [`../docs/`](../docs/README.md) | The texts this tree is measured against, kept in `docs/` of the repository and pinned by SHA-256 in `rokh/conformance/texts.tsv`: the treatise, in Persian, the founding text of Rokh, cited as `T`; the ledger without consensus, an English rendering of its Persian original, cited as `N`; how the two are read; the contract of version 1. |
| [`rokh/docs/`](rokh/docs/) | Grammar, authority, carrier, bandwidth, inventory, the booth API, peering, the sentence surface, the screen. |
| [`rokh/`](rokh/README.md) | The core: packages, commands and layers. |
| [`rokh-home/`](rokh-home/README.md) | The home and its gate. |
| [`STATE.md`](STATE.md) | What this tree is and is not. |
| [`../AGENTS.md`](../AGENTS.md) | The working rules for anyone, person or program, who changes the repository. |

## Contributing and license

Proposals come as pull requests from a fork; what enters `main` is decided by
the owner of the repository. See [CONTRIBUTING.md](../.github/CONTRIBUTING.md);
work to pick up is in [`work/`](../work/README.md), and questions for the owner
are asked in [`lab/`](../lab/README.md).

Copyright (c) 2026 The Karvareh authors. Rokh is free software under the GNU
Lesser General Public License, version 3 or later: [LICENSE](../LICENSE), with
the GNU General Public License it rests on in [GPL-3.0.txt](../GPL-3.0.txt).
