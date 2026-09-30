# The Rokh grammar, version 1

> This document is the contract. The code applies it, not the other way round.
> Any other implementation that satisfies this document and the conformance
> vectors writes Rokh.

## 0. The founding rule

> **The bytes that are signed, the bytes that are hashed, and the bytes that
> are stored are the same bytes.**

Nothing is ever re-serialized. An event is a byte string; its id is the hash of
that byte string; the carrier stores exactly that byte string. So "the
recomputed hash does not match the stored hash" is structurally impossible.

The consequence: **JSON is not the wire format.** JSON has more than one
encoding for the same meaning (whitespace, key order, unicode escapes), and any
rewrite can change the bytes without changing the meaning.

## 1. Frame

```
frame  := magic(4) || kind(1) || fields || sig(64)
fields := field*        ordered by tag, strictly ascending, no duplicates
field  := tag(u16 BE) || len(u32 BE) || value(len)
```

- `magic = "RKH1"`. The grammar version lives in the magic; a second version is
  a different frame.
- `kind = 0x01` for an event. An unknown kind is **rejected**.
- Signature: Ed25519 over `magic || kind || fields`, that is every byte before
  itself.
- Id: `sha256` of the whole frame, signature included.

### Canonical form, enforced on read

A reader rejects every deviation. It never repairs and never overlooks:

1. exact magic;
2. known kind;
3. tags strictly ascending (which also kills duplicates);
4. `len == 0` is invalid: a field is present or absent;
5. unknown tag is invalid;
6. the length must be consumed exactly, then exactly 64 signature bytes;
7. `MaxFrame` 8 KiB, `MaxFields` 16.

**Final check:** rebuilding the frame from the decoded fields must reproduce
the input byte for byte. The code does this on every parse.

## 2. Event fields

| tag | name | value | required |
|---|---|---|---|
| `0x0001` | `carrier` | 32 bytes, the ledger anchor | absent iff genesis |
| `0x0002` | `author` | 32 bytes, Ed25519 public key | always |
| `0x0003` | `authority` | 32 bytes, id of a grant | absent implies author is root |
| `0x0004` | `parents` | `32*n`, ascending, distinct | absent iff none |
| `0x0005` | `address` | UTF-8 | always |
| `0x0006` | `verb` | UTF-8 | always |
| `0x0007` | `payload` | bytes | absent iff empty |
| `0x0008` | `attest` | attestations | absent iff empty |

```
attest      := attestation*     ordered by (oracle, claim), distinct
attestation := nameLen(u8) || oracle(UTF-8) || claimLen(u32) || claim
```

### What is deliberately not a field

- **No timestamp.** Time is testimony (a clock oracle), not a column. Because
  it is not a field, no implementation can order by it even if it wanted to.
  The earlier implementation let a claimed timestamp affect acceptance order;
  this one cannot.
- **No local counter.** Causality comes from parents. Two writers sharing a key
  would make a counter meaningless; they cannot make a parent meaningless.
- **No owner.** The carrier binding and the authority chain both terminate at
  genesis on their own.
- **No visibility policy.** Encryption at rest belongs to the carrier;
  selective sharing belongs to a later layer.

## 3. Limits, derived rather than chosen

| thing | limit | why this number |
|---|---|---|
| address | 255, component 64 | it is an identifier, not prose |
| verb | 64 | same |
| **inline payload** | **4 KiB** | a generous note. Rokh is not a content store |
| parents | 16 | merging sixteen branches is already excessive |
| attestations | 8, claim 256 | testimony is a stamp, not cargo |
| oracle name | 32 | |
| verbs per grant | 32 | |
| fields per frame | 16 | eight are defined today |
| **whole frame** | **8 KiB** | the sum of everything above plus overhead |

The maximum, to the byte:

```
   5   magic + kind
  38   carrier      (6 + 32)
  38   author       (6 + 32)
  38   authority    (6 + 32)
 518   parents      (6 + 16*32)
 261   address      (6 + 255)
  70   verb         (6 + 64)
4102   payload      (6 + 4096)
2350   attest       (6 + 8*(1+32+4+256))
  64   sig
-----
7484   <  8192
```

**The previous limits were 64 KiB for payload and 128 KiB for the frame, both
arbitrary.** That open limit was what disabled the low-bandwidth path: bulk
content could sit *inside* an event, and such an event passes through no narrow
mesh.

The **bundle** limit and the **transport frame** limit are separate, belong to
the carry layer, and have not been ruled on. See `04-bandwidth.md`.

## 4. Address: anything addressable

> The Rokh ledger records events about **anything that can be addressed**.

Addresses are hierarchical, separated by `/` - star/planet/moon if you like,
but *inside the event*, never as a directory name. Because the ledger is
content-addressed, `/` never reaches disk, so the old problem of a path
component containing a separator does not arise at all.

### Character rule: refuse, do not repair

Full Unicode normalization needs large tables and a dependency-free core cannot
carry them. So instead of *repairing* ambiguity we *refuse* it. In an address
or verb these are invalid:

- invalid UTF-8;
- ASCII and C1 control characters;
- **any combining mark** (`U+0300-036F`, `U+064B-065F`, `U+0670`,
  `U+06D6-06ED`, ...);
- **any format or bidirectional character** (`U+200B-200F`, `U+202A-202E`,
  `U+2066-2069`, `U+FEFF`, ...).

Two results, both free:

1. **NFD is not expressible.** With no combining mark permitted, a decomposed
   character is rejected and only the precomposed form remains. A macOS box
   writing NFD and a Linux box writing NFC reach the same bytes, without a byte
   of tables.
2. **Visual spoofing is closed.** An address that displayed as something other
   than what it is cannot be constructed.

Also: no empty component, no `.` or `..`, no leading or trailing space, no `/`
in a verb.

**Important boundary:** this strictness applies to **identifiers** only. A
**payload** is prose and accepts any bytes.

## 5. Reserved verbs

Any verb beginning with `rokh.` belongs to the core. Apart from these four, a
reserved verb is **unknown and rejected**:

| verb | meaning |
|---|---|
| `rokh.genesis` | birth of a ledger; the only event with no carrier and no parents |
| `rokh.grant` | confer a bounded right |
| `rokh.revoke` | withdraw a right, from here on, not in the past |
| `rokh.merge` | join branches; carries no payload, because it claims nothing |

A reserved verb always sits at the address `rokh`, so "where it sits" stays
separate from "what it confers" and each has exactly one form.

The boundary is closed in both directions: **the address `rokh` and everything
beneath it is core territory.** A user verb does not sit there. Without this
clause, core and user events would mix on one address.

## 6. Grant and revoke payloads

The same TLV encoding as section 1, inside `payload`.

**Grant**

| tag | name | value |
|---|---|---|
| `0x0001` | `subject` | 32 bytes, the key receiving the right |
| `0x0002` | `scope` | UTF-8 address prefix. **Absent means the whole ledger** |
| `0x0003` | `verbs` | `(len(u8) || verb)*`, ordered, distinct. **Absent means any non-reserved verb** |
| `0x0004` | `canDelegate` | `0x01`. Absent means false |

**Revoke**: `0x0001 target`, 32 bytes, the id of a grant event.

Rules:

- A reserved verb **never** appears in a verb list. Granting is governed by
  `canDelegate`; revocation by "everyone may withdraw their own".
- `canDelegate` is itself the permission to write a grant event. Without it
  nobody delegates anything.
- Scope stops at the component boundary, not at a string prefix: scope `a` does
  not cover `ab`.

## 7. Testimony, and its red line

An oracle observes one thing at one moment and testifies. Testimony sits inside
the event and the hash and signature cover it.

> **The signature proves the author *said* the oracle reported this. Not that
> the report is true.**

Default oracles: `clock` and `chance`.

`chance` (16 random bytes) does real work: events are content-addressed, so two
events with the same content and parents get the same id and *are* one event.
Chance is what separates "twice, separately" from "once, repeated".

`host` exists but is **not** a default: a host name is identifying data that
every future recipient sees forever. Adding it is a deliberate act.

An oracle that fails testifies to nothing and the event is made without it.

## 8. What this version does not say

Bundles and sync, peer discovery, selective inter-personal encryption, large
object stores, queryable indexes. Each has its own layer and none has been
ruled on.
