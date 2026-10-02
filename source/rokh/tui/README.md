# The screen of Rokh

This package draws Rokh's sentence surface on a terminal: the same sentences
the line surface reads one at a time, with the ledger in view while a person
types. It is the art of the terminal, and it is part of the source.

It imports nothing of Rokh's. The shell hands it a `Snapshot` of a session it
has already verified and takes back the sentence a person typed, byte for
byte; the shell parses it, judges it and records it. This package creates no
event, opens no carrier, holds no key and knows no socket. Delete it and Rokh
loses a way of looking, and nothing else.

It builds wherever Rokh builds. The terminal's raw mode and the window's size
go through one small seam: `term_unix.go` drives `stty` on the systems that
have it, `term_other.go` has nothing on the rest. Where the seam has nothing,
the screen is not started; the line surface is, and says why.

## See it without a ledger

    go run ./cmd/rokh-shell -demo

draws every screen from a made-up ledger that ships with the source
(`sample.json`, the same as `Sample()`), at the size of the terminal it runs
in, first in colour and then plain. It opens nothing, asks for no passphrase
and records nothing. To walk through the screens with the keys:

    go run ./cmd/rokh-shell -state tui/sample.json

Every sentence said to that picture is answered that there is no ledger
behind it; `go` leaves. Any snapshot written as JSON is drawn the same way.

## The screens

Every view of the sample at every layout is drawn as a file under
`screens/`, one per layout and view (`medium-ledger.txt`, `rail-layers.txt`,
and so on). The tests make them and check them, and check that each screen
below is the file it names, so what this page shows is what the code draws.

The ledger at 80×24:

<!-- screen: screens/medium-ledger.txt -->
```text
╭─ ROKH ──────────────────────────────────────────────────────────────────────╮
│ ◇ ROKH  ·  ledger home  ·  anchor a0b6443e  ·  custody warm                 │
│ 3 accepted and 4 system  ·  0 pending  ·  0 rejected  ·  1 head             │
│ ▸ 1 ledger    2 sentence    3 authority    4 layers     58 MB free of 64 MB │
╰─────────────────────────────────────────────────────────────────────────────╯
╭─ LEDGER ────────────────────────────────────────────────────────────────────╮
│ THE CHAIN, IN CAUSAL ORDER                                                  │
│ I am standing at 8c91d2af, the only head of home.                           │
│ ▸ 1 sentence waiting in working state — write records, cancel lets go       │
│                                                                             │
│ ● ACCEPTED  8c91d2af  note at home/journal/today                            │
│   “Walked to the river before work; the water was high.”                    │
│   signed by root · under the owner's own right · through rokh-shell         │
│ ● ACCEPTED  72b4e191  system  rokh.revoke at rokh                           │
│   grant 0be21d6c taken back, from here on                                   │
│   signed by root · under the owner's own right · through rokh-shell         │
│ ● ACCEPTED  5c1f09aa  note at work/plans                                    │
│   “Draft the budget by Friday.”                                             │
│   signed by assistant · under grant 5f20c331 · through rokh-home/program    │
╰─────────────────────────────────────────────────────────────────────────────╯
╭─ SAY ONE SENTENCE ──────────────────────────────────────────────────────────╮
│ › █ try write  ·  ? for the list                                            │
╰─────────────────────────────────────────────────────────────────────────────╯
  tab or ←→ view   ? sentences   1–4 jump   "go" or Ctrl-C leaves
```

A long answer being read, at 80×24:

<!-- screen: screens/medium-reading.txt -->
```text
╭─ ROKH ──────────────────────────────────────────────────────────────────────╮
│ ◇ ROKH  ·  ledger home  ·  anchor a0b6443e  ·  custody warm                 │
│ 3 accepted and 4 system  ·  0 pending  ·  0 rejected  ·  1 head             │
│ ▸ 1 ledger    2 sentence    3 authority    4 layers     58 MB free of 64 MB │
╰─────────────────────────────────────────────────────────────────────────────╯
╭─ ROKH SAYS ─────────────────────────────────────────────────────────────────╮
│ — home/journal/day01 (3a7c1e61): the note of day 1, as it was written       │
│ — home/journal/day02 (3a7c1ec2): the note of day 2, as it was written       │
│ — home/journal/day03 (3a7c1f23): the note of day 3, as it was written       │
│ — home/journal/day04 (3a7c1f84): the note of day 4, as it was written       │
│ — home/journal/day05 (3a7c1fe5): the note of day 5, as it was written       │
│ — home/journal/day06 (3a7c2046): the note of day 6, as it was written       │
│ — home/journal/day07 (3a7c20a7): the note of day 7, as it was written       │
│ — home/journal/day08 (3a7c2108): the note of day 8, as it was written       │
│ — home/journal/day09 (3a7c2169): the note of day 9, as it was written       │
│ — home/journal/day10 (3a7c21ca): the note of day 10, as it was written      │
│ — home/journal/day11 (3a7c222b): the note of day 11, as it was written      │
│ — home/journal/day12 (3a7c228c): the note of day 12, as it was written      │
│ 12 events at home/journal.                                                  │
╰─────────────────────────────────────────────────────────────────────────────╯
╭─ SAY ONE SENTENCE ──────────────────────────────────────────────────────────╮
│ › █ try write  ·  ? for the list                                            │
╰─────────────────────────────────────────────────────────────────────────────╯
  ↑↓ PgUp PgDn page the answer   esc back   "go" or Ctrl-C leaves
```

The sentence view in the rail, at 48×20:

<!-- screen: screens/rail-sentence.txt -->
```text
◇ ROKH  ·  ledger home  ·  custody warm
3 accepted · 4 system · 0 pending · 0 rejected
  1 ledger ▸ 2 sent.   3 auth.   4 layers
room 58 MB free of 64 MB
│ 1 SENTENCE WAITING
│ NEWEST     1
│ ADDRESS    home/journal/today
│ SIGNER     root
│ AUTHORITY  the owner's own right
│ PARENT     8c91d2af
│ PAYLOAD    42 bytes
│ 
│ NOT RECORDED
│ write records it; no undo.
│ cancel lets it go.
│ 
│ 
│ 
› █ try write  ·  ? for the list
  ? sentences   "go" or Ctrl-C leaves
```

The layers of a wide window that is short, at 120×12:

<!-- screen: screens/compact-layers.txt -->
```text
◇ ROKH  AN INDIVIDUAL'S EVENT LEDGER  ·  ledger home  ·  anchor a0b6443e  ·  custody warm
3 accepted events and 4 system events  ·  0 pending events  ·  0 rejected events  ·  1 head
Star  a0b6443e  ·  4 system events
Planet  home  ·  2 events  ·  Moons journal
Planet  work  ·  1 event  ·  Moons plans





› █ try read home  ·  ? for the list
  tab or ←→ view   ? sentences   1–4 jump   "go" or Ctrl-C leaves
```

## The views

- **ledger** — the chain, newest first: each event on three rows (its verdict,
  id, verb and address; what it holds; who signed it, under what, after what
  and through which door). The ledger's own events are marked `system` and
  said as what they did. A ledger with nothing of the person's yet shows
  START HERE: the two sentences that make a first note.
- **sentence** — every sentence waiting in working state, with the key that
  would sign it, and what `write` and `cancel` do.
- **authority** — what is entrusted, what is opened to whom, what was taken
  back.
- **layers** — the only part of the layout shown: the Star is the rokh itself,
  a Planet the first component of an address, a Moon the second; what lies
  deeper is listed inside its Moon.

The list of sentences opens over any view. Each sentence says in plain words
what it does, then exactly, and the seven that record an event are marked
`● records`.

## The layouts

The layout follows the window's size. Every one fills the window exactly and
its last row is the hint line, which always says how to leave; the last
column is never used, so nothing wraps.

| layout | from | what it is |
|---|---|---|
| wide | 96×27 | the view and a standing panel side by side, the answer and the prompt under them |
| medium | 58×22 | one framed view, the answer and the prompt under it |
| rail | 34×15 | the view down the left edge, unframed |
| compact | 34×6 | a wide window that is short: one row per event, the answer, the prompt |
| tiny | below | who and where, the tally or the answer, the prompt, the hints |

The header keeps custody whole and shortens the rest first; the room is shown
as the vessel holds it. An answer wraps by words and never hides the newest
event; a longer one says which lines are shown and which key reads the rest.

## The six colours

Colour is meaning, not decoration; each colour carries one of Rokh's
distinctions, and a colour never replaces a word, a glyph or a verdict.

| meaning | value | at 256 colours | at 16 |
|---|---|---|---|
| place: vault, ledger, address, view | `#2E5AAC` | nearest | blue |
| working state: draft, preview, pending | `#D99800` | nearest | yellow |
| settled history: accepted, verified | `#009B95` | nearest | cyan |
| the recording boundary: record, refuse, revoke | `#C4462D` | nearest | red |
| authority: owner, key, grant, custody | `#5B7F2C` | nearest | green |
| lineage: anchor, id, parent, head | `#8B58B8` | nearest | magenta |

Frames and hints are dim; payloads and ordinary words take no colour.
`COLORTERM=truecolor` or `24bit` gets the values themselves, a `TERM` that
names 256 colours the nearest of those, any other terminal the sixteen.

## Plain

`NO_COLOR` set, or an output that is not a terminal, draws without colour;
every screen reads the same. `TERM=dumb` or unset, a terminal whose mode
cannot be set, and `-plain` get the line surface instead of the screen: one
sentence a line, the same answers.

## Keys

| key | does |
|---|---|
| type, `Enter` | the sentence, in any script, said |
| `?` or `/` on an empty prompt; `help`, `sentences` | the list; type to find, `↑` `↓`, `Enter` places a sentence |
| `←` `→` `Home` `End` `Ctrl-A` `Ctrl-E` | the cursor, in the line being typed |
| `Backspace` `Delete` `Ctrl-U` `Ctrl-K` `Ctrl-W` | take a rune, the start, the rest, a word |
| `↑` `↓` | the sentences of this session, in memory only |
| `↓` `↑` `PgDn` `PgUp` on a long answer | read it whole |
| `Esc` | out of a long answer; again, close it; close the list |
| `Tab` `←` `→` `1`–`4` | change view, while nothing is typed |
| a paste | text only: a line break in it never presses `Enter` |
| `go`, `Ctrl-C`, `Ctrl-D` on an empty prompt | leave |

## The golden screens

The screens are pinned as files under `testdata/`; `go test ./tui` draws each
and compares. A change to what a person sees is a change somebody means:

    go test ./tui -run 'TestGoldenScreens|TestTheShippedScreens|TestTheSampleFileIsTheSample' -update

rewrites them, the shipped screens under `screens/` and the screens on this
page with them, and the difference is read as a picture before it is kept.
