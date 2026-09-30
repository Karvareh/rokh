# State of this tree

This is the source of version 1.0.0 of Rokh. What is written here was seen in
runs on Linux and on macOS; nothing is called met that was not run.

## What is built

- A carrier is a folder that holds a sealed vessel: fixed in size, or
  growing by itself in whole slabs. Opened without Rokh it shows no name, no
  address and no count.
- One owner's passphrase, which has no recovery. Keys that read, keys that
  write, keys with no right, each with a passphrase of its own; a key is
  taken back with `rokh key revoke` and rotated with `rokh key rotate`.
- Seeds, whole or a slice, with `rokh seed`; two seeds become one only by
  `rokh reconcile`. When both sides changed the same thing, both are shown
  and nothing is chosen.
- Two booths on one protocol, `rokh.booth/1`: `rokh daemon` for a carrier,
  and the gate of `rokh-home` for a home. A session is served the view of
  its own key, opened with that key's own reader.
- Several doors on one carrier at once, booths and runs of the command
  line: every recording answered `recorded` is in the ledger read afresh.
- The sentence surface and the screen: `rokh`, `rokh FOLDER`,
  `rokh-shell -demo`.
- The conformance map: 154 evidence-bearing obligations of the two
  specification texts, 140 met, none owed, 7 waiting for a ruling, 5 outside
  the design, 2 that only use in the field can answer.

## Known to fail, and skipped

Three tests fail in this tree, and have failed since before it was assembled.
Each is skipped with `t.Skip` naming this file, so that a run of the suite
measures everything else; remove the skip and the test fails.

| test | why |
|---|---|
| `rokh/cmd/rokh` `TestADelegateCannotAcceptForThePerson` | written for the way keys were made before version 1; not yet rewritten |
| `rokh/key` `TestOldEnvelopesOpenAfterTheOwnerRotates` | it and another review test ask opposite things of one session; the ruling is not made |
| `rokh-home/archive` `TestALargeAttachmentStreamsBothWaysInBoundedMemory` | a large attachment is held in memory several times over; not repaired |

Run by the superuser, two more fail, and are not skipped:
`TestAMarkerNamingASeedGivenAndTakenIsRefused` and
`TestAResumedSeedGivesNoCellToAKeyTakenBackSince`, in `rokh/cmd/rokh`. Each
cuts a seed by making the source's slab files read-only, and the superuser
writes them anyway, so the seed is not cut. Run by anyone else, they pass.

## Not built, or not finished

- The command line has no `bring`: a file larger than 4,096 bytes is
  brought through the sentence surface or through the home, not through
  `rokh write`.
- `rokh key passwd` is not built; it says so.
- A seed cannot be made growing; repeating a reconcile changes a few bytes
  on one side, though no event, head or key changes.
- A large file that goes in or out through the home takes memory several
  times its size, and through the core (`content.Bring`, `content.Fetch`)
  four times: 8 GiB for a file of 2 GiB (`rokh/docs/11-scale.md`).
- A booth does not serve `content.put`, `content.get`, the keyring, or
  seeding between two booths; those are done with the command line on the
  folder.
- An engine still lives inside `rokh-home` (`native`), written for one
  engine. It is to move to a module of its own.
- The home does not build for Windows.
- Eight test files of the booth of a carrier, and four of the home, are
  behind a build tag and in no run.
- The sentences of the surface were carried over from Persian one word at a
  time; some do not read as English (`go` closes the ledger). A pass over
  every sentence a person types or reads is owed.
- `rokh/docs/06-api.md` describes the booth of the version before this one
  and carries a note saying so. The cryptography document of the previous
  version was withdrawn from this tree: its statements about the carrier no
  longer held. A document for version 1 is owed.
- A commit writes every inventory segment, where the contract writes the
  changed ones (2.6), and verifies every segment twice: once a vessel passes
  4 GiB, each recording writes 1/4096 of it (256 MiB at 1 TiB) and takes
  about 5 µs for every slab (`rokh/docs/11-scale.md`).
- A door's answers take longer as the history grows: each commit drops the
  vessel's index, the references are found by opening every branch pointer
  ever recorded, and the ledger's order is made again whole. At 300,000
  events a write takes 1.2 s and a status 0.75 s (`rokh/docs/11-scale.md`).
- Every grant, revocation and keyring change keeps its own copy of all of
  them in its causal past, so their memory grows as the square of the people
  named: 2 GiB for 8,000 grants (`rokh/docs/11-scale.md`).
- The writing turn goes to whichever door tries first once it is free, not to
  the one that asked first: with many doors writing, the slowest wait far
  longer than the rest, and among 128 writers on 32 doors 11 writes of 1,024
  were refused after waiting 15 s (`rokh/docs/11-scale.md`).

## Not promised, by design

- A copy of the folder made while a writer records more than three times
  is not promised to open. Copy when nothing writes.
- Two machines writing one synced folder is not supported. Each machine
  takes a seed.
- What a key could read before it was taken back, it may have kept.
- At most 31 keys beside the owner's open one vessel, and an envelope names
  at most 32 readers, the owner's generations among them (contract 3.1): an
  address that more keys read cannot be sealed.
- Every opening verifies the whole history, one event after another, and
  holds it in memory (N2.6): a door opened on 300,000 events in 44 s and held
  about 3 KB for each.
- Every recording writes whole slabs: with the default slab, about two
  megabytes for a small note. A small device takes a smaller slab.
- What was recorded is not deleted.

## Measured

On one machine: a vessel to 1 TiB and one thing in it of 2 GiB, a history of
a million events, eight thousand people named in one ledger and a million at
an open address, 128 writers at once, two thousand seeds in one lineage and
twenty-four through the command line. The numbers, the model they fit and
where each ends are in `rokh/docs/11-scale.md`; the benchmarks that give them
are in `rokh/bench` and `rokh/cmd/rokh`, and run only when asked.

## Never run

- Windows and Android: the core builds for both and was run on neither.
- exFAT and a synced folder. FAT32 and the test machines' own file systems
  were run.
- A real engine.
- Vessels larger than 16 GiB on a disk. Larger ones, to 1 TiB, were made,
  opened and written only in memory, with the slabs nobody wrote kept as
  their length (`rokh/docs/11-scale.md`).
