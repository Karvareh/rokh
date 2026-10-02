# The texts, as this tree is measured against them

These are copies of the texts of
[rokh-docs](https://github.com/Karvareh/rokh-docs), where they are kept and
where they change:

| file | text | cited as |
|---|---|---|
| `رساله.md` | the treatise, in Persian: the founding text | `T` |
| `without-consensus.md` | the ledger without consensus, an English rendering of the Persian original | `N` |
| `conformance.md` | how the two texts are read, cited and measured | |
| `contracts/v1.md` | the contract of version 1: bytes, vessel, envelope, keyring, seed, booth | `contract` |

`go test ./conformance` reads the first two, and the code is built to all
four. The tests never open the network, so the texts they read are here.

They are never edited here. [`texts.tsv`](texts.tsv) names the SHA-256 of
each copy and the version of the texts it belongs to, and a test of
`rokh/conformance` refuses a copy that differs from its pin, a copy the pin
does not name, and a pin with no copy. A new version of the texts is taken up
from rokh-docs whole, by a mission of rokh-work: the copies, the pin, and the
obligations and code the new version asks for, in one change.
