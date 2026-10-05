# The sentence shell: eight verbs, one human surface

> The owner has fixed the human surface of Rokh: a closed set of canonical
> sentences with variable slots. Typing a sentence performs the operation; the
> reply is a sentence in the same register.
>
> **The sentences were Persian and are now English** (the owner's ruling, 10
> Shahrivar 1405): the command line and the terminal speak technical English,
> whole, and Persian stays for the treatise and the charter written in it and
> for the site. What a person *writes into* a sentence — an address, a name,
> a payload — is their content and is never folded to the surface's
> language.
>
> The translation is the standing proof of the claim in §4: the syntax moved
> and not one of the eight meanings did. Every conformance vector kept its
> operation and its slots across it.
>
> Code, identifiers, API and commit messages were always English. The
> subcommands and the socket API remain the bridge for programs; the shell is
> a human front over the same doors.

## 0. Placement

The sentence surface is package `shell`, carried by bare `rokh` and by
`cmd/rokh-shell`; it is an adapter, like `rokh-forms`. It uses the same
packages `cmd/rokh` uses, through the same public doors - `ledger.Add`, the
carrier, the existing patterns - and adds **no core semantics**. Zero
dependencies; the grammar is parsed by hand.

Held by `arch/` tests: the shell holds no socket of any kind (it does not even
import `net`), it never imports `announce` or `daemon`, and it signs nothing a
person did not ask for in a sentence.

## 1. Session: a temporary machine in RAM

`open` loads the carrier, verifies everything, stands at the heads. The
session lives in memory only; **an idle session writes nothing**, with the
same style of test as the daemon's. `leave` closes: nothing new is flushed, and
each carrier is re-read and compared, confirming it is exactly as the last
accepted operation left it.

One session may hold several ledgers open; sentences address the currently
opened one. One-shot use: `rokh-shell VAULT -c "<sentence>"`. The folder may be
either shape: a folder of ledgers, or a carrier made by `rokh init`, which is
the session's one ledger, named after its folder (a second ledger is made in
a folder of ledgers, not inside it).

**The design nudges toward one ledger per person.** When nothing is open and
the vault holds exactly one ledger, it becomes the subject of the sentence
without ceremony. Creating a second ledger works but is never the suggested
path. Projects are address scopes inside the one ledger, not separate ledgers;
a project may keep its own Git for code - Git is part of a planet's body, not
a rival of the ledger.

## 2. The cosmos: vault, library, seats, berths

```
<vault>/ledgers/<local-name>/     each a normal carrier
<star-library>/rokh/<anchor>/     the machine's library, named explicitly
<mount>/<seat-name>/              seats: berths and mirrors
```

- **The vault** is a plain folder and holds ledgers only. No registry, no
  config file, no OS coupling. Local names are aliases; **identity is the
  anchor**. Same anchor, same ledger; different anchor, a different ledger,
  never mergeable.
- **Content lives inside the vessel** (contract E5), in the same commit as
  the event that describes it. **The library belongs to the machine, one
  layer below Rokh**, at `<library>/rokh/<anchor>/`, passed explicitly
  (`-library`); it refuses to sit inside a carrier or inside the vault's
  ledgers tree, and both refusals are tested. Bridge engines may, on explicit
  command only, record an inventory statement of machine assets as ordinary
  events; no watcher, no automatic inventory - the no-automatic-event rule
  stands.
- **A berth** (the Persian surface had its own word for it) is a same-anchor
  carrier of an opened ledger on another machine or medium. "Moon" is
  reserved for the layers the screen shows (contract section 6) and never
  names a carrier copy.
- **A seat** is a neutral opening place under the mount folder. Every seat
  states three things before any operation is offered: the anchor it holds
  (read off the opened carrier itself, never from a claim), its kind - `berth` or
  `mirror` - and the recorded human ruling it rests on (`seat.json` beside the
  carrier; the owner's file, not Rokh's). An undeclared seat is not operated
  on. A mirror is a foreign ledger, read-only beside yours, never merged;
  nothing crosses from it without a covenant.

## 3. The sentences

Parsing rules:

- The FRAME — the keywords and role words — is folded only for ASCII case:
  `WRITE AT` is `write at`. Nothing else is folded.
- Every SLOT is preserved **byte-for-byte as typed**. The payload of `write` in
  particular: no normalization, no trimming beyond the frame (the colon and
  exactly one following space). There is no sentence-final period; a period
  belongs to whoever typed it.
- The verb comes first, and the role words anchor the slots after it: `at`
  (ledger, address, scope), `to` (destination, recipient), `for` (audience),
  `named` (a new ledger's name); the colon introduces the payload.
- ZWNJ inside slots is content. Control bytes in any displayed payload are
  escaped on output, so stored content can never command the terminal.
- `{id}` is a full event id only; prefixes are refused, per `ParseID`.
- Shell addresses are space-free; the core allows inner spaces, the surface
  does not.

```
open the ledger {name}
open a new ledger named {name}
leave
write at {address}: {text}
write            write {n}
cancel           cancel {n}
read {address}
see the ledger
see the ledgers
see the grants
bring {thing} to {address}
bring the returned ledger
entrust writing at {place} to {who}
entrust reading {place} to {who}
take back the grant {id}
carry the ledger to {path}
carry {address} to {path}
carry the bundle for {who}
reconcile
```

Eight verbs — read, write, bring, see, entrust, open, leave, carry — and a few
small words that are not verbs: `leave` and `reconcile` stand alone, `take back`
is entrusting's opposite, and `cancel` is the working state's own word for
letting a waiting sentence go. The count on the screen says so; it does not
call them eight when they are more.

**Working state.** `write at …` puts a sentence in working state and records
nothing. Several may wait at once, one per address: saying a sentence again
for the same address revises it in place, a sentence for another address
waits beside it. `write` records the newest waiting sentence, `write {n}` the
n-th counted from the oldest; `cancel` and `cancel {n}` let one go, and
nothing is recorded by letting go. The screen lists every waiting sentence
with the key that would sign it, the grant it would sign under and the parent
it would name — so a person sees exactly what would be recorded, and by whom,
before anything is (T4.4, T9.2).

**The list.** Both surfaces show one list (`tui.Sentences`): each sentence
says in plain words what it does, then exactly, and whether it records. The
seven that record an event - open a new ledger, write, bring a thing,
entrust writing, entrust reading, take back, reconcile - say "records one
event" and are marked; the ones that only look say "reads only"; `write at`
says it waits. A test says every sentence on a ledger and counts the events
it made, so the list and the behaviour cannot drift. `?`, `/`, `help` and
`sentences` show the list on both surfaces and are not sentences.

**Near misses.** A line that begins like one of the sentences and has the
wrong shape is answered with that sentence's shape and an example, chosen by
its first word or two (`"write at" needs an address, a colon and your text:
write at home/journal: my first note`). It is still refused as a line that
is not a sentence, `[unknown_sentence]`, and the parser does not guess.

`shell/testdata/sentences.json` holds the conformance vectors - every
sentence parsed to operation, slots and reply template, with ZWNJ written as a
JSON escape so the file documents its own invisibles.

## 4. Mapping

| sentence | operation | layer |
|---|---|---|
| open | load, verify, stand at heads | shell session |
| open (new) | genesis in `ledgers/<name>`; the first page is the typed sentence itself; refuse an existing name; refused inside a carrier made by `rokh init` | existing init pattern |
| leave | close; confirm the carrier untouched | shell session |
| write at | one waiting `note` sentence; payload = the exact bytes of {text}; nothing recorded | working state |
| write, write {n} | one `note` event from the waiting sentence; the reply names the id, the signing key, the grant and the door | existing write |
| cancel, cancel {n} | the waiting sentence leaves working state; nothing recorded | working state |
| read | events at that scope in deterministic order; a descriptor payload is resolved from the vessel and hash-verified before showing; a system event is marked `system` and said as what it did | existing log + content |
| see (the ledger) | status + verify in one answer, the person's events and the ledger's own counted apart; on the line surface the three layers follow as an aside on stderr | existing verify |
| see (the ledgers) | vault ledgers: name, anchor, events, heads, custody (warm/cold); then every seat with its three declarations | adapter |
| see (the grants) | live grants and open disclosures at the heads | existing views + covenant evaluator |
| bring (a thing) | the import rite: hash and check → the content into the vessel and one descriptor event, in one commit. A folder becomes one canonical tree manifest (sorted paths, size, hash; no symlinks, no special files, no permissions or times in identity) referenced by one event | forms pattern + docs/07 |
| bring (the returned) | reunion of berths: scan the seats; refuse a different anchor plainly and never offer union for it; skip mirrors and undeclared seats; union both ways by offering raw signed bytes to each side's `Add`; report verdicts per side; if two heads remain clean, ask exactly `say "reconcile" to make them one` | courier pattern, same anchor only |
| reconcile | only on this exact reply: one `rokh.merge` signed with a session key the ledger accepts, committed to both sides | existing merge |
| entrust (writing) | grant: subject, scope, verbs=note; owner-signed | existing grant |
| entrust (reading) | share covenant `peer.share`; owner-signed, bundle-layer evaluator | docs/07 covenant |
| take back | a grant id revokes; a share id unshares. The object is always the recorded id, never "his right" | existing revoke / covenant |
| carry (the ledger) | in v1 a whole copy is a seed (contract 4.7): refused here, with the command line that makes it, `rokh seed FROM TO`; nothing is recorded | adapter |
| carry (an address) | export payloads and hash-verified content as plain files; zero events | adapter |
| carry (a bundle) | courier bundle under the peer's live covenants; exclusions named | existing courier |

## 5. The answers

Fixed templates, same register; only numbers and ids vary; every write reply
carries the full event id. Stdout carries the sentences and nothing else;
auxiliary detail goes to stderr.

```
opened; I am standing at the head of the chain. {n} events and {s} system events, {k} heads.
a new ledger is open; its anchor is {id}.
left. The ledger is closed, everything where it was.
waiting as sentence {n} of {m}: at {address}, {bytes} bytes, to be signed by {key} under {authority} — nothing is recorded yet. "write" records it; "cancel" lets it go.
written and recorded. {full id} — signed by {key} under {authority}, through {door}.
let go. Sentence {n} left working state; nothing was recorded.
{n} events and {s} system events, {k} heads, all verified.
brought it, checked it, wrote it. {id}
brought it but did not write it — {why}. The source is untouched.
{n} new from them, {m} from us; {verdicts}. Two open heads — say "reconcile" to make them one.
they met; neither was erased and neither won. {id}
entrusted. {id}
opened to them. {id}
taken back; from here on, not over what is past.
carried; a whole copy at {path}. The keys did not go — writing there needs a new entrusting.
carried; {n} put out as files. Nothing in the ledger moved.
bundled to carry: {n} events, under the same grant whose reading you entrusted to them. {what} did not go.
```

Every refusal, on both surfaces, is `no — {sentence} [{code}; {record}]`:
a sentence for the person, in one or two plain lines that say what happened,
whether anything was recorded and what to do next, then the stable word a
script branches on and, where anything could have been recorded, whether it
was — `not recorded` or `unknown`. The core's own words are translated, not
shown: a wrong passphrase is `passphrase_refused`, a full Rokh `vessel_full`
with the way out (`rokh grow FOLDER --to SIZE`), and the codes of the
contract (`vessel_corrupt`, `turn_lost`, `turn_unavailable`, `anchor_differs`,
`ancestry_unproven`, `content_conflict`, `content_incomplete`, `key_revoked`,
`view_denied`) keep their names. A line that is not a sentence answers `I do
not know that sentence — "?" or "/" lists the ones I do [unknown_sentence]`,
or its near miss; a ledger that refuses an event says which check failed, and
the code is `rejected`, `ancestry_pending` or `storage_failed`; a refusal
whose cause has no code of its own is `refused`.

Exit codes agree, on the line surface, in a script and with `-c`: 0 when
every sentence was answered, 1 when any was refused, 4 when whether a
recording happened is unknown; the worst stands. The `? ` prompt and the
greeting are for a person at a terminal and are never printed into a pipe.

A system event is read in the same line as any event, marked, and said as
what it did: `— system rokh (4d3277c8): key "reader" added, generation 1,
reads journal`; its bytes are never printed.

The `signed by … under … through …` tail keeps three questions apart on
the surface as the grammar keeps them in the bytes: the key that signed, the
grant it signed under, and the door that recorded. None of them says who
composed the words, which the surface never claims to know (T12.5). Custody
is one of three words everywhere — `warm` (the root key is at hand),
`delegated` (only an entrusted key is), `cold` (none).

The discipline of the docs is carried in the wording, never softened: accepted
is not true; verified is not honest content; entrusted is not consent or
ownership; opened-to is not sent. Announce and hello have **no sentence** -
they are machine affairs behind the bridge and must not leak into this
surface.

## 6. Principles recorded, not built

- **Every machine is governed by somebody's rokh.** Operating on another
  person's star means being a guest under their covenants. This names the
  future guest mode; nothing of it is built.
- Cross-owner mirrors stay read-only beside yours; a foreign bundle still
  cannot be accepted, and the courier's warning stands.
- Sealing, radio drivers, automatic union or merge, key-carrying copies: all
  deliberately absent, named here rather than stubbed.
- The Persian treatise stays in place, untranslated and unmodified.
