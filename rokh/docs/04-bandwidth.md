# Bandwidth: the narrow path and the wide one

> The narrow mesh carries **news of an event**. The event itself arrives later
> over a link that can afford it. That is the simplest design that captures
> most of the value, and it is the one implemented.

## 1. Measured sizes, not guesses

`oracle/size_test.go` recomputes these on every run:

| event | size | SF7 frames (222 B) | SF12 frames (51 B) |
|---|---|---|---|
| genesis | 231 bytes | 2 | 5 |
| grant | 347 bytes | 2 | 7 |
| delegated write | 383 bytes | 2 | 8 |

Default testimony overhead: 66 bytes.

A delegated write, to the byte:

```
  5  magic+kind      38  carrier     38  author      38  authority
 38  parents         33  address     16  verb        44  payload
 66  attest          64  sig
---
383
```

## 2. No Rokh event fits in one LoRa frame

And it does not compress away. Four large fields each have a hard reason, and
one of them is a decision re-examined during the audit and kept:

- **`sig` (64)** - Ed25519. It does not get smaller.
- **`author` (38)** - it *could* have been dropped and inferred from the grant.
  **Deliberately not.** With `author` present, a receiving node discards
  forgeries with one signature check and **zero state**. Without it, a node must
  hold data it cannot yet verify until the grant arrives - exactly what a
  low-memory mesh node must not be forced to do. Thirty-eight bytes buys
  **stateless rejection**.
- **`carrier` (38)** - the anti-replay binding; without it an event could be
  moved under another ledger.
- **`parents` (38 each)** - causality itself.

**So fragmentation on LoRa is mandatory, not optional.** At SF12 under a one
percent duty cycle, one 383-byte event needs minutes of airtime. That is not a
defect; it is what a narrow band is, and it must be visible in the transport
design rather than hidden.

> The LoRa figures above are the commonly cited EU868 ones (51 bytes at SF12 up
> to 222 at SF7; about 237 bytes for a whole Meshtastic packet). **They must be
> measured on the actual hardware before any ruling.** Here they are the basis
> of an arithmetic, not evidence.

## 3. Hashes on LoRa: yes, if short and full are never confused

An id is 32 bytes. One fits in an SF12 frame; six fit in an SF7 frame. For
**announcing heads** that is poor. The usual answer, the one git takes, is a
truncated id:

| length | per SF12 frame | per SF7 frame |
|---|---|---|
| 32 bytes (full) | 1 | 6 |
| 16 bytes | 3 | 13 |
| 8 bytes | 6 | 27 |

**The red line:** a truncated id is for *pointing* - "do you have this?" - and
never for *trusting*. Acceptance always rests on the full, recomputed id.

That line is closed in code and not by accident:

```go
// frame.ParseID
if len(s) != IDSize*2 {
    return id, fmt.Errorf("id: bad length ...")
}
```

`ParseID` accepts no short form. Every id arriving from outside - a revoke
target, a branch point, an authority - goes through it. `ID.Short()` is marked
"never a reference" and is used only for printing.

## 4. Three classes of traffic

| class | carries | size | on LoRa? | today? |
|---|---|---|---|---|
| **0 - control** | heads, wants, receipts, with short ids | tens of bytes | yes, one frame | **announcement: yes. wants and receipts: no** |
| **1 - event** | the ledger itself | 230-400 bytes | with fragmentation | yes |
| **2 - content** | image, audio, model, package | megabytes | **never** | no |

## 5. The announcement

Implemented in `announce/`. It is the class-0 message this design needs, and
nothing more.

```
byte  0      magic 'R'
byte  1      version 0x01
byte  2      flags; bit 0 set if metadata follows
bytes 3..10  ledger anchor, first 8 bytes
bytes 11..18 head id, first 8 bytes
bytes 19..   optional metadata, tag(u8) len(u8) value
               0x01 count   (u16)  events the announcer holds
               0x02 address (<=24) hint about what changed
```

**19 bytes bare, 51 bytes maximum.** `MaxSize` is the smallest LoRa payload we
intend to support and it is enforced, not hoped for: a real announcement from
the CLI measures **29 bytes**.

### An announcement is a hint, not evidence

Announcements are **unsigned**, deliberately. A signature would add 64 bytes and
push the message past the smallest frame, and it would buy nothing: an
announcement asserts no right and transfers no data. Nothing is ever accepted on
the strength of one. The worst a forged announcement can do is cost a wasted
fetch, which the receiver then rejects at the ledger door like any other bad
bytes.

So the transport rule holds with one clause added: **the narrow path grants
arrival, not permission - and not truth either.**

## 6. What the core does and does not do

**Does - and this is the heart of the answer:**

The inline payload limit is 4 KiB. So **bulk content structurally cannot sit
inside an event**. The event is forced to carry a hash and a descriptor rather
than the thing itself. The separation of class 1 from class 2 is enforced by
that one limit.

> The previous limit was 64 KiB. With it, anyone could put an image inside an
> event and take the ledger off the mesh. The audit found this and the limit was
> corrected.

**Does not:**

- wants and receipts: only the announcement half of class 0 exists;
- **the content-reference format is not standardized.** An event can carry a
  hash in its payload, but *how* is the bundle layer's contract. Deliberately
  unwritten: without the sync protocol, any format invented now would be a
  guess;
- fragmentation and reassembly.

## 7. Where bandwidth awareness belongs - and it is not the overlay network

One might assume that path selection belongs to an IP overlay network such as
Yggdrasil. It does not, for three reasons:

**1. An IP overlay and LoRa are not the same world.** An overlay like Yggdrasil
runs on IP: peers connect over TCP/TLS/QUIC and every node gets an IPv6 address
derived from its public key. LoRa is not IP at all. An IP overlay over LoRa is
not a supported configuration, and with a ~200 byte MTU and a one percent duty
cycle it is not practical either. So the overlay cannot *be* the LoRa path;
these are **two separate transports**, not one beneath the other.

**2. The overlay grants arrival, not selection.** It sees link cost for its own
routing, but it never tells an application "this link is narrow, send something
small", and it does not and should not know which Rokh bytes matter more.

**3. The right place is the bundle layer.** Deciding "what do I send within this
budget" needs knowledge of the ledger: which heads the peer lacks, which event
is a prerequisite for which, what a revocation takes priority over. Transport
knows none of that.

This is the rule we already had, and it still holds:

> **The synchronizer does not know the transport; it only builds and checks a
> bundle.** And **scoping happens at bundling time, not at sending time.**

The byte budget sits exactly where scoping sits: **at bundling time.**

```
ledger  -->  bundle (what to send, within what budget)  -->  transport (just carry it)
             ^ here                                          IP overlay | LoRa | USB stick | file
             bandwidth is seen here
```

## 8. What needs a ruling before any more code

1. **Bundle limit and transport frame limit.** The event limits are settled
   (4 KiB payload, 8 KiB frame). These two wait for real measurement on the mesh
   and on the target hardware.
2. **Short id length for class 0.** Eight bytes or sixteen? The trade is
   accidental collision against frames per message.
3. **The content-reference format** - hash, size, type; and which path the
   content itself travels.
4. **Does LoRa carry events at all, or only class 0?** The simplest answer may
   be that the narrow mesh only ever carries *news*, and events always come over
   a wide link. That is the least machinery for the most value, and it is what
   this design currently assumes.
