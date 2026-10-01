# State of this tree

This is the source of version 1.0.0 of Rokh. What is written here was seen in
runs on Linux and on macOS; nothing is called met that was not run.

## What is built

- A carrier is a folder that holds a sealed vessel: fixed in size, or
  growing by itself in whole slabs. Opened without Rokh it shows no name, no
  address and no count. It shows its capacity and when it was written
  (contract U1), and, in the clear, the salt and the cost of the passphrase,
  which are the same in every seed of one Rokh (contract 2.3).
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
  the design, 2 that only use in the field can answer. Some of the 140 rest
  on tests that do not compile or on paths nothing reaches; they are named
  under "Not built, or not finished".

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

Thirty-three more tests do not run in a fresh clone or in the checks, and say
so only in a verbose run: each wants a short folder for its sockets, looks for
one six or seven levels above its package, outside the tree, and skips when
there is none. Sixteen are the core's, in `rokh/daemon`, `rokh/cmd/rokh` and
`rokh/proof`; seventeen are the gate's. Among them are the tests of two persons
on two booths of one carrier, of a key's session on the owner's booth, of two
running daemons writing to one carrier, of the harness, and of the gate's
enclosure. Given a short folder, the core's sixteen passed in the audit; of the
gate's, twelve passed and five also want the sandbox the enclosure runs in.
What "Two booths on one protocol" and "Several doors on one carrier at once"
say above rests on these tests.

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
  behind a build tag (`legacy09`) and in no run; they were written for the
  carrier of the version before this one and no longer compile. Among them is
  the only test that an idle daemon writes nothing.
- The sentences of the surface were carried over from Persian one word at a
  time; some do not read as English (`go` closes the ledger). A pass over
  every sentence a person types or reads is owed.
- `rokh/docs/06-api.md` describes the booth of the version before this one
  and carries a note saying so. The cryptography document of the previous
  version was withdrawn from this tree: its statements about the carrier no
  longer held. A document for version 1 is owed.
- A commit writes every inventory segment, where the contract writes the
  changed ones (2.6), and verifies every segment twice: once a vessel passes
  4 GiB of capacity, each recording writes 1/4096 of the capacity (256 MiB at
  1 TiB), however little it holds, and takes about 5 µs for every slab
  (`rokh/docs/11-scale.md`).
- A door's answers take longer as the history grows: each commit drops the
  vessel's index, the references are found by opening every branch pointer
  ever recorded, and the ledger's order is made again whole. At 300,000
  events a write takes 1.2 s and a status 0.75 s (`rokh/docs/11-scale.md`).
  The index is made again by reading, verifying and opening every pack,
  content included, so an opening and the first answer after every commit
  read all the content a vessel holds: read in the code, and not measured
  beyond 2 GiB of content (`WORKPLAN.md`).
- Every grant, revocation and keyring change keeps its own copy of all of
  them in its causal past, so their memory grows as the square of the keys
  named: 2 GiB for grants to 8,000 keys (`rokh/docs/11-scale.md`).
- Every opening verifies the whole history again and holds it in memory, and
  nothing keeps a verification from one opening to the next: a door opened on
  300,000 events in 44 s and held about 3 KB for each. N2.6 asks that one
  person can verify the whole ledger, not that every opening does
  (`rokh/docs/11-scale.md`).
- The writing turn goes to whichever door tries first once it is free, not to
  the one that asked first: with many doors writing, the slowest wait far
  longer than the rest, and among 128 concurrent writers on 32 doors, all
  with one key, 11 writes of 1,024 were refused after waiting 15 s
  (`rokh/docs/11-scale.md`).
- `rokh-courier apply`, `rokh bind` and `rokh bound` speak the protocol of
  the version before this one. Against `rokh daemon` the courier is refused
  (`hello_first`) at its first request, reports that as one rejected event,
  and exits 0. A booth's `capabilities` still names its protocol
  `rokh.daemon/3`. `transport`, `rokh-courier`, `rokh-forms`, `rokh-chest`
  and the command of `rokh-home` have no tests.
- On the sentence surface: `see the grants` ends in a panic on a ledger that
  holds an open grant. After a waiting sentence is rewritten, the answer says
  `write` records it, but `write` records another waiting sentence, and the
  rewritten one is let go when the session closes (T9.2). The guides lose
  every `key: `, taken for the name of a layer. Some refusals end with exit 0
  and no code; `see the ledgers` prints names without escaping them;
  `-library` changes nothing.
- Of the 140 obligations the conformance map counts as met: five rows (T2.2,
  T2.3, T11.10/enforced, T11.11/enforced, T13.5/covenant) rest on tests behind
  the build tag above, since the map's index of the tree ignores build tags;
  sixteen rest on one of eight constants nothing uses (`Measure`, `Witness`,
  `SeatOfJudgement`, `ClosedAndAlive` in `rokh/ledger`; `Complete`, `Custom`,
  `Position` in `rokh/arch`; `generation.Profile`); T1.1, T5.1 and N4.3 point
  at `FreshSize`, under a comment that belongs to `Event`; T6.5 rests on a
  type, `bond.Bound`, while a grant names no ending event in advance; T13.3
  rests on `working.Folder`, which nothing in production uses; and T4.1 rests
  on a search of the source that `weak.tsv` does not list.
- Five documents describe the version before this one without saying so:
  `docs/01` (RKH1, four reserved verbs, freshness by an oracle), `docs/04`
  (sizes its test no longer measures), `docs/05` (`rokh.json`,
  `.rokh/objects`, temporary files, content outside the carrier, tests that do
  not exist), `docs/07` (no TCP, no sealing, disclosure by the root alone) and
  `docs/08` (`seat.json`, the library). `docs/02` cites a test that does not
  exist. The README's `rokh seed SRC DST [--size …]` fails, since the flags go
  before the new folder; and it calls `rokh-forms` an adapter on the booth,
  which runs the command line and writes what it is given, as plaintext, to a
  folder outside the carrier (contract E5).
- About forty-five comments cite texts that are not in this tree (`goal 7.1`,
  `packet C.3`, `DEFECT K1` and the like). The owner's word of 2026-09-29, that
  nothing reconciles without being asked, is written only in a comment
  (`rokh/cmd/rokh/v1_vessel.go`, above `cmdReconcile`), while the contract's
  R6 asks seeds that meet to reconcile without being asked.
- A booth may listen on TCP at the loopback interface (`transport.Listen`),
  which the contract allows (B1) and N does not: "not one code path that opens
  a network socket" (the requirement under Axiom 5), and a door for programs
  on a Unix socket in its base profile (§4, 4.10). No ruling in this tree
  settles which holds. Tests open TCP on the loopback interface.
- A booth reads and parses a message of up to 8 MiB before its session is
  bound, and its listener takes any number of connections at once
  (`booth.MaxLine`, `transport.Serve`); one run of the audit, not reproduced,
  held about 2.3 GB for twelve connections.
- The home keeps a catalog, a journal or the preview of an import in one
  pointer record, which must fit in one slab: about 2,000 items in a catalog,
  or 6,000 files in a preview (one run of the audit, not reproduced). The
  comment on `BytesWithHash` says its memory does not grow with the file's
  size; it does, since the store assembles the whole object before the
  window is cut (`vesselstore.Store.Get`, read in the code).
- An offer of hosting with an end arms a timer in the gate; when it fires,
  the gate takes the hosting back, stops the programs and seals the engine's
  last state, at a moment nobody asked for (`rokh-home/gate/server.go`,
  `RevokeHosting`).
- On Windows the command line cannot ask for a passphrase: the prompt opens
  `/dev/tty` and runs `/bin/stty` (`rokh/passphrase`). `rokh/build.sh` builds
  for neither Windows nor Android, and passes over a target that fails to
  build.
- The checks run on Linux alone, with no look at formatting, no build for
  another system, no count of skips and no look for files a run changed.
  Nineteen sleeps in fourteen test files, a test that rewrites
  `conformance/STATE.md` beside its package, and tests that open TCP on the
  loopback interface go against the rules AGENTS.md sets for tests.

## Not promised, by design

- A copy of the folder made while a writer records more than three times
  is not promised to open. Copy when nothing writes. The contract calls this
  a gap, G1, red until closed, and not a limit of nature (U3).
- Two machines writing one synced folder is not supported. Each machine
  takes a seed.
- What a key could read before it was taken back, it may have kept.
- At most 31 keys beside the owner's open one vessel; and in version 1 an
  envelope names at most 32 readers, the owner's generations among them
  (contract 3.1), so an address that more keys read cannot be sealed.
- Every recording writes whole slabs: with the default slab, about two
  megabytes for a small note. A small device takes a smaller slab.
- What was recorded is not deleted.

## Measured

On one machine: vessels of up to 1 TiB of capacity, each holding a few notes,
and one thing of 2 GiB of content; a history of a million events; grants to
8,000 keys in one ledger and a million keys at an open address; 128
concurrent writers with one key; two thousand seeds in one lineage and
twenty-four through the command line. What was measured, what was read in the
code, what is extrapolated and what is proposed are kept apart in
`rokh/docs/11-scale.md`, with its raw data in `rokh/docs/11-scale/`; the
benchmarks are in `rokh/bench` and `rokh/cmd/rokh`, and run only when asked.

## Never run

- Windows and Android: the core builds for both and was run on neither.
- exFAT and a synced folder. FAT32 and the test machines' own file systems
  were run.
- A real engine.
- Vessels of more than 16 GiB of capacity on a disk. Larger capacities, to
  1 TiB, were made, opened and written only in memory, with the slabs nobody
  wrote kept as their length, and held a few notes each
  (`rokh/docs/11-scale.md`).
