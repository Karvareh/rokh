# The home of Rokh

A home is where programs work with a person's ledger without ever touching
it. One trusted process, the gate, holds the home open and is the only way
in. Behind it: the sealed store, the owner's ledger, the catalogue of items,
the registry of programs, the keys the home holds for them, and the index.
Every read, write and handing-out is decided against the asking program's
authority before anything is read or done.

Two doors onto one ledger. The owner's door may sign with the root key and is
reached only by the owner, over a channel keyed from the owner's passphrase.
The programs' door never signs with the root: it signs with the key the home
holds for the asking program, under the grant the owner made for that key,
and the ledger judges that grant in the event's causal past. Neither door is
a socket a program can reach on its own.

## Packages

| package | what |
|---|---|
| `home` | the home as the gate holds it open: store, ledger, catalogue, registry, keys, index, and the decision before every act |
| `gate` | the one trusted process and its two doors; the booth of a home on `rokh.booth/1` |
| `authority` | who may do what in a home, and why: the registry of programs and the eight separate rights, none implying another |
| `archive` | bringing files, folders and projects in and handing them out: preview, copy, verify; nothing lost, nothing overwritten |
| `index` | the lexical index: a reading layer over exact bytes that folds what Persian writing varies in; visibility is decided before anything is counted |
| `sheet` | the provenance sheet of one version of one item: who wrote it, who recorded it, where it came from; a registrar's signature is never an authorship claim |
| `store`, `vesselstore` | the sealed keeping of the home's files and its ledger |
| `native` | an engine beside the core, not inside it: the home opens, reads, writes, searches, exports and closes with no engine present |
| `cmd/rokh-home` | `rokh-home init | serve | owner | program | version`; also reached as `rokh home ...` |

## Build and check

```sh
go build -trimpath -o ../dist/ ./cmd/...
go vet ./... && go test ./...
```

Builds for Linux, macOS and Android. One test is skipped because it fails;
`../STATE.md` names it and what else is owed: a large file through the home
takes memory several times its size, the engine is to move to a module of its
own, and the home does not yet build for Windows.
