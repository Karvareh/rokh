# Rokh core — events, addresses and causal links

The core records an individual's events as a graph of addressable data. It
preserves signed records, their causal links and the authority under which
they were made; copies can develop independently and later be combined
without rewriting either history.

In this module: the byte grammar, the event, the ledger and its rule of
authority, the sealed vessel and the carrier around it, the keys and the views
they open, seeds and reconcile, the booth of a carrier, the sentence surface
and the screen, and the commands built on them.

Go, standard library only. No database, no framework, no hand-written
cryptography: SHA-256, HMAC, HKDF, PBKDF2, AES-256-GCM, Ed25519 and X25519
from Go's `crypto`. No listener is opened unless `rokh daemon` is asked for
one.

## Build and check

```sh
go build -trimpath -o ../dist/ ./cmd/...
go vet ./... && go test -timeout 45m ./...
go test ./conformance          # measures the code against T and N; writes conformance/STATE.md
go run ./cmd/rokh-shell -demo  # every screen, from a made-up ledger, recording nothing
```

## Commands

`rokh` on its own asks which folder is your Rokh, looks at what is on it, and
only then asks for the passphrase; `rokh FOLDER` skips the asking. That is the
sentence surface, which is what the program is for. Everything below is
plumbing: it exists for programs and for the awkward jobs, and every
subcommand answers `-h` with its own flags.

```
rokh init      DIR --message TEXT [--cold KEYFILE] [--size 64M] [--slab 1M] [--growth fixed|auto:STEP:MAX]
rokh write     DIR --address ADDR [--verb V] [--message TEXT] [--key NAME] [--branch B]
                   [--attempt NAME] [--expect-heads H1,H2] [--json]
rokh attempt   DIR --attempt NAME [--key NAME | --author HEX] [--json]
rokh key add   DIR --name NAME --reads A,B [--write --scope ADDR] [--key-passphrase-file F] [--attempt NAME]
rokh key list  DIR
rokh key show  DIR --key ID|NAME
rokh key revoke DIR --key ID|NAME [--attempt NAME]
rokh key rotate DIR --key ID|NAME [--key-passphrase-file F] [--attempt NAME]
rokh seed      SRC DST [--scope A,B] [--size 64M] [--slab 1M] [--attempt NAME]
rokh reconcile DIR OTHER [--merge]
rokh grow      DIR --to SIZE
rokh shrink    DIR --to SIZE [--finish]
rokh view      DIR
rokh grant     DIR --to NAME [--scope ADDR] [--verbs a,b] [--delegate] [--root-key FILE]
rokh revoke    DIR --target ID [--root-key FILE]
rokh merge     DIR --branch A --from B [--key NAME] [--root-key FILE]
rokh log       DIR [--long] [--json]
rokh verify    DIR
rokh branch    DIR [--create NAME --at ID]
rokh keys      DIR [--public] [--reading] [--export NAME --out FILE] [--drop NAME]
rokh share     DIR --to HEXKEY [--scope ADDR | --room ID] [--seal-to HEXKEY] [--root-key FILE]
rokh unshare   DIR --target ID [--root-key FILE]
rokh covenants DIR [--at ID]
rokh bond leaf     DIR --with ANCHORS --doing TEXT --out FILE
rokh bond accept   DIR --leaf FILE --place TEXT [--branch B] [--root-key FILE]
rokh bond standing DIR --leaf FILE
rokh bind      --socket PATH --namespace NS --version V --can VERB[,VERB] ...
rokh bound     --socket PATH
rokh announce  DIR
rokh show      DIR
rokh daemon    DIR (--socket PATH | --listen unix:PATH|tcp:127.0.0.1:PORT | --stdio) [--read-only] [--no-sign] [--no-root]
rokh version
```

The passphrase: `--passphrase-file FILE` (`-` for stdin), then
`ROKH_PASSPHRASE_FILE`, then `ROKH_PASSPHRASE`, otherwise asked for on the
terminal. An event is only ever created by one of these commands; nothing is
recorded until you record it.

`rokh-shell` is the sentence surface on its own. `rokh-courier` moves
already-signed bytes between two ledgers, holds no key and is trusted with
nothing. `rokh-forms` is a thin local adapter over the booth, not part of
Rokh. `rokh-chest` keeps a Rokh in one file instead of one folder, on Linux.

## Layout

Layers point downwards only; `arch/` holds a test that says so.

| package | what |
|---|---|
| `frame` | the byte grammar: one canonical form, self-delimiting and versioned |
| `canon` | one encoding per value, written down rather than borrowed |
| `event` | builds, reads and checks events: six questions and nothing else |
| `generation` | how Rokh changes without rewriting its past |
| `oracle` | the witnesses present when an event is made |
| `ledger` | the ledger, and authority judged in the causal past |
| `lineage` | how a ledger spreads and meets itself: seeds and reconcile |
| `seal` | content closed for a named reader |
| `key`, `keyview`, `passphrase` | envelopes, slot cells, the view a passphrase opens, the one passphrase rule |
| `vessel` | the keeping layer: equal-sized encrypted slabs, four head files, one commit point |
| `carrier` | the bag: a folder whose whole footprint is one vessel |
| `chest` | the other carrier profile: one file holding an encrypted filesystem |
| `medium`, `turn`, `transport` | the host adapters: files, the writer's lock, the wire |
| `working` | the boundary between doing and recording |
| `content` | the minimal content descriptor: hash, size, type |
| `covenant`, `bundle`, `selective` | what may be disclosed, what may cross, one field shown alone |
| `announce` | the narrow path: news of an event, never the event |
| `receipt`, `harness`, `answer` | the unit of work a program does on Rokh, the covenant it binds, what it owes when it answers |
| `bond` | the lasting knot between independent owners |
| `booth`, `daemon` | the protocol `rokh.booth/1`, and the booth of a carrier over a socket |
| `shell`, `tui` | the sentence surface, and the screen |
| `size`, `arch`, `bench`, `proof` | how a number of bytes is written; the layering test; measurements; the adversarial witness |
| `conformance` | the code measured against the two specification texts |

## Documents

| | |
|---|---|
| [`docs/`](../../docs/README.md) of the repository | The texts this code is measured against, kept there and pinned by SHA-256 in `conformance/texts.tsv`: |
| [`رساله.md`](../../docs/رساله.md) | The treatise, in Persian: the founding text. Cited as `T`. |
| [`without-consensus.md`](../../docs/without-consensus.md) | The ledger without consensus, an English rendering of the Persian original. Cited as `N`. |
| [`conformance.md`](../../docs/conformance.md) | How the two texts are read, cited and measured. |
| [`contracts/v1.md`](../../docs/contracts/v1.md) | The contract of version 1. |
| `docs/01-grammar.md` | The byte grammar. |
| `docs/02-authority.md` | Authority, and the monotonicity theorem. |
| `docs/03-carrier.md` | The carrier. |
| `docs/04-bandwidth.md` | The narrow path and the wide one. |
| `docs/05-inventory.md` | Everything Rokh creates, and nothing else. |
| `docs/06-api.md` | The booth API, as it was before version 1; it says so. |
| `docs/07-peering.md` | Disclosure, couriers, content. |
| `docs/08-sentences.md` | The sentence shell: eight verbs, one human surface. |
| `docs/10-terminal.md`, `tui/README.md` | The screen. |

## Deliberately absent

Radio drivers, mirrors of a peer's ledger, fragmentation, queryable indexes,
and any database. Each is named as absent rather than stubbed.

Automation of Rokh's own is absent too, and permanently: there is no timer,
no watcher, no poller and no background goroutine that writes. Only an
explicit request puts an event into the ledger. A program that works on Rokh
binds a covenant with `rokh bind` and leaves receipts, an intent before the
step and a result after it, both of them real signed events. What Rokh
refuses is to act on its own, not to carry what a program did.

## Open rulings

Seven belong to the specification itself and are the owner's to settle;
nothing in the code settles them quietly: the byte form of selective
disclosure (T13.1, T7.5); root key rotation (T13.2), which also blocks part
of the bond (T11.8); a mirror between two owners and the reader of a bond
(T13.4); succession (T13.6); and what to do about a full-name collision
(T13.8). `conformance/STATE.md` lists them as they stand.
