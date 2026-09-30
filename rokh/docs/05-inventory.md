# File inventory: everything Rokh creates, and nothing else

> This is the **complete** list of what Rokh puts on disk.
> `carrier/inventory_test.go` executes this list: any new entry anyone adds
> later breaks that test.

## 1. Inside the carrier

```
<carrier>/
  rokh.json
  .rokh/
    objects/<aa>/<30 hex>
    refs/<32 hex>
```

### `rokh.json`

| | |
|---|---|
| **what** | the carrier's public face: version, anchor, KDF parameters, sealed identity block, a human note |
| **required** | yes. Without it the directory is not a Rokh carrier |
| **created** | once, by `carrier.Create` |
| **written by** | `Create` and `SetSecrets`/`ForgetSecrets` only |
| **encrypted** | partly. Anchor and KDF parameters are **in the clear** (a client must recognize the carrier before it has a passphrase); the keyring is inside `sealed` under AES-256-GCM |
| **safe to delete** | **no.** Without the keyring nothing opens, and it is not reconstructible |

### `.rokh/objects/<aa>/<...>`

| | |
|---|---|
| **what** | one event, sealed. Its name is `HMAC(nameKey, id)[:16]`; the first two hex digits become a directory so no single directory holds millions of entries |
| **required** | yes. These are the ledger |
| **created** | whenever an event is **accepted**. A rejected event never reaches the carrier |
| **written by** | `Carrier.Put` only |
| **safe to delete** | **no.** The ledger is append-only; deleting an object cuts the chain |
| **regenerable** | only from another carrier that holds the same event |

### `.rokh/refs/<...>`

| | |
|---|---|
| **what** | one branch: a name and its head, sealed. The on-disk name is `HMAC(nameKey, "ref:"+name)`, so branch names are not visible on disk either |
| **required** | **no.** Branches are a human convenience; the real heads are always computed by re-reading the ledger (`Ledger.Heads`) |
| **created** | by `SetRef`, when writing on a branch or creating one |
| **written by** | `Carrier.SetRef` only |
| **safe to delete** | **yes, harmlessly.** Delete every ref and the ledger is untouched and `verify` is still clean. You lose names, not events |

### `.rokh-tmp-*` (transient)

| | |
|---|---|
| **what** | the temp file of an atomic write, in the destination directory |
| **created** | on every write; it disappears at `rename` |
| **survives** | only if power is cut mid-write |
| **safe to delete** | **yes, always.** `List` ignores them, so they are neither counted as objects nor treated as errors (`TestOrphanTempIsIgnored`) |

## 2. Outside the carrier

| entry | created by | why | safe to delete |
|---|---|---|---|
| **the carrier directory** | `rokh init` via `MkdirAll` | it has to exist | yes - that is removing the carrier |
| **the cold key file** | `rokh init --cold` or `rokh keys --export` | root custody on separate media, mode 0600 | **no** - it is the root key. Never overwritten: both commands refuse an existing path |
| **the daemon socket** | `rokh daemon --socket` | the local API, mode 0600 | yes; it is recreated on start |
| **`./rokh`** | `go build` | the binary | yes; it is in `.gitignore` |

**And nothing else.** Rokh creates nothing in `~/.config`, `~/.cache`, `/tmp`,
or anywhere else. No config file, no log, no lock, no automatic backup.

## 3. What is deliberately absent

| absent | why |
|---|---|
| **cache or index** | "state is always rebuilt by re-reading the ledger." A stored cache is a second source of truth. An index may come later, but it must be **discardable** and sit outside the carrier |
| **database** | a mutable file beneath an immutable ledger |
| **log** | anything worth recording is an event |
| **lock file** | one writer at a time, by human discipline. A concurrency lock arrives when it becomes a real problem |
| **state or turn file** | state is derived from the ledger |
| **`.rokh/README.txt`** | **removed in the audit.** The same text is already in the `note` field of `rokh.json`, and two copies of one sentence is one too many |
| **automatic backup** | Rokh says *what* to back up, not *how* |

## 4. Reversibility

```
rokh.json
.rokh/
```

`carrier.Footprint()` returns exactly those two, and
`TestFootprintIsTheWholeTrace` checks that after removing them the directory is
**exactly** what it was before Rokh - even the owner's pre-existing files
untouched.

Rokh never touches a partition table, never formats, and never writes to a raw
device. "No hardware damage" is not a claim; it is these two lines.

## 5. Who may write

No layer of the core **deletes** anything. `Store` has no removal verb:

```go
type Store interface {
    Read(name string, max int) ([]byte, error)
    Write(name string, b []byte) error
    Exists(name string) (bool, error)
    List(prefix string) ([]string, error)
}
```

`Remove` was taken off this boundary in the audit. An append-only ledger should
not hold a deletion tool; removing a carrier is a human act with human tools.

The one exception is `os.Remove` on a write's own temp file, which never touches
a finished object.

`rokh keys --drop` removes a **key** from the keyring. That is key custody, not
ledger deletion: it cannot remove an event, and no event becomes invalid because
a key left the keyring.

## 6. Deleting content

An event can never be erased from history. But the **content an earlier event
referred to** can be deleted - and the deletion itself is recorded as a new,
irreversible event.

That works because content does not live in the carrier: the ledger holds
events, and bulk content lives outside, referred to by hash. Deleting the
content is an act out in the world; recording that you deleted it is an ordinary
write:

```
rokh write DIR --address photos/2026/beach --verb delete --message "removed the original"
```

History is never rewritten. The ledger only grows, by recording what happened
next.
