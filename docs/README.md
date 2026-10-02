# docs: what is ruled

**Docs: what is ruled.** The texts of Rokh that the owner has accepted: the
two texts that specify Rokh, how they are read, the contract that fixes its
bytes, and the register of the owner's rulings. Source is measured against
these texts; the lab examines what they do not yet say; work carries out what
they ask.

| folder | standing |
|---|---|
| [`source/`](../source/README.md) | source: what runs |
| **`docs/`**, this one | docs: what is ruled |
| [`lab/`](../lab/README.md) | lab: examined, not ruled |
| [`work/`](../work/README.md) | work: to be examined or done |

## The texts

| file | text | cited as |
|---|---|---|
| [`رساله.md`](رساله.md) | The treatise, in Persian: the original and founding text of Rokh. Thirteen bands, ninety-one numbered propositions. | `T`, as in `T3.4` |
| [`without-consensus.md`](without-consensus.md) | The ledger without consensus: nine sections and seven axioms, an English rendering of the Persian original, with its numbering. | `N`, as in `N4.10` or `N-Axiom5` |
| [`conformance.md`](conformance.md) | How the two texts are read, cited and measured: which statements bind, and how code shows that it conforms. | |
| [`contracts/v1.md`](contracts/v1.md) | The contract of version 1: bytes, vessel, envelope, keyring, seed, booth. | `contract 2.6` |
| [`rulings/`](rulings/README.md) | The register: every ruling of the owner, with the question it answers and what it changed. | `ruling 0001` |

The treatise is the one Persian text of Rokh. It is the original, and it is
not translated here. A path named inside the texts, such as
`conformance/obligations.tsv` or `docs/08`, is a path of the source, under
`source/rokh/`.

## How a text here changes

Only by a ruling. A question is asked and examined in [`lab/`](../lab/README.md);
the owner rules; the ruling is recorded in `rulings/`, and the texts change in
the same pull request, which names it.

- A byte form is never edited. A change of one is a new generation with a name
  of its own, in a contract of its own, and the old one stays.
- A ruling in force is never edited. A ruling that changes it is a new record
  that says which ruling it replaces; the old one stays, marked replaced.
- An erratum changes no meaning: a sentence said more plainly, a broken
  reference, a typing error. It comes as a pull request, or as an issue with
  the erratum form, and says why it changes no meaning.

## The version source is measured against

Source reads the texts here, and names the version it is measured against by
the SHA-256 of each text, in
[`source/rokh/conformance/texts.tsv`](../source/rokh/conformance/texts.tsv); a
test of the source fails while a text here differs from its pin. So a text and
its pin change together, with the obligations and the code the new version
asks for. [CHANGELOG.md](CHANGELOG.md) lists every version of the texts with
the SHA-256 of each.

## Not here

Questions, studies, measurements and proposals are in [`lab/`](../lab/README.md).
Missions are in [`work/`](../work/README.md). The code, its own documents and
its `STATE.md` are in [`source/`](../source/README.md).

## Check

From the root of the repository:

```sh
cd .github/records && go vet ./... && go test ./... && go run . -root ../.. check
```

It checks every ruling's record, the table of rulings, and every relative link
of every document. `go run . -root ../.. index` writes the table again.

## License

Copyright (c) 2026 The Karvareh authors. The texts are free under the GNU
Lesser General Public License, version 3 or later, as the whole of this
repository is: [LICENSE](../LICENSE), with the GNU General Public License it
rests on in [GPL-3.0.txt](../GPL-3.0.txt). Whether they stay under it, or take
a license written for texts, is question
[D-23](../lab/questions/D-23-license-of-texts.md) of `lab/`, and it stays open
until the owner rules.
