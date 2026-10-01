# The carrier: the domain Rokh lives in (v1)

> Rokh and its carrier are seen together. A ledger without a carrier has
> nowhere to be; a carrier without a ledger is a directory like any other.

This document describes the v1 carrier, contract `texts/contracts/v1.md` sections 1
and 2. The 0.9 layout (`rokh.json`, `.rokh/objects`, `.rokh/refs`) is gone: v1
neither reads nor converts it (contract section 7).

## 1. What a carrier is

A carrier is an ordinary **folder**: it can be selected, copied, cut, and put on
a disk, a flash drive, an SD card, a phone or a cloud folder. Rokh never
touches a partition table, never formats, never writes to a raw device. Its
whole footprint is one directory, `rokh`, inside that folder:

```
<carrier>/
  rokh/
    head0.rkh .. head3.rkh      four head files, 65,536 bytes each
    000/00000000.rkh ..         N slab files of S bytes each, 1,024 per directory
```

`carrier.Footprint()` and `vessel.Footprint()` return `["rokh"]`. Every name is
lowercase `a-z 0-9` in 8.3 shape with extension `rkh`, so FAT32, exFAT and
cloud folders keep it as it is. Foreign entries (`desktop.ini`, `.DS_Store`,
conflict copies) are ignored, never read, never deleted.

Opened without Rokh the folder shows nothing readable: no name, no address, no
count or size of any item. It shows that a vessel of format 1 is there, its
KDF cost and salt, S, N, and file times (contract 2.3, U1).

## 2. Size

At every instant, also in the middle of a write, the vessel is exactly N + 4
files of N×S + 4×65,536 bytes. There is no temporary file, no lock file and no
rename. The file set and the lengths change only by `rokh grow --to SIZE`,
`rokh shrink --to SIZE [--finish]`, or a step of automatic growth
(`auto(step, max)`). Both commands name a target, never a delta, so repeating
one changes nothing. A free slab is S bytes of randomness, so a full vessel and
an empty one look the same.

## 3. Keys and sealing

- `VK`: 32 random bytes per vessel (each seed has its own). `SLK`, `HDK` and
  `PTK` are HKDF-Expand of VK.
- A slab is `salt(32) || AES-256-GCM(HKDF(SLK, salt), zero nonce, plaintext,
  aad = vessel_id || index) || tag`. It is written whole and never partly. A
  slab moved to another index or another vessel does not open.
- A head file carries the magic, the KDF parameters, 32 key slot cells, and the
  root record sealed under HDK with the file number bound in.
- A passphrase opens the vessel through a slot cell (the key layer,
  `key.Unlock`); the vessel never sees it. The key must be among the cells of
  the generation that opens: a cell that survives only in an older head file
  does not open a newer generation.
- Records inside packs: an event's signed RKH3 head is in the clear of the pack
  (which is itself sealed) and its body is sealed for the readers of its
  address; content is sealed chunk by chunk with its coordinates bound in.

## 4. The commit point and recovery

One recording is one commit, for example an event and its branch pointer
together (contract 2.6):

1. The owner still holds the vessel. The base is the highest generation that
   verifies.
2. New and rewritten slabs go only into slabs never used, or freed at least R =
   3 generations ago. Each is flushed.
3. The inventory segments are written the same way.
4. The owner still holds, and the highest verified generation is still the base,
   by number and by token. Otherwise `turn_lost`.
5. Head file `n mod 4` is written whole and flushed.
6. It is read back: this commit and a clean write → `recorded`. Another head →
   `not-recorded`. A failed read-back, or this commit behind a write that
   reported an error → `unknown`.

Opening takes the highest generation whose inventory and recent slabs verify.
When a newer one does not, the report says so (`fell_back{from, to, why}`) and
the next commit is built on the verified base. Recovery writes nothing by
itself.

What this proves and what it does not:
- A cut at any byte opens to the last complete commit (V1).
- A file-by-file copy opens to a complete commit only when at most R commits
  start during the copy (V2). Beyond that the copy may not open at all. This is
  gap G1 of the contract and is reported red.
- `recorded` means written, flushed and read back. On a medium that does not
  honour a flush, survival of a power cut is that medium's promise (U4).

## 5. The writer's lock

A writer takes the kernel's lock on `rokh/head0.rkh` (package `turn`): `flock`
on darwin, linux and android, a byte-range lock beyond the end of the file on
windows. Nothing is created or written for it. It is held until release or the
holder's death, never by a timer. The core never takes a lock: the host hands
it an Owner. The lock is local; two machines writing one synced folder are not
kept apart (U5). Two machines use two seeds.

## 6. The portability boundary

The core (`frame`, `event`, `ledger`, `carrier`, `vessel`, `key`, `lineage`)
imports no `os`, `path/filepath`, `time`, `syscall`, `net`, `os/exec` or
`runtime`, draws no entropy of its own, and carries no build tag or per-OS
file (`arch/core_test.go`). Files reach it through `vessel.Medium`, whose file
implementation `medium.Dir` confines every operation to the carrier folder
with `os.Root`: no symlink or `..` leads out of it.

## 7. Content

Content lives inside the vessel (contract E5): `content.Bring` puts the bytes
into the same commit as the `content.put` event that names them. It is served
only after every chunk has opened at its own coordinates and the whole has
hashed to its id (E8). The machine library outside the carrier is no longer
written.

## 8. Seeds and reconcile

A new rokh folder comes only from `init` (the root) or `rokh seed SRC DST`. A
seed is a new vessel with its own VK, the inherited salt, the same anchor and
its scopes. It holds its scopes whole and every other ancestor as a signed head
only. `rokh reconcile DIR OTHER` is the union of records both ways. It records
no event unless asked with `--merge`, and its answer names both halves. Two
different anchors never meet.

## The chest: the other profile

A folder keeps every event sealed and every name covered, and it is still a
folder: a file manager shows that a Rokh is there, how many events it holds,
how large each is and when it was last written. The ruling says the shape is a
**profile** and not the carrier, and that another profile is allowed while the
properties hold (T8.7). The chest is the other one.

```
mine.chest        one file, a fixed size, random from end to end
  └ LUKS2 header
     └ btrfs      the vault: ledgers/<name>/ are carriers, as always
```

`rokh-chest` is a separate command, built for Linux only, and Rokh does not
know it exists. It makes a folder appear at a path; `rokh` opens what it finds
there, as it would open any folder.

```sh
rokh-chest make ~/mine.chest --size 2G   # once
rokh-chest enter ~/mine.chest            # open, run rokh, close after
```

**Why the file is filled with random first.** A container that grew as it was
used would say how much was in it; one whose unused part was zeros would say
the same to anyone who read the file. Random before anything else means the
file reads the same from the first byte to the last, whether it holds one
sentence or ten thousand. The size is fixed when it is made, and stays.

**Who holds the privilege.** Mapping an encrypted device is the kernel's
device mapper and needs privilege — but not a person typing `sudo`. A desktop
already trusts udisks2 to do exactly this for whoever is sitting at it, and
the rule that governs it (`org.freedesktop.udisks2.loop-setup`,
`encrypted-unlock`, `filesystem-mount`, `modify-device`) grants an **active
local session** with no password. So at your own desk it needs nothing. Over
ssh or in a script there is no such session, and then it needs root; the same
steps run in the same order, and only the asking differs.

**The passphrase is never an argument.** It goes to the standard input of the
one command that needs it. `cryptsetup luksFormat` writes the header into a
file its owner already owns and needs no privilege at all; only the mapping
does.

**Two secrets, not one.** The chest has its own passphrase and the ledger
inside has its own. They may be the same string; nothing here makes them so.

**What a chest still does not hide.** That a LUKS2 container is there, its
size, and when the file was last written. Encryption hides content, not the
fact of a thing or the time it was touched (T7.6).
