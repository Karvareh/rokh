# The screen surface

The same sentences as the line surface, drawn on a terminal that is measured,
coloured by meaning and redrawn as the ledger changes. `rokh` on a terminal
goes through the gate and opens the screen; a pipe, a script or `-plain` gets
the line surface instead, and so does a terminal that cannot be drawn on
(`TERM=dumb` or unset, or no way to set its mode), which is told so in one
sentence. They are one surface. Only the drawing differs. The package's own
page, `tui/README.md`, says the same as this one about the screen, with the
screens as they are drawn.

## The gate

What happens between typing `rokh` and standing at a ledger, in the person's
order, not the machine's. Before the first question the gate says what a Rokh
is (a folder that keeps your notes and files sealed under a passphrase), that
nothing is written until the last question is answered, and that nothing
shows while a passphrase is typed.

1. **The folder.** `rokh` alone asks which folder is your Rokh, with the tool
   the system already has for choosing folders — the Finder's dialog on a
   Mac, `zenity` or `kdialog` on a Linux desktop — and by a typed path where
   there is no desktop (over ssh, on a server). Closing the dialog is an
   answer: nothing is opened, and the person is told how to try again.
   `rokh PATH` names the folder and skips the asking.
2. **What is on it.** A quick look that writes nothing. Two shapes are a
   Rokh: a folder of ledgers, `ledgers/<name>/`, holding at least one
   carrier, as the gate makes it; and a carrier itself, as `rokh init` makes
   it, which opens as the session's one ledger. A carrier is known by its
   vessel's head file, `rokh/head0.rkh`, which exists to be recognized before
   any passphrase is known. An empty folder is not a Rokh, and neither is one
   with other things in it.
3. **Then the passphrase**, and only then; nothing shows while it is typed. A
   wrong one is said to be wrong and asked for again, three times in all; one
   that came from a file or the environment is tried once. With one ledger,
   that ledger is opened and stood at without a sentence; with more, the
   screen says none is open and the choosing is the person's.
4. **Or make one.** An empty folder — or a path that is not there yet — is
   offered: *make your Rokh here?* Yes asks:
   - **how much room** it should have: the size of its vessel, in whole
     slabs, at least 4 MB, no more than the disk has free; Enter is 64 MB.
     Then whether it should **grow by itself** when it fills: a quarter of
     its size at a time, up to the room the disk had free. `rokh grow` and
     `rokh shrink` change the size later.
   - **a passphrase**, twice; two that differ are asked for again, three
     times in all. There is no recovery without it.
   - **the name of the ledger**: one word, no slash, not beginning with a
     dot, a name the sentences can say back. It becomes a folder's name. A
     name that breaks the rule is refused with the reason and asked for
     again.

   The first event is then recorded: the one canonical sentence that would
   have made the ledger at the prompt, `open a new ledger named …`, with that
   name, signed, with the oracles' attestations, and nothing else — the size
   lives in the vessel. The folder holds `ledgers/<name>/` and nothing else.
   Any other answer makes nothing.

Nothing in the gate is recorded except that one first event, and only after
the last answer. No default vault exists and none is chosen for anyone. In a
script or a pipe there is nobody to ask, so the folder is named on the
command line and the passphrase comes by the one passphrase rule
(`ROKH_PASSPHRASE_FILE`, then `ROKH_PASSPHRASE`).

What is sealed, said as it is: the notes and files are sealed and open to
nobody without the passphrase. The name given to a ledger is a folder's name,
and the vessel is a folder of equal-sized slab files and four head files
whose sizes anyone who sees the folder can see.

## Placement

`tui/` sits beside `shell/` at the same layer and imports nothing of Rokh's.
The shell fills a `Snapshot` from a verified session and hands it over; the
screen hands back the sentence a person typed, byte for byte. Parsing,
judging and recording never leave the shell. The package creates no event,
opens no carrier, holds no key and knows no socket; `arch/` holds it to that.
It names no signal either: the command that starts it hands it the signals
that end the program, and on any of them the screen puts the terminal back as
it found it — its mode, the main screen, the cursor, bracketed paste, colour —
between keys, never in the middle of a sentence being said.

The terminal's size and its raw mode go through one small seam:
`tui/term_unix.go` drives `stty` on the systems that have it, and
`tui/term_other.go` has nothing on the rest; where the seam has nothing, the
screen is not started and the line surface is, and says why. A terminal in raw mode does no output processing, so the
frame sent to it ends every row but the last with a carriage return before
its newline — a bare newline in raw mode moves down without returning, and a
screen sent that way comes out as a staircase — and the last row, the
terminal's own last row, is not followed by a line break, which would scroll
the screen. `Render` separates rows by bare newlines, because it is the
screen as a file holds it (the golden files are this); `Frame` is what the
terminal is sent, and it redraws in place: home the cursor, each row erased
beyond its own end, everything below erased. No blanking between frames.

A screen can be drawn with no ledger at all. `rokh-shell -demo` draws every
screen from the made-up ledger the source ships (`tui/sample.json`), at the
terminal's size, in colour and then plain, and records nothing;
`rokh-shell -state FILE` draws one snapshot written as JSON and runs it with
the keys, and every sentence said to it is answered that there is no ledger
behind it.

## Voice

- Rokh speaks in the first person only for its present position: "I am
  standing at the head of the chain."
- Everything else is concrete: ledger, event, address, parent, authority,
  carrier, working state, boundary.
- A reply names what happened and, when it protects the owner, what did not.
  A refusal says in one or two lines what happened, whether anything was
  recorded and what to do next; the core's own words (`vessel:`, slabs,
  generations, the numbers of rulings) are translated, never shown.
- Payload and slot text belong to the person. It is drawn safely — a control
  byte or a bidirectional override is shown as its escape — and never
  translated, folded or rewritten.
- The terminal is technical English, whole.

## Words kept apart

- `accepted` — complete ancestry and a settled verdict. Not *true*.
- `verified` — bytes, signature, lineage and presented authority checked. Not *honest*.
- `pending` — ancestry has not arrived. Not *absent*.
- `entrusted` — a bounded writing authority exists. Not *consent*.
- `opened to` — disclosure is permitted. Not *sent*.
- `working state` — outside the ledger; a waiting sentence has no id.
- `write` — the recording boundary. Before it, no event; after it, no undo.
  Several sentences may wait; `write {n}` names one, `cancel` lets one go.
- `signed by … under … through …` — the key, the grant and the door: three
  questions kept apart. None of them is who composed the words.
- `system` — the ledger's own events (its making, keys, grants, seeds,
  merges), in the same line as any event, said as what they did, never as
  bytes, and counted apart from the person's.
- `warm` / `delegated` / `cold` — custody, in one word: the root key is at
  hand; only an entrusted key is; none is.
- `taken back` — closes the future; erases nothing.
- `carry` — a copy, an export or a permitted bundle; never a silent sync.
- No clock orders events. Parentage does.

## Six colours, six meanings

| role | hex | means |
|---|---|---|
| place | `#2E5AAC` | vault, ledger, address, current view |
| working | `#D99800` | draft, preview, pending — not settled |
| settled | `#009B95` | accepted events, verification that passed |
| boundary | `#C4462D` | recording, rejection, refusal, revocation |
| authority | `#5B7F2C` | owner, key, grant, covenant, custody |
| lineage | `#8B58B8` | anchor, id, parent, head, berth, reunion |

Neutral text carries payloads and ordinary grammar. Colour never replaces a
word, a glyph or a verdict; every screen reads the same with `NO_COLOR` set.
A terminal whose `COLORTERM` says `truecolor` or `24bit` is given these values;
one whose `TERM` names 256 colours is given the nearest of those; any other
the sixteen, by meaning (place blue, working yellow, settled cyan, boundary
red, authority green, lineage magenta). `NO_COLOR`, or a `TERM` that is dumb
or unset, means none.

## The views

Four, on every layout: the **ledger** (the chain, newest first, and, on a
ledger with nothing of the person's yet, a START HERE panel with the two
sentences that make a first note), the **sentence** (every sentence waiting
in working state, with what would sign it), **authority** (what is entrusted,
opened and taken back), and **layers** — the only part of the layout the
screen shows: the Star is the rokh itself, a Planet the first component of
an address, a Moon the second, and what lies deeper is listed inside its
Moon. The list of sentences opens over any of them; each sentence says in
plain words what it does, then exactly, and the seven that record an event
are marked `● records`.

## Keys

| key | does |
|---|---|
| type | the sentence, in any script; `Enter` says it |
| `?` or `/` on an empty prompt, or `help`, `sentences`, `?` then `Enter` | the list of sentences, searchable by typing; on the line surface the same words print it |
| `↑` `↓` in the list | move; `Enter` places the sentence up to its first slot |
| `←` `→` `Home` `End`, `Ctrl-A` `Ctrl-E` | move the cursor in the line being typed |
| `Backspace` `Delete`, `Ctrl-U` `Ctrl-K` `Ctrl-W` | take the rune before or under the cursor; everything before it, everything after it, the word before it |
| `↑` `↓` on the prompt | the sentences said in this session, kept in memory only |
| `↓` `↑` `PgDn` `PgUp` while an answer is longer than its box | read it whole, at full height |
| `Esc` | out of a long answer; again, close it and clear the prompt; close the list |
| `Tab` `←` `→` `1` `2` `3` `4` | change view, while nothing is typed |
| a paste | is text: a line break in it is kept and drawn as `\n`, and never presses `Enter` |
| `leave`, `Ctrl-C`, or `Ctrl-D` on an empty prompt | leave; sentences still waiting are named as let go |

A slash inside a sentence is part of the sentence: `read home/journal` means
what it says. The list opens only when the prompt is empty. While nothing is
typed, the prompt offers the sentence likely on the view in sight.

## Layouts

Five, by the terminal's size: two panes from 96×27, one framed pane from
58×22, a rail from 34×15, a compact one for a window that is wide and short
(from 34×6), and four rows below that. Every layout fills the window exactly,
and its last row is the hint line, which always says how to leave. The last
column is never used, so nothing wraps. Wide runes and emoji are measured by
cell. The header keeps custody whole, shortening the rest first; the room is
shown as the vessel holds it (free of how much, or full and how room is
made); an answer wraps by words and never hides the newest event; a longer
answer says which lines are shown and which key reads the rest. The screens
are pinned as golden files under `tui/testdata/`, and every view of the
sample at every layout ships drawn under `tui/screens/`.
