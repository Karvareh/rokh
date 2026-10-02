# The daemon API

> This document describes the booth of the version before 1.0.0. The protocol
> of 1.0.0, `rokh.booth/1`, is in [the contract of version
> 1](../../../docs/contracts/v1.md), section 5, and in the code
> of `booth/`, `daemon/` and `rokh-home/gate`; the examples and the op names
> below may differ from it. Rewriting this document against 1.0.0 is owed;
> see `STATE.md`.

> Newline-delimited JSON over a Unix socket. No framework, no code generation,
> no schema compiler. Any language that can open a Unix socket and encode JSON
> can read from and write to Rokh.

## 1. Starting it

```
rokh daemon DIR --socket /run/user/1000/rokh.sock [--read-only] [--no-sign]
```

- The socket is **Unix-domain only**, created with mode **0600**. There is no
  TCP listener anywhere in Rokh and no code path that opens one.
- A stale socket from a crash is removed, but only after checking that nothing
  is listening and that the path really is a socket. A regular file at that path
  is refused, not deleted.
- Socket paths are limited by `sun_path` (about 104 bytes); an over-long path
  gets a clear error rather than a bare "invalid argument".

## 2. Shape

One request per line, one response per line.

```
-> {"op":"status"}
<- {"ok":true,"anchor":"a0b6...","accepted":6,"heads":["38bc..."]}
```

Every response has `"ok"`. On failure: `{"ok":false,"error":"...","code":"..."}`.
`error` is a sentence for a person; `code` is the stable word a program
branches on.

The protocol is `rokh.daemon/3`. Version 2 added typed recording answers,
attempts, a precondition on the heads and the `capabilities` op; version 3
adds a cursor and filters on `log`, the `wait` op, the `door` attestation,
`key` and `authority` beside each listed event, and the `ancestry_pending`
code. Everything the earlier versions said is still said in the same words.

Requests that only read are answered together: many programs asking at once
are answered at once, and none of them waits for another reader. A request
that records has the door alone for as long as its commit takes.

### Was it recorded?

Every answer to a writing op — `write`, `append`, `intent`, `outcome` — carries
`"record"`, one of three words:

| `record` | meaning |
|---|---|
| `recorded` | the event is reachable from the carrier's references: it is in the ledger |
| `not-recorded` | nothing reached the commit point; the refusal's `code` says why |
| `unknown` | a step failed and the carrier could not be read back to find out; ask the `attempt` op before doing anything else, never send again blind |

The answer is read from the carrier, not from the step that failed. If writing
the reference returned an error but the reference landed, the answer is
`recorded`; if it did not land, `not-recorded`. A daemon that could not re-read
the carrier says `unknown`, and rebuilds its view before it answers anything
else. The live answers never hold an event the carrier does not.

Codes a program can meet: `bad_request`, `unknown_op`, `read_only`,
`signing_disabled`, `root_refused`, `key_unknown`, `authority_unknown`,
`harness_refused`, `not_accepted`, `ancestry_pending`, `precondition_failed`,
`attempt_invalid`, `attempt_conflict`, `attempt_unsupported`,
`receipt_refused`, `storage_failed`, `carrier_unreadable`, `turn_busy`,
`not_found`, `namespace_taken`, `bind_refused`.

`not_accepted` and `ancestry_pending` are two answers, not one: the first says
the bytes were judged and refused — they never were; the second says their
ancestry has not arrived here, which is not the same as not existing (T12.3).
A courier that meets the second fetches ancestors and offers again; nothing is
stored either way, and the door's view does not grow with offers that were
never events.

## 3. Operations

### `status`

```json
{"op":"status"}
```
Returns `anchor`, `root` (hex public key), `accepted`, `rejected`, `pending`,
`heads[]`, `branches{}`, `grants[]`.

### `log`

```json
{"op":"log","limit":50}
{"op":"log","after":"<64 hex>","limit":50}
{"op":"log","address":"clerk","verb":"intent"}
```
Returns `events[]` in deterministic topological order, and `last`, the id of
the last event listed. Each event has `id`, `address`, `verb`, `author`,
`parents[]`, and optionally `payload` (base64) and `attest[]` (`oracle`,
`claim` base64). Three more fields keep three questions apart: `authority`,
the grant the event was signed under (absent for the root); `key`, the name
the signing key is held under here, when the door can name it (`root` for
the ledger's root); and `door`, the name of the door that recorded it, read
from its `door` attestation when it left one.

`limit` alone returns the last N. `after` is a cursor — the id of the last
event the caller has read — and then the listing is what came after it, at
most `limit` of them, so a program that keeps `last` between visits reads each
event once. `address` narrows the listing to that address and what lies
under it (the component boundary: `clerk` covers `clerk/desk`, not `clerks`);
`verb` narrows it to one verb.

### `wait`

```json
{"op":"wait","after":"<64 hex>","address":"home/tasks/<program>","timeout":30}
```
Answers as `log` with a cursor would, but if nothing has come after the
cursor yet it holds the answer until something does, or until `timeout`
seconds pass (default 30, at most 600), and then answers with `timed_out:true`
and no events. The zero id (64 zeros) means "from the beginning". A program
that has work to receive sleeps here instead of asking every second. It is a
watcher, and a watcher writes nothing: no event is created by waiting.

### `get`

```json
{"op":"get","id":"<64 hex>"}
```
Returns `raw` (hex) and `state`. The raw bytes are exactly what was signed,
hashed and stored, so a client can verify them independently.

### `announce`

```json
{"op":"announce"}
```
Returns `announcement` (hex), `bytes`, and a human `text`. This is the narrow
path message: it always fits one SF12 LoRa frame.

### `append` - the generic write path

```json
{"op":"append","raw":"<hex of a signed event>"}
```

Takes bytes that are **already signed**. No key is involved, so the daemon is
not a signing oracle. This is the path other programs should use: build an event
per `docs/01-grammar.md`, sign it wherever your key lives, hand over the bytes.

An event the ledger rejects is **never stored**.

### `write` - convenience

```json
{"op":"write","address":"home/journal/today","verb":"note",
 "message":"...","key":"assistant","branch":"main"}
```

Builds and signs with a named key from the carrier keyring. `payload` (base64)
may be given instead of `message`. Disabled by `--no-sign`, and then it is gone;
`append` still works.

### `bind` - what a harness means

```json
{"op":"bind","namespace":"clerk","version":"1","can":["clerk.ask","clerk.answer"],
 "unknown":"refuse","repeat":"idempotent","retry":"never-retry",
 "compensate":"compensable","ending":"ask-a-person"}
```

The core weighs bytes, signature and authority. It does not know what a verb
means and does not guess, so a harness that wants Rokh to carry its meaning
says **eight** things first and none of them has a default. Four are the
covenant and four are what its effects do somewhere Rokh does not rule:

| field | what it says | the only answers |
|---|---|---|
| `namespace` | the harness's own space for addresses and verbs | an address |
| `version` | which version of it is speaking | any string |
| `can` | everything it can do, exhaustively | verbs, each `NAMESPACE.rest` |
| `unknown` | a verb in its space it has never heard | `refuse`, `ignore` |
| `repeat` | what a second identical effect does at the destination | `idempotent`, `duplicates` |
| `retry` | whether it may send again on its own | `may-retry`, `never-retry` |
| `compensate` | whether an act undoes the effect | `compensable`, `irreversible` |
| `ending` | what it does when it never learns how the step ended | `close-unknown`, `ask-a-person` |

A verb is joined to its namespace with a **full stop**, not a slash: an address
is `agents/helper`, and a verb of that harness is `agents/helper.note`.

Two harnesses cannot share a space, and rebinding a namespace that is already
bound is refused rather than silently replacing it. A binding lives in the
daemon's memory for as long as the process runs; it is not an event and does
not survive a restart. Whether it should is an open ruling.

The space has two places (T11.11: its own root, the subtree of its verbs). The exact
namespace — `clerk` — is the harness itself and the shelf its receipts sit on:
`intent` and `outcome` are written there and only there, by the receipt ops
below, and they are never in `can` — they are the ritual's words, not the
covenant's, and a `can` that lists `clerk.intent` or `clerk.outcome` is refused.
Under the root — `clerk/…` — is where the harness's verbs act, governed by the
four clauses above. A receipt aimed under the root is refused, and refused
whatever `unknown` says, because it is misplaced rather than unknown; the
refusal names the root. A verb of the harness's own aimed at the exact root is
refused too: the root is not where its verbs act.

Returns the eight as bound, plus `mayResend` — not a ninth declaration but the
arithmetic of two of them: sending again is called safe only when a harness
that may retry sends into a destination that is idempotent.

`rokh bind` is this op from the terminal.

### `bound` - which meanings are answered for here

```json
{"op":"bound"}
```

Returns `harnesses[]`, each with the same eight fields and `mayResend`.
`rokh bound` is this op from the terminal.

### `intent`, `outcome`, `receipts` - the unit of a harness's work

The receipt is not part of Rokh. Rokh knows events; the receipt is the custom
the harnesses keep on top of it, and these three ops are how an engine holding
delegated authority leaves one.

```json
{"op":"intent","address":"clerk","doing":"send the invoice",
 "witness":{"origin":"...","authority":"...","audience":"...",
            "state":"...","wayBack":"..."}}
```

Records the half written **before** the step and returns `id` — the ledger's own
name for the recorded event, which is the hash of its bytes. Carry that name
into the result.

An intent shows five witnesses. The sixth, `receipt`, is the name of the intent
a result answers, and an intent cannot name its own: its name is the hash of
bytes that would have to contain it. A result owes all six and the sixth is
filled in from the intent it closes, so the two halves cannot disagree about
which receipt they are.

```json
{"op":"outcome","address":"clerk","intent":"<64 hex>","outcome":"done",
 "saying":"...","once":"INV-7788","witness":{...}}
```

Records the half written **after** the step. `outcome` is `done`, `failed` or
`unknown` — and `unknown` is not a softer `failed`; it is the end that was never
learned, said out loud. The intent must already be recorded. The result is
written at the intent's own address and names it as a parent.

Where is "the intent's own address"? For a bound harness, its root and nowhere
else — see `bind` above. `intent` at `clerk/desk` is refused with a message that
says so; `intent` at `clerk` is the shelf. Outside any bound space a receipt may
be written at any address, as before.

`once` is a handle a cooperating destination may use to refuse a repeat. Rokh
alone does not promise "exactly once" for an effect outside it, and says so
rather than implying otherwise.

```json
{"op":"receipts","address":"clerk"}
```

Returns `open[]`: the intents here that no result answers. It reads and does
nothing else — no timeout turns an open receipt into a failure, and reading the
list twice leaves it as the first reading found it. It signs and stores nothing,
so a daemon opened `--read-only` still answers it, which is what a harness
closing its books on a cold carrier needs.

`intent` and `outcome` are refused under `--read-only` and under `--no-sign`,
because a receipt is two signed events.

## 4. Writing a client

The whole client contract is: connect, write a JSON line, read a JSON line.

```sh
printf '%s\n' '{"op":"status"}' | nc -U /run/user/1000/rokh.sock
```

```python
import json, socket
s = socket.socket(socket.AF_UNIX); s.connect("/run/user/1000/rokh.sock")
f = s.makefile("rw")
f.write(json.dumps({"op": "log", "limit": 10}) + "\n"); f.flush()
print(json.loads(f.readline()))
```

For a client that wants to write without trusting the daemon with a key,
implement the grammar and use `append`. `event/testdata/vectors.json` holds
conformance vectors: the same inputs must produce exactly those bytes and ids.

## 5. Guarantees

- **No automatic events.** The daemon has no timer, no watcher, no poller and no
  background goroutine that writes. Only an explicit request can put an event
  into the ledger. An idle daemon writes nothing, and
  `TestIdleDaemonWritesNothing` says so.
- **Rejected is never stored.** Both write paths check the ledger first and
  touch the carrier only on acceptance.
- **The ledger trusts only bytes.** Every input goes through `event.Parse`,
  which enforces the canonical form and verifies the signature.
- **No network.** Unix socket only, mode 0600.

## 6. One writer at a time: the writing turn

`rokh write` works directly on the carrier by design, so a person can record an
event while a daemon is up, and the sentence surface writes the same way. What
keeps them apart is the **writing turn**: the kernel's advisory lock on the
carrier's `.rokh` directory (package `turn`). The daemon, every writing command
of `rokh` and the sentence surface take it before they read the heads they will
write on, and hold it through the commit point. A killed writer's turn goes
away with its process; nothing is created on the carrier for it.

Under the turn the view is read from the carrier first. So a writer that finds
a reference naming an event it does not hold re-reads — the same walk opening a
carrier does, creating no event — and the precondition, the attempt and the new
event's parent are all taken from what is recorded now.

Two daemons on one carrier are serialised the same way. What the turn does not
do, said as plainly: it is advisory, so a program that never asks for it is not
stopped by it; and it is local to one machine, so a carrier written live from
two machines over a network filesystem without locking is not protected — a
berth is the way to write on two machines.

### Preconditions

Any writing op may carry `"expect_heads": [...]`, the heads the request was
prepared against. Under the turn, if the ledger's heads are not exactly that
set, nothing is written and the answer is
`{"ok":false,"code":"precondition_failed","record":"not-recorded","heads":[...]}`
with the heads as they now are. Absent means no precondition.

### Attempts

`write`, `intent` and `outcome` may carry `"attempt": "NAME"` (1 to 128 bytes
of text): the caller's own name for this one recording. It is bound to the
ledger, to the key that signs and to the exact request, inside the signed
bytes of the event as an attestation, so nothing is kept beside the ledger and
a carrier read afresh answers the same.

- The same request under the same name again returns the event already
  recorded, with `"already":true`, and records nothing.
- A different request under the same name is refused with `attempt_conflict`.
- `append` does not take an attempt: signed bytes carry their own name, and
  `get` with it answers whether they were recorded (`attempt_unsupported`).

An attempt is correlation, not a promise about the world. It does not make an
effect outside Rokh happen exactly once.

### `attempt` - what happened under a name

```json
{"op":"attempt","attempt":"gateway:plan-1","key":"gateway-worker"}
```

Reads only, under the shared turn, so it never answers while a recording is in
flight. `key` (or `"author":"<64 hex public key>"`) says whose attempt; with
neither, the root's. Returns `record` `recorded` with `id`, `address`, `verb`
and `request` (the hash of the request it was given to), or `not-recorded`.

### `capabilities` - what this door is

```json
{"op":"capabilities"}
```

Reads only. Returns `protocol`, `release`, `anchor`, `generations`
(`{"reads":["RKH1","RKH2"],"writes":"RKH2"}`), `limits` (`line`, `frame`,
`payload`, `attempt`), `read_only`, `allow_sign`, `no_root`, `ops`,
`record_states`, `preconditions`, `attempts`, `writer_turn` (`carrier` or
`process`), `door` (the name this door attests on what it records, or empty),
`log` (its cursor, filters and the wait op), and `signers`: each name that
signs here with its **public** key, the authority it signs under and whether
that authority is still standing. Nothing secret is in it.

### The door's mark

A door started with a name — `rokh` for the command line, `rokh-home/owner`
and `rokh-home/program` for the home's two doors — adds one attestation to
every event it records: oracle `door`, claim the name. It is the registrar's
mark, and like every attestation it is the signer's testimony and proves
nothing about the world. It sits beside, and never replaces, the two fields
the grammar already keeps apart: `author`, the key that signed, and
`authority`, the grant it signed under. Who composed the words is a fourth
question, answered — if at all — inside the payload or by the owner's own
declaration in the home; a door never answers it.

### A program's door: `--no-root`

`rokh daemon DIR --socket S --no-root` never signs with the root key, whether a
request names `root` or names no key: it is refused with `root_refused`. A
program then writes only with the delegated key it was granted.

### Declaring the same meaning twice

`bind` with exactly the declaration already bound in that space is that
binding, answered with `"already":true`; a different declaration in a taken
space is still refused (`namespace_taken`). `bind` and `bound` both return
`declaration`, the name of the eight declarations as bound, so a harness can
carry it into what it witnesses. A declaration is meaning, not authority.

## 7. Not here yet

Sync between carriers, wants, subscriptions or push, and any authentication
beyond file permissions on the socket. The socket's security is the
filesystem's: anything running as the same user can open it. Programs that must
not see the whole ledger do not get this socket; they go through a gate that
authenticates each of them and holds each to its own scope, outside this
repository (`rokh-home`).

A harness's covenant is held in memory and is not recorded, so there is no
`unbind` and a restart forgets every binding.
