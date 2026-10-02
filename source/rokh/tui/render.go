package tui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A screen is built from lines, a line from spans, a span from text with one
// meaning. Nothing below this comment knows what a ledger is; it knows what a
// cell is and where a frame ends.

type span struct {
	text string
	role Role
	bold bool
}

type line []span

func text(v string) line            { return line{{text: inline(v)}} }
func tint(v string, r Role) line    { return line{{text: inline(v), role: r}} }
func strong(v string, r Role) line  { return line{{text: inline(v), role: r, bold: true}} }
func pieces(vs ...span) line        { return line(vs) }
func s(v string, r Role) span       { return span{text: inline(v), role: r} }
func sb(v string, r Role) span      { return span{text: inline(v), role: r, bold: true} }
func (l line) with(vs ...span) line { return append(l, vs...) }
func repeat(v string, n int) string { return strings.Repeat(v, max(0, n)) }
func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// inline keeps text from commanding the terminal. A payload is the person's,
// and it is shown — but a newline, a control byte or a bidirectional override
// inside it would move the cursor, forge a status line or break a frame, so
// each is drawn as its escape. ZWNJ and other joiners are ordinary writing in
// several scripts and stay literal. The same discipline the line surface
// applies before it prints a payload.
func inline(v string) string {
	var out strings.Builder
	for _, r := range v {
		switch {
		case r == '\n':
			out.WriteString(`\n`)
		case r == '\r':
			out.WriteString(`\r`)
		case r == '\t':
			out.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&out, `\x%02x`, r)
		case r >= 0x80 && r <= 0x9f:
			fmt.Fprintf(&out, `\u%04x`, r)
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			fmt.Fprintf(&out, `\u%04x`, r)
		case r == utf8.RuneError:
			out.WriteString(`�`)
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// cells is how many terminal columns a rune takes: none for marks and
// joiners, two for the East Asian wide ranges and emoji, one otherwise. It is
// an approximation without a table behind it, and it is right for what a
// ledger is likely to hold.
func cells(r rune) int {
	if r == 0 || unicode.IsControl(r) || unicode.Is(unicode.Mn, r) ||
		unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	switch {
	case r >= 0x1100 && r <= 0x115f,
		r == 0x2329 || r == 0x232a,
		r >= 0x2e80 && r <= 0xa4cf && r != 0x303f,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe10 && r <= 0xfe19,
		r >= 0xfe30 && r <= 0xfe6f,
		r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1faff,
		r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

func width(v string) int {
	n := 0
	for _, r := range v {
		n += cells(r)
	}
	return n
}

func (l line) width() int {
	n := 0
	for _, p := range l {
		n += width(p.text)
	}
	return n
}

// truncate cuts a line to a width, ending it with an ellipsis when it cut.
func (l line) truncate(w int) line {
	if w <= 0 {
		return nil
	}
	if l.width() <= w {
		return l
	}
	if w == 1 {
		return line{{text: "…", role: RoleMuted}}
	}
	target, used := w-1, 0
	out := line{}
	for _, p := range l {
		var b strings.Builder
		for _, r := range p.text {
			c := cells(r)
			if used+c > target {
				break
			}
			b.WriteRune(r)
			used += c
		}
		if b.Len() > 0 {
			out = append(out, span{text: b.String(), role: p.role, bold: p.bold})
		}
		if used >= target {
			break
		}
	}
	return append(out, span{text: "…", role: RoleMuted})
}

func (l line) pad(w int) line {
	l = l.truncate(w)
	return append(l, span{text: repeat(" ", w-l.width())})
}

func fit(rows []line, n int) []line {
	if n < 0 {
		n = 0
	}
	if len(rows) > n {
		return rows[:n]
	}
	for len(rows) < n {
		rows = append(rows, text(""))
	}
	return rows
}

func frame(title string, role Role, body []line, w int) []line {
	title = inline(title)
	if width(title) > w-5 {
		t := text(title).truncate(max(1, w-5))
		title = ""
		for _, p := range t {
			title += p.text
		}
	}
	top := pieces(s("╭─ ", RoleMuted), sb(title, role),
		s(" "+repeat("─", w-width(title)-5)+"╮", RoleMuted))
	out := []line{top.pad(w)}
	inner := max(0, w-4)
	for _, row := range body {
		out = append(out, pieces(s("│ ", RoleMuted)).with(row.pad(inner)...).with(s(" │", RoleMuted)))
	}
	return append(out, tint("╰"+repeat("─", w-2)+"╯", RoleMuted).pad(w))
}

func beside(left []line, lw int, right []line, rw, gap int) []line {
	n := max(len(left), len(right))
	out := make([]line, 0, n)
	for i := 0; i < n; i++ {
		l, r := text(""), text("")
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, l.pad(lw).with(s(repeat(" ", gap), RoleDefault)).with(r.pad(rw)...))
	}
	return out
}

// ---------- the parts of a screen ----------

// tally counts what the person recorded, the ledger's own events beside it,
// what waits and what was refused, and the heads.
func tally(st Snapshot) line {
	l := pieces(sb(countOf(st.Accepted, "accepted event"), RoleSettled))
	if st.System > 0 {
		l = l.with(s(" and "+countOf(st.System, "system event"), RoleSettled))
	}
	return l.with(s("  ·  ", RoleMuted),
		sb(countOf(st.Pending, "pending event"), RoleWorking), s("  ·  ", RoleMuted),
		sb(countOf(st.Rejected, "rejected event"), RoleBoundary), s("  ·  ", RoleMuted),
		sb(countOf(st.Heads, "head"), RoleLineage),
	)
}

func midTally(st Snapshot) line {
	l := pieces(sb(strconv.Itoa(st.Accepted)+" accepted", RoleSettled))
	if st.System > 0 {
		l = l.with(s(" and "+strconv.Itoa(st.System)+" system", RoleSettled))
	}
	return l.with(s("  ·  ", RoleMuted),
		sb(strconv.Itoa(st.Pending)+" pending", RoleWorking), s("  ·  ", RoleMuted),
		sb(strconv.Itoa(st.Rejected)+" rejected", RoleBoundary), s("  ·  ", RoleMuted),
		sb(countOf(st.Heads, "head"), RoleLineage),
	)
}

func shortTally(st Snapshot) line {
	l := pieces(sb(strconv.Itoa(st.Accepted)+" accepted", RoleSettled))
	if st.System > 0 {
		l = l.with(s(" · "+strconv.Itoa(st.System)+" system", RoleSettled))
	}
	return l.with(s(" · ", RoleMuted),
		sb(strconv.Itoa(st.Pending)+" pending", RoleWorking), s(" · ", RoleMuted),
		sb(strconv.Itoa(st.Rejected)+" rejected", RoleBoundary),
	)
}

// tabs names the views, the current one marked. A narrow row closes the gaps
// first, then shortens the names, and only then cuts.
func tabs(v View, w int) line {
	build := func(gap string, names []string) line {
		row := line{}
		for i, name := range names {
			if i > 0 {
				row = row.with(s(gap, RoleDefault))
			}
			prefix, role := "  ", RoleMuted
			if i == v.Screen && !v.MenuOpen {
				prefix, role = "▸ ", RolePlace
			}
			row = row.with(sb(strings.TrimRight(prefix+strconv.Itoa(i+1)+" "+name, " "), role))
		}
		return row
	}
	short := make([]string, len(ScreenNames))
	for i, name := range ScreenNames {
		short[i] = name
		if len(name) > 6 {
			short[i] = name[:4] + "."
		}
	}
	// Last before cutting: the other views by number only, the current one
	// by name as well.
	numbers := make([]string, len(ScreenNames))
	for i := range numbers {
		if i == v.Screen && !v.MenuOpen {
			numbers[i] = short[i]
		}
	}
	for _, t := range []line{build("     ", ScreenNames[:]), build("  ", ScreenNames[:]), build(" ", short), build(" ", numbers)} {
		if t.width() <= w {
			return t
		}
	}
	return build(" ", numbers).truncate(w)
}

// identity is the first line of the header: what this is, which ledger, its
// anchor and its custody. Custody is never cut: when the row is narrow the
// words that name the product go first, then the anchor, and the ledger's
// name is shortened last.
func identity(st Snapshot, w int) line {
	brand := sb("◇ ROKH", RolePlace)
	long := s("  AN INDIVIDUAL'S EVENT LEDGER", RoleDefault)
	if st.Ledger == "" {
		for _, t := range []line{
			pieces(brand, long, s("  ·  ", RoleMuted), s("no ledger open", RoleWorking)),
			pieces(brand, s("  ·  ", RoleMuted), s("no ledger open", RoleWorking)),
		} {
			if t.width() <= w {
				return t
			}
		}
		return pieces(brand, s(" · no ledger open", RoleWorking)).truncate(w)
	}
	name := sb(st.Ledger, RolePlace)
	custody := pieces(s("  ·  custody ", RoleMuted), sb(st.Custody, RoleAuthority))
	anchor := pieces(s("  ·  anchor ", RoleMuted), sb(st.Anchor, RoleLineage))
	for _, t := range []line{
		pieces(brand, long, s("  ·  ledger ", RoleMuted), name).with(anchor...).with(custody...),
		pieces(brand, s("  ·  ledger ", RoleMuted), name).with(anchor...).with(custody...),
		pieces(brand, s("  ·  ledger ", RoleMuted), name).with(custody...),
		pieces(brand, s("  ·  ", RoleMuted), name).with(custody...),
	} {
		if t.width() <= w {
			return t
		}
	}
	room := w - pieces(brand, s("  ·  ", RoleMuted)).width() - custody.width()
	return pieces(brand, s("  ·  ", RoleMuted)).with(line{name}.truncate(max(1, room))...).with(custody...).truncate(w)
}

// status is the second line of the header: the tally, as long as it fits.
func status(st Snapshot, w int) line {
	if st.Ledger == "" {
		return pieces(s("vault ", RoleMuted), sb(st.Vault, RolePlace)).truncate(w)
	}
	for _, t := range []line{tally(st), midTally(st)} {
		if t.width() <= w {
			return t
		}
	}
	return shortTally(st).truncate(w)
}

func header(st Snapshot, v View, w int) []line {
	inner := max(1, w-4)
	return frame("ROKH", RolePlace, []line{identity(st, inner), status(st, inner), tabsAndRoom(st, v, inner)}, w)
}

// tabsAndRoom is the tabs, with the room at the right end of the row when
// both fit whole; the tabs close their gaps first to make it fit.
func tabsAndRoom(st Snapshot, v View, w int) line {
	if st.Room == "" || st.Ledger == "" {
		return tabs(v, w)
	}
	note := roomNote(st)
	t := tabs(v, w-note.width()-3)
	for _, p := range t {
		if strings.Contains(p.text, "…") {
			return tabs(v, w)
		}
	}
	return t.with(s(repeat(" ", w-t.width()-note.width()), RoleDefault)).with(note...)
}

// roomNote is the room in one phrase: what is free of how much, or full.
func roomNote(st Snapshot) line {
	if st.Full {
		return pieces(sb("full", RoleBoundary), s(" · rokh grow makes room", RoleMuted))
	}
	return pieces(s(st.Free+" free of "+st.Room, RoleMuted))
}

func verdictRole(v string) Role {
	switch v {
	case "accepted":
		return RoleSettled
	case "pending":
		return RoleWorking
	case "rejected":
		return RoleBoundary
	}
	return RoleMuted
}

func noLedger(n int) []line {
	return fit([]line{
		strong("NO LEDGER IS OPEN", RoleWorking),
		text(""),
		pieces(s("Say ", RoleDefault), sb("open the ledger {name}", RolePlace), s(" to open one that exists,", RoleDefault)),
		pieces(s("or ", RoleDefault), sb("open a new ledger named {name}", RoleLineage), s(" to make one.", RoleDefault)),
		text(""),
		pieces(s("\"/\" lists every sentence. Nothing is recorded until you say so.", RoleMuted)),
	}, n)
}

func ledgerBody(st Snapshot, n, w int) []line {
	if st.Ledger == "" {
		return noLedger(n)
	}
	standing := "the only head"
	if st.Heads != 1 {
		standing = "one of " + countOf(st.Heads, "head")
	}
	rows := []line{
		strong("THE CHAIN, IN CAUSAL ORDER", RolePlace),
		pieces(s("I am standing at ", RoleDefault), sb(st.Head, RoleLineage), s(", ", RoleDefault),
			sb(standing, RoleLineage), s(" of ", RoleDefault), sb(st.Ledger, RolePlace), s(".", RoleDefault)),
	}
	// A sentence in working state is not in the chain, and a person looking
	// at the chain must not lose track of it: it is theirs until "write",
	// and gone with the session if they never say so.
	if n := len(st.Drafts); n > 0 {
		rows = append(rows, pieces(sb("▸ "+countOf(n, "sentence")+" waiting", RoleWorking),
			s(" in working state — ", RoleMuted), sb("write", RoleBoundary), s(" records, ", RoleMuted),
			sb("cancel", RoleWorking), s(" lets go", RoleMuted)))
	}
	if n >= 10 {
		rows = append(rows, text(""))
	}
	// A ledger that holds nothing of the person's yet says how to begin: the
	// two sentences that make a first note, and where the rest are.
	if st.Accepted == 0 && len(st.Drafts) == 0 && n >= 10 {
		rows = append(rows,
			strong("START HERE", RoleWorking),
			pieces(s("1  ", RoleMuted), sb("write at home/journal: my first note", RoleWorking), s("   puts it down, waiting", RoleMuted)),
			pieces(s("2  ", RoleMuted), sb("write", RoleBoundary), s("   records it; there is no undo", RoleMuted)),
			pieces(s("\"?\" lists every sentence; \"go\" leaves.", RoleMuted)),
			text(""))
	}
	// The events come before everything below them: the newest is what a
	// person just did, and it stays in view however little room is left.
	for _, e := range st.Events {
		if len(rows)+3 > n {
			break
		}
		role := verdictRole(e.Verdict)
		// A system event is the same three rows, marked system, and its
		// middle row is Rokh's sentence for what it did, not anybody's
		// words in quotation marks.
		head := pieces(sb("● "+strings.ToUpper(e.Verdict), role), s("  ", RoleDefault), sb(e.ID, RoleLineage))
		said := pieces(s("  “"+e.Payload+"”", RoleDefault))
		if e.System {
			head = head.with(s("  system", RoleLineage))
			said = pieces(s("  "+e.Payload, RoleMuted))
		}
		rows = append(rows,
			head.with(s("  "+e.Verb+" at ", RoleDefault), sb(e.Address, RolePlace)),
			said,
			provenance(e, w),
		)
	}
	if len(rows)+3 <= n {
		rows = append(rows, text(""))
	}
	if len(rows) < n {
		rows = append(rows, pieces(s("No clock orders this list; ", RoleMuted), sb("parentage does", RoleLineage), s(".", RoleMuted)))
	}
	if len(rows) < n {
		rows = append(rows, pieces(s("Verification checks the event, ", RoleMuted), sb("not the truth of its payload", RoleBoundary), s(".", RoleMuted)))
	}
	return fit(rows, n)
}

// provenance is the third row of an event: who signed it, under what, after
// what and through which door. Three questions and three answers, none of
// them who composed the words. A narrow row tightens its separators first,
// then leaves out the parent, which the chain above already shows.
func provenance(e EventItem, w int) line {
	build := func(sep string, parent bool) line {
		l := pieces(s("  signed by ", RoleMuted), sb(orUnnamed(e.Signer), RoleAuthority),
			s(sep+"under ", RoleMuted), sb(e.Authority, RoleAuthority))
		switch {
		case e.Parent == "":
			l = l.with(s(sep+"genesis", RoleLineage))
		case parent:
			l = l.with(s(sep+"after ", RoleMuted), sb(e.Parent, RoleLineage))
		}
		if e.Door != "" {
			l = l.with(s(sep+"through ", RoleMuted), sb(e.Door, RoleMuted))
		}
		return l
	}
	for _, t := range []line{build("  ·  ", true), build(" · ", true), build(" · ", false)} {
		if t.width() <= w {
			return t
		}
	}
	return build(" · ", false)
}

func orUnnamed(s string) string {
	if s == "" {
		return "an unnamed key"
	}
	return s
}

func field(label, value string, role Role) line {
	return pieces(s(label+repeat(" ", max(1, 11-width(label))), RoleMuted), sb(value, role))
}

func sentenceBody(st Snapshot, n int) []line {
	if st.Ledger == "" {
		return noLedger(n)
	}
	if len(st.Drafts) == 0 {
		return fit([]line{
			strong("NO SENTENCE IS WAITING", RoleSettled),
			text(""),
			pieces(s("Begin with ", RoleDefault), sb("write at {address}: {text}", RoleWorking), s(".", RoleDefault)),
			text("It enters working state first; nothing is recorded there."),
			text("Several may wait at once; each is recorded or let go by its own act."),
		}, n)
	}
	rows := []line{strong(strings.ToUpper(countOf(len(st.Drafts), "sentence"))+" WAITING", RoleWorking)}
	// The newest is the one a bare "write" or "cancel" takes; the others are
	// named by their number. All of them are shown, so nothing waits unseen.
	for i := len(st.Drafts) - 1; i >= 0; i-- {
		d := st.Drafts[i]
		mark := digits(d.Index) + "  "
		if i == len(st.Drafts)-1 {
			mark = digits(d.Index) + " ▸"
		}
		rows = append(rows,
			pieces(sb(mark, RoleWorking), s(" “"+d.Sentence+"”", RoleDefault)),
			pieces(s("     at ", RoleMuted), sb(d.Address, RolePlace), s("  ·  "+countOf(d.PayloadBytes, "byte"), RoleMuted),
				s("  ·  signed by ", RoleMuted), sb(orUnnamed(d.Signer), RoleAuthority),
				s(" under ", RoleMuted), sb(d.Authority, RoleAuthority),
				s("  ·  after ", RoleMuted), sb(d.Parent, RoleLineage)),
		)
		if len(rows) > n-6 {
			break
		}
	}
	rows = append(rows,
		text(""),
		strong("NOT RECORDED", RoleWorking),
		text("Nothing has crossed into the ledger."),
		pieces(sb("write", RoleBoundary), s(" crosses the recording boundary for the newest; ", RoleDefault), sb("write {n}", RoleBoundary), s(" for the n-th.", RoleDefault)),
		pieces(s("There is ", RoleDefault), sb("no undo", RoleBoundary), s(" past it.", RoleDefault)),
		pieces(sb("cancel", RoleWorking), s(" lets the newest go; ", RoleDefault), sb("cancel {n}", RoleWorking), s(" the n-th. Nothing is recorded.", RoleDefault)),
	)
	return fit(rows, n)
}

func narrowSentenceBody(st Snapshot, n int) []line {
	if st.Ledger == "" || len(st.Drafts) == 0 {
		return sentenceBody(st, n)
	}
	d := st.Drafts[len(st.Drafts)-1]
	return fit([]line{
		strong(strings.ToUpper(countOf(len(st.Drafts), "sentence"))+" WAITING", RoleWorking),
		field("NEWEST", digits(d.Index), RoleWorking),
		field("ADDRESS", d.Address, RolePlace),
		field("SIGNER", orUnnamed(d.Signer), RoleAuthority),
		field("AUTHORITY", d.Authority, RoleAuthority),
		field("PARENT", d.Parent, RoleLineage),
		field("PAYLOAD", countOf(d.PayloadBytes, "byte"), RoleDefault),
		text(""),
		strong("NOT RECORDED", RoleWorking),
		pieces(sb("write", RoleBoundary), s(" records it; ", RoleDefault), sb("no undo", RoleBoundary), s(".", RoleDefault)),
		pieces(sb("cancel", RoleWorking), s(" lets it go.", RoleDefault)),
	}, n)
}

func authorityBody(st Snapshot, n int) []line {
	if st.Ledger == "" {
		return noLedger(n)
	}
	rows := []line{strong("WRITING", RoleAuthority)}
	if len(st.Writing) == 0 {
		rows = append(rows, text("Nothing is entrusted."))
	}
	for _, it := range st.Writing {
		rows = append(rows,
			pieces(sb("● "+it.Subject, RoleAuthority), s("  "+it.Detail+" at ", RoleDefault), sb(it.Scope, RolePlace)),
			pieces(s("  grant ", RoleMuted), sb(it.ID, RoleLineage)))
	}
	rows = append(rows, text(""), strong("DISCLOSURE", RoleAuthority))
	if len(st.Disclosure) == 0 {
		rows = append(rows, text("Nothing is opened to anyone."))
	}
	for _, it := range st.Disclosure {
		rows = append(rows,
			pieces(sb("● "+it.Subject, RoleAuthority), s("  "+it.Detail+" at ", RoleDefault), sb(it.Scope, RolePlace)),
			pieces(s("  covenant ", RoleMuted), sb(it.ID, RoleLineage), s("  ·  nothing has been sent", RoleWorking)))
	}
	if len(st.TakenBack) > 0 {
		rows = append(rows, text(""), strong("TAKEN BACK", RoleBoundary))
		for _, it := range st.TakenBack {
			rows = append(rows, pieces(sb("● grant "+it.ID, RoleLineage), s("  "+it.Detail+" ", RoleMuted), sb(it.Subject, RoleLineage),
				s("  ·  closed from there on; its past stands", RoleMuted)))
		}
	}
	rows = append(rows, text(""),
		strong("FROM HERE ON", RoleBoundary),
		text("Taking authority back closes the future; it does not erase the past."))
	return fit(rows, n)
}

func standingBody(st Snapshot, n int) []line {
	rows := []line{
		strong("YOU ARE STANDING AT", RolePlace),
		field("VAULT", st.Vault, RolePlace),
	}
	if st.Ledger == "" {
		rows = append(rows, field("LEDGER", "none open", RoleWorking))
		return fit(rows, n)
	}
	rows = append(rows,
		field("LEDGER", st.Ledger, RolePlace),
		field("ANCHOR", st.Anchor, RoleLineage),
		field("HEAD", st.Head, RoleLineage),
		field("CUSTODY", st.Custody, RoleAuthority))
	// The room is the vessel's own: its size, what is free to write now,
	// and whether it grows by itself. A full Rokh says so, and how to make
	// room, in the boundary's colour.
	if st.Room != "" {
		free := RoleSettled
		if st.Full {
			free = RoleBoundary
		}
		rows = append(rows, field("ROOM", st.Room, RolePlace), field("FREE NOW", st.Free, free), field("GROWTH", st.Growth, RoleMuted))
		if st.Full {
			rows = append(rows, strong("FULL: rokh grow makes room", RoleBoundary))
		}
	}
	rows = append(rows, text(""))
	if st.Verified {
		rows = append(rows,
			strong("VERIFICATION SAYS", RoleSettled),
			pieces(sb("● ", RoleSettled), s("the stored bytes match", RoleDefault)),
			pieces(sb("● ", RoleSettled), s("every signature checks", RoleDefault)),
			pieces(sb("● ", RoleLineage), s("the ancestry is complete", RoleDefault)),
			pieces(sb("● ", RoleAuthority), s("the presented authority held", RoleDefault)),
			pieces(sb("NOT THIS: ", RoleBoundary), s("payload truth", RoleDefault)))
	} else {
		rows = append(rows, strong("NOT VERIFIED", RoleBoundary), text("this session has not checked this ledger"))
	}
	return fit(rows, n)
}

func matching(q string) []int {
	needle := strings.ToLower(strings.TrimSpace(q))
	out := make([]int, 0, len(Sentences))
	for i, sn := range Sentences {
		if needle == "" || strings.Contains(strings.ToLower(sn.Say+" "+sn.Plain+" "+sn.Does), needle) {
			out = append(out, i)
		}
	}
	return out
}

func menuBody(v View, n int) []line {
	idx := matching(v.Query)
	q := v.Query
	if q == "" {
		q = "type a verb to find a sentence"
	}
	rows := []line{
		pieces(s("FIND  ", RoleMuted), sb(q, RoleWorking), sb("█", RoleWorking)),
		pieces(sb(strconv.Itoa(len(idx)), RoleLineage), s(" of "+strconv.Itoa(len(Sentences))+" sentences  ·  eight verbs and a few small words", RoleMuted)),
	}
	if len(idx) == 0 {
		return fit(append(rows, text(""), pieces(sb("NO SENTENCE", RoleBoundary), s(" uses those words.", RoleDefault))), n)
	}
	sel := min(max(v.Selected, 0), len(idx)-1)
	visible := max(1, (n-len(rows))/3)
	start := 0
	if sel >= visible {
		start = sel - visible + 1
	}
	if start+visible > len(idx) {
		start = max(0, len(idx)-visible)
	}
	for i := start; i < min(len(idx), start+visible); i++ {
		sn := Sentences[idx[i]]
		pointer, role := "  ", RoleMuted
		if i == sel {
			pointer, role = "▸ ", sn.Role
		}
		head := pieces(sb(pointer+sn.Say, role))
		if sn.Records {
			head = head.with(s("  ● records", RoleBoundary))
		}
		rows = append(rows, head, pieces(s("  "+sn.Plain, RoleDefault)), pieces(s("  "+sn.Does, RoleMuted)))
	}
	return fit(rows, n)
}

func layersBody(st Snapshot, n, w int) []line {
	if st.Ledger == "" {
		return noLedger(n)
	}
	l := st.Layers
	star := pieces(sb("Star    ", RoleLineage), sb(l.Star.Anchor, RoleLineage),
		s("  ·  "+countOf(l.Star.System, "system event")+"  ·  "+countOf(l.Star.Keys, "key")+"  ·  "+countOf(l.Star.Seeds, "seed"), RoleMuted))
	rows := []line{strong("STAR, PLANETS AND MOONS", RolePlace), star}
	if len(l.Planets) == 0 {
		rows = append(rows, text("No Planet yet: nothing is written at an address of yours."))
	}
	for _, p := range l.Planets {
		rows = append(rows, pieces(sb("Planet  ", RolePlace), sb(p.Name, RolePlace), s("  ·  "+countOf(p.Events, "event"), RoleMuted)))
		for _, m := range p.Moons {
			rows = append(rows, pieces(s("  ", RoleDefault), sb("Moon  ", RoleSettled), sb(m.Name, RoleSettled), s("  ·  "+countOf(m.Events, "event"), RoleMuted)))
			for _, d := range m.Deeper {
				rows = append(rows, pieces(s("        "+d, RoleDefault)))
			}
		}
	}
	for i := range rows {
		rows[i] = rows[i].truncate(w)
	}
	return fit(rows, n)
}

func mainBody(st Snapshot, v View, n, w int) []line {
	if v.MenuOpen {
		return menuBody(v, n)
	}
	switch v.Screen {
	case ScreenSentence:
		return sentenceBody(st, n)
	case ScreenAuthority:
		return authorityBody(st, n)
	case ScreenLayers:
		return layersBody(st, n, w)
	}
	return ledgerBody(st, n, w)
}

// wrapText breaks a line of text into rows no wider than w, at spaces, so an
// answer is read whole and never cut in the middle of a sentence. A word
// wider than a row is broken where the row ends; nothing is dropped.
func wrapText(v string, w int) []string {
	v = inline(v)
	if w < 1 {
		w = 1
	}
	var out []string
	cur, curW := "", 0
	flush := func() { out = append(out, cur); cur, curW = "", 0 }
	for _, word := range strings.Split(v, " ") {
		ww := width(word)
		if curW > 0 && curW+1+ww > w {
			flush()
		}
		if curW > 0 {
			cur, curW = cur+" ", curW+1
		}
		for ww > w-curW && ww > 0 {
			// The word does not fit even on a row of its own: it is broken
			// where the row ends, and a rune wider than a whole row still
			// gets a row, so the loop always moves on.
			if curW > 0 {
				flush()
				continue
			}
			head, used := "", 0
			for i, r := range word {
				if used > 0 && used+cells(r) > w {
					break
				}
				head, used = word[:i+utf8.RuneLen(r)], used+cells(r)
			}
			cur, curW = head, used
			word = word[len(head):]
			ww = width(word)
			flush()
		}
		cur, curW = cur+word, curW+ww
	}
	if cur == "" && len(out) > 0 {
		return out
	}
	return append(out, cur)
}

// answerRows is what the shell last said, in the shell's own words, wrapped
// to a width. A refusal takes the boundary colour; an answer the settled one.
func answerRows(v View, w int) []line {
	role := replyRole(v)
	var rows []line
	for _, ln := range v.Reply {
		for _, r := range wrapText(ln, w) {
			rows = append(rows, line{{text: r, role: role}})
		}
	}
	return rows
}

// pager is what a layout found about the answer: how many rows it has at
// this width, which row the box begins with, and how many it shows.
type pager struct{ total, at, shown int }

func (p *pager) overflows() bool { return p != nil && p.shown < p.total }

// replyBody is the answer in a box of at most n rows. When the answer is
// longer, the box shows a page of it from where the person is reading, and
// its last row says which lines these are and which key reads on. Nothing
// is cut by a count without a way to the rest.
func replyBody(v View, w, n int, pg *pager) []line {
	rows := answerRows(v, w)
	pg.total, pg.at, pg.shown = len(rows), 0, len(rows)
	if len(rows) <= n || n < 1 {
		return rows
	}
	page := n - 1
	at := min(max(v.AnswerAt, 0), len(rows)-page)
	pg.at, pg.shown = at, page
	out := append([]line{}, rows[at:at+page]...)
	return append(out, pageMark(v, at, page, len(rows), w))
}

// pageMark says which lines of the answer are shown and how to see the rest.
func pageMark(v View, at, page, total, w int) line {
	where := "lines " + strconv.Itoa(at+1) + "–" + strconv.Itoa(at+page) + " of " + strconv.Itoa(total)
	tries := []string{"(" + where + " · ↓ or PgDn reads the rest · esc closes)", "(" + where + " · ↓ reads the rest)"}
	if v.Reading {
		tries = []string{"(" + where + " · ↑↓ PgUp PgDn page · esc back)", "(" + where + " · ↑↓ page)"}
	}
	tries = append(tries, "("+strconv.Itoa(at+1)+"–"+strconv.Itoa(at+page)+" of "+strconv.Itoa(total)+" lines ↓)")
	for _, t := range tries {
		if width(t) <= w {
			return tint(t, RoleMuted)
		}
	}
	return tint(tries[len(tries)-1], RoleMuted).truncate(w)
}

// promptLine is the line being typed, with the cursor where it stands. A
// line longer than the row scrolls: the cursor is always in sight, and when
// it is at the end, so is the end of the line. What was cut off to the left
// is marked with an ellipsis. Everything typed is drawn escaped, so a pasted
// line break shows as \n and moves nothing.
func promptLine(v View, w int) line {
	return promptFor(v, Snapshot{}, w)
}

// suggestion is the sentence a person is likely to say on the view they are
// looking at, and the meaning it belongs to: how to begin on a fresh ledger,
// "write" when a sentence waits, the grants on the authority view.
func suggestion(v View, st Snapshot) (string, Role) {
	switch {
	case st.Ledger == "":
		return "", RoleMuted
	case v.Screen == ScreenSentence && len(st.Drafts) > 0:
		return "write", RoleBoundary
	case v.Screen == ScreenSentence:
		return "write at home/journal: my first note", RoleWorking
	case v.Screen == ScreenAuthority:
		return "see the grants", RoleAuthority
	case st.Accepted == 0 && len(st.Drafts) == 0:
		return "write at home/journal: my first note", RoleWorking
	case v.Screen == ScreenLayers && len(st.Layers.Planets) > 0:
		return "read " + st.Layers.Planets[0].Name, RoleSettled
	case len(st.Drafts) > 0:
		return "write", RoleBoundary
	case len(st.Events) > 0 && !st.Events[0].System:
		return "read " + st.Events[0].Address, RoleSettled
	}
	return "see the ledger", RoleSettled
}

// promptFor is the prompt line for a view of a snapshot: the line being
// typed, or, while nothing is, the sentence likely on this view, coloured
// by its meaning, and how to see the rest.
func promptFor(v View, st Snapshot, w int) line {
	mark := sb("› ", RolePlace)
	cursor := sb("█", RoleWorking)
	if v.MenuOpen {
		return pieces(mark, s("/"+v.Query, RoleWorking), cursor).truncate(w)
	}
	if v.Input == "" {
		if say, role := suggestion(v, st); say != "" {
			for _, t := range []line{
				pieces(mark, cursor, s(" try ", RoleMuted), s(say, role), s("  ·  ? for the list", RoleMuted)),
				pieces(mark, cursor, s(" try ", RoleMuted), s(say, role)),
			} {
				if t.width() <= w {
					return t
				}
			}
		}
		return pieces(mark, cursor, s(" say a sentence, or ? for the list", RoleMuted)).truncate(w)
	}
	back := min(max(v.Back, 0), len(v.Input))
	left, right := inline(v.Input[:len(v.Input)-back]), inline(v.Input[len(v.Input)-back:])
	room := w - 2 - 1 // the mark, and the last cell kept free
	if room < 3 {
		return pieces(mark, cursor).truncate(w)
	}
	if width(left)+1+width(right) <= room {
		return pieces(mark, s(left, RoleDefault), cursor, s(right, RoleDefault))
	}
	// Keep the cursor in sight: what comes after it takes the room the line
	// before it leaves, or half the row when both are long; what comes
	// before it fills the rest. A side that was cut is marked.
	lw, rw := width(left), width(right)
	afterRoom := room - 1 - lw
	if afterRoom < rw {
		afterRoom = max(afterRoom, (room-1)/2)
	}
	out := pieces(mark)
	after, cutRight := right, false
	if rw > afterRoom {
		after, cutRight = takeCells(right, max(0, afterRoom-1)), true
	}
	beforeRoom := room - 1 - width(after)
	if cutRight {
		beforeRoom--
	}
	if lw > beforeRoom {
		out = out.with(s("…", RoleMuted), s(lastCells(left, max(0, beforeRoom-1)), RoleDefault))
	} else {
		out = out.with(s(left, RoleDefault))
	}
	out = out.with(cursor, s(after, RoleDefault))
	if cutRight {
		out = out.with(s("…", RoleMuted))
	}
	return out
}

// takeCells is the start of v that fits in n cells; lastCells is its end.
func takeCells(v string, n int) string {
	used := 0
	for i, r := range v {
		if used+cells(r) > n {
			return v[:i]
		}
		used += cells(r)
	}
	return v
}

func lastCells(v string, n int) string {
	used, start := 0, len(v)
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(v[:start])
		if used+cells(r) > n {
			break
		}
		used += cells(r)
		start -= size
	}
	return v[start:]
}

// hints is the last row: how to move, how to ask, how to read the rest of a
// long answer, and above all how to leave. When the row is narrow, the least
// needed hint goes first; how to leave goes last of all.
func hints(v View, w int, pg *pager) line {
	leave := s("\"go\" or Ctrl-C leaves", RoleMuted)
	var tries []line
	switch {
	case v.MenuOpen:
		tries = []line{
			pieces(s("  type to find", RoleMuted), s("   ↑↓ move", RoleLineage), s("   enter choose", RoleSettled), s("   esc closes the list", RoleMuted)),
			pieces(s("  ↑↓ move", RoleLineage), s("   enter choose", RoleSettled), s("   esc closes the list", RoleMuted)),
			pieces(s("  ↑↓", RoleLineage), s("  enter", RoleSettled), s("  esc closes", RoleMuted)),
		}
	case v.Reading:
		tries = []line{
			pieces(s("  ↑↓ PgUp PgDn page the answer", RoleLineage), s("   esc back", RoleMuted), s("   ", RoleMuted), leave),
			pieces(s("  ↑↓ page", RoleLineage), s("   esc back", RoleMuted), s("   ", RoleMuted), leave),
			pieces(s("  ↑↓ page", RoleLineage), s("  go or Ctrl-C leaves", RoleMuted)),
			pieces(s("  go or Ctrl-C leaves", RoleMuted)),
		}
	case pg.overflows():
		tries = []line{
			pieces(s("  ↓ or PgDn reads the whole answer", RoleLineage), s("   esc closes it", RoleMuted), s("   ", RoleMuted), leave),
			pieces(s("  ↓ reads on", RoleLineage), s("   esc closes", RoleMuted), s("   ", RoleMuted), leave),
			pieces(s("  ↓ reads on", RoleLineage), s("  go or Ctrl-C leaves", RoleMuted)),
			pieces(s("  go or Ctrl-C leaves", RoleMuted)),
		}
	default:
		tries = []line{
			pieces(s("  tab or ←→ view", RoleMuted), s("   ? sentences", RolePlace), s("   1–4 jump", RoleMuted), s("   ", RoleMuted), leave),
			pieces(s("  tab or ←→ view", RoleMuted), s("   ? sentences", RolePlace), s("   ", RoleMuted), leave),
			pieces(s("  ? sentences", RolePlace), s("   ", RoleMuted), leave),
			pieces(s("  ? list", RolePlace), s("  go or Ctrl-C leaves", RoleMuted)),
			pieces(s("  go or Ctrl-C leaves", RoleMuted)),
		}
	}
	for _, t := range tries {
		if t.width() <= w {
			return t
		}
	}
	return tries[len(tries)-1].truncate(w)
}

func mainTitle(v View) string {
	if v.MenuOpen {
		return "SENTENCES · " + strings.ToUpper(countOf(len(Sentences), "sentence"))
	}
	return strings.ToUpper(ScreenNames[min(max(v.Screen, 0), len(ScreenNames)-1)])
}

// ---------- the layouts ----------

func replyRole(v View) Role {
	if v.Failed {
		return RoleBoundary
	}
	return RoleSettled
}

// share splits the rows a layout has between the view and the answer. The
// answer takes what it needs, but never so much that the view keeps fewer
// than least rows: the newest event stays in sight while an answer shows.
// While the person reads a long answer it takes the view's place, frame and
// all, and body is zero. viewChrome is the view's frame, chrome the
// answer's.
func share(v View, w, avail, least, viewChrome, chrome int, pg *pager) (body int, reply []line) {
	if len(v.Reply) == 0 {
		return max(1, avail-viewChrome), nil
	}
	if v.Reading {
		return 0, replyBody(v, w, max(1, avail-chrome), pg)
	}
	reply = replyBody(v, w, max(1, avail-viewChrome-least-chrome), pg)
	return max(1, avail-viewChrome-len(reply)-chrome), reply
}

// wide is two panes side by side: the view and where the person stands.
func wide(st Snapshot, v View, w, rows int, pg *pager) []line {
	side := 31
	left := w - side - 1
	gap := rows >= 30
	fixed := 5 + 3 + 1 // the header, the prompt, the hints
	chrome := 2        // the answer's frame
	if gap {
		fixed += 2
		chrome++
	}
	body, reply := share(v, w-4, rows-fixed, 8, 2, chrome, pg)
	out := header(st, v, w)
	if gap {
		out = append(out, text(""))
	}
	if body > 0 {
		out = append(out, beside(frame(mainTitle(v), RolePlace, mainBody(st, v, body, left-4), left), left,
			frame("STANDING", RoleLineage, standingBody(st, body), side), side, 1)...)
		if gap {
			out = append(out, text(""))
		}
	}
	if len(reply) > 0 {
		out = append(out, frame("ROKH SAYS", replyRole(v), reply, w)...)
		if gap {
			out = append(out, text(""))
		}
	}
	out = append(out, frame("SAY ONE SENTENCE", RolePlace, []line{promptFor(v, st, w-4)}, w)...)
	return append(out, hints(v, w, pg))
}

// medium is one framed pane, the view, above the answer and the prompt.
func medium(st Snapshot, v View, w, rows int, pg *pager) []line {
	gap := rows >= 30
	fixed := 5 + 3 + 1
	chrome := 2
	if gap {
		fixed += 2
		chrome++
	}
	body, reply := share(v, w-4, rows-fixed, 6, 2, chrome, pg)
	out := header(st, v, w)
	if gap {
		out = append(out, text(""))
	}
	if body > 0 {
		out = append(out, frame(mainTitle(v), RolePlace, mainBody(st, v, body, w-4), w)...)
		if gap {
			out = append(out, text(""))
		}
	}
	if len(reply) > 0 {
		out = append(out, frame("ROKH SAYS", replyRole(v), reply, w)...)
		if gap {
			out = append(out, text(""))
		}
	}
	out = append(out, frame("SAY ONE SENTENCE", RolePlace, []line{promptFor(v, st, w-4)}, w)...)
	return append(out, hints(v, w, pg))
}

// narrow is a rail: the view down the left edge, the answer and the prompt
// under it, no frames.
func narrow(st Snapshot, v View, w, rows int, pg *pager) []line {
	room := text("")
	if st.Room != "" && st.Ledger != "" {
		room = pieces(s("room ", RoleMuted)).with(roomNote(st)...).truncate(w)
	}
	out := []line{identity(st, w), status(st, w), tabs(v, w), room}
	fixed := len(out) + 1 + 1 // the prompt and the hints
	body, reply := share(v, w, rows-fixed, 4, 0, 1, pg)
	if body > 0 {
		rows2 := mainBody(st, v, body, w-2)
		if !v.MenuOpen && v.Screen == ScreenSentence {
			rows2 = narrowSentenceBody(st, body)
		}
		for _, r := range rows2 {
			out = append(out, pieces(s("│ ", RoleMuted)).with(r.truncate(max(0, w-2))...))
		}
		if len(reply) > 0 {
			out = append(out, text(""))
		}
	}
	out = append(out, reply...)
	return append(out, promptFor(v, st, w), hints(v, w, pg))
}

// compact is for a window that is wide enough and short: one row for who
// and where, one for the tally, the view one row per item, the answer, the
// prompt and the hints.
func compact(st Snapshot, v View, w, rows int, pg *pager) []line {
	out := []line{identity(st, w), status(st, w)}
	body, reply := share(v, w, rows-len(out)-2, 1, 0, 0, pg)
	if body > 0 {
		out = append(out, compactBody(st, v, w, body)...)
	}
	out = append(out, reply...)
	return append(out, promptFor(v, st, w), hints(v, w, pg))
}

// compactBody is the view in one row per thing.
func compactBody(st Snapshot, v View, w, n int) []line {
	var rows []line
	switch {
	case v.MenuOpen:
		for _, i := range matching(v.Query) {
			sn := Sentences[i]
			rows = append(rows, pieces(sb(sn.Say, sn.Role), s("  "+sn.Plain, RoleMuted)))
		}
		if sel := min(max(v.Selected, 0), max(0, len(rows)-1)); sel < len(rows) {
			rows[sel] = pieces(sb("▸ ", RolePlace)).with(rows[sel]...)
			if sel >= n {
				rows = rows[sel-n+1:]
			}
		}
	case st.Ledger == "":
		rows = []line{pieces(s("no ledger is open; say ", RoleDefault), sb("open the ledger {name}", RolePlace))}
	case v.Screen == ScreenSentence:
		for i := len(st.Drafts) - 1; i >= 0; i-- {
			d := st.Drafts[i]
			rows = append(rows, pieces(sb(digits(d.Index)+" waiting ", RoleWorking), s("“"+d.Sentence+"”", RoleDefault)))
		}
		rows = append(rows, pieces(strong("NOT RECORDED", RoleWorking)...).with(s("  ", RoleDefault), sb("write", RoleBoundary), s(" records, no undo", RoleDefault)))
	case v.Screen == ScreenLayers:
		rows = append(rows, pieces(sb("Star  ", RoleLineage), sb(st.Layers.Star.Anchor, RoleLineage),
			s("  ·  "+countOf(st.Layers.Star.System, "system event"), RoleMuted)))
		for _, p := range st.Layers.Planets {
			moons := make([]string, 0, len(p.Moons))
			for _, m := range p.Moons {
				moons = append(moons, m.Name)
			}
			row := pieces(sb("Planet  ", RolePlace), sb(p.Name, RolePlace), s("  ·  "+countOf(p.Events, "event"), RoleMuted))
			if len(moons) > 0 {
				row = row.with(s("  ·  Moons "+strings.Join(moons, ", "), RoleSettled))
			}
			rows = append(rows, row)
		}
	case v.Screen == ScreenAuthority:
		for _, it := range st.Writing {
			rows = append(rows, pieces(sb("● "+it.Subject, RoleAuthority), s("  "+it.Detail+" at ", RoleDefault), sb(it.Scope, RolePlace)))
		}
		for _, it := range st.Disclosure {
			rows = append(rows, pieces(sb("● "+it.Subject, RoleAuthority), s("  "+it.Detail+" at ", RoleDefault), sb(it.Scope, RolePlace), s("  nothing has been sent", RoleWorking)))
		}
		if len(rows) == 0 {
			rows = append(rows, text("Nothing is entrusted and nothing is opened to anyone."))
		}
	default:
		for _, e := range st.Events {
			if e.System {
				rows = append(rows, pieces(sb("● ", verdictRole(e.Verdict)), sb(e.ID, RoleLineage), s("  system "+e.Verb+" at ", RoleDefault),
					sb(e.Address, RolePlace), s("  "+e.Payload, RoleMuted)))
				continue
			}
			rows = append(rows, pieces(sb("● ", verdictRole(e.Verdict)), sb(e.ID, RoleLineage), s("  "+e.Verb+" at ", RoleDefault),
				sb(e.Address, RolePlace), s("  “"+e.Payload+"”", RoleDefault)))
		}
	}
	for i := range rows {
		rows[i] = rows[i].truncate(w)
	}
	return fit(rows, n)
}

// tiny is four rows at the least: who, the tally or the answer, the prompt
// and the hints; a taller window gives the answer the rows between.
func tiny(st Snapshot, v View, w, rows int, pg *pager) []line {
	if v.MenuOpen {
		idx := matching(v.Query)
		out := []line{pieces(sb("ROKH", RolePlace), s(" · "+strconv.Itoa(len(idx))+" sentences", RoleMuted)).truncate(w)}
		if len(idx) == 0 {
			out = append(out, pieces(sb("NO SENTENCE", RoleBoundary)).truncate(w), pieces(s("find: "+v.Query, RoleWorking)).truncate(w))
		} else {
			sn := Sentences[idx[min(max(v.Selected, 0), len(idx)-1)]]
			out = append(out, pieces(sb("▸ "+sn.Say, sn.Role)).truncate(w), pieces(s(sn.Plain, RoleMuted)).truncate(w))
		}
		return lastRows(pad(out, rows-1), hints(v, w, pg), rows)
	}
	first := pieces(sb("ROKH", RolePlace))
	if st.Ledger != "" {
		first = first.with(s(" · "+st.Ledger, RolePlace), s(" · custody "+st.Custody, RoleAuthority))
	} else {
		first = first.with(s(" · no ledger open", RoleWorking))
	}
	middle := []line{tally(st).truncate(w)}
	if len(v.Reply) > 0 {
		middle = replyBody(v, w, max(1, rows-3), pg)
	}
	out := append([]line{first.truncate(w)}, middle...)
	out = pad(out, rows-2)
	return lastRows(append(out, promptFor(v, st, w)), hints(v, w, pg), rows)
}

// pad fills rows up to n with empty ones, so what follows lands at the
// bottom of the window.
func pad(rows []line, n int) []line {
	for len(rows) < n {
		rows = append(rows, text(""))
	}
	return rows
}

// lastRows puts the hints on the last row and keeps as many rows above it
// as there is room for, from the top.
func lastRows(rows []line, hint line, n int) []line {
	if n < 1 {
		return nil
	}
	if len(rows) > n-1 {
		rows = rows[:n-1]
	}
	return append(rows, hint)
}

// Lines lays a snapshot out for a terminal: exactly as many rows as the
// terminal has, the hints on the last. The result never reaches the last
// column, so nothing wraps and the prompt stays whole; and Frame never ends
// the last row with a line break, so nothing scrolls.
func Lines(o Options, v View, st Snapshot) []line {
	out, _ := layout(o, v, st)
	return out
}

// settle keeps the reading place inside the answer as this window shows it,
// and notes whether the answer is longer than its box and how long a page
// is, for the keys that page through it. It is asked after every key and
// every change of size.
func settle(o Options, v View, st Snapshot) View {
	_, pg := layout(o, v, st)
	v.AnswerAt, v.Overflow, v.Page = pg.at, pg.overflows(), max(1, pg.shown)
	return v
}

func layout(o Options, v View, st Snapshot) ([]line, pager) {
	cols, rows := o.Columns, o.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 26
	}
	w := max(1, min(cols-1, 116))
	v.Screen = min(max(v.Screen, 0), len(ScreenNames)-1)
	pg := &pager{}
	var out []line
	switch {
	case cols >= 96 && rows >= 27:
		out = wide(st, v, w, rows, pg)
	case cols >= 58 && rows >= 22:
		out = medium(st, v, w, rows, pg)
	case cols >= 34 && rows >= 15:
		out = narrow(st, v, w, rows, pg)
	case cols >= 34 && rows >= 6:
		out = compact(st, v, w, rows, pg)
	default:
		out = tiny(st, v, w, rows, pg)
	}
	if len(out) != rows && len(out) > 0 {
		// Every layout counts its rows to fill the window; this only keeps
		// a miscount from scrolling the screen or losing the hints.
		hint := out[len(out)-1]
		out = lastRows(pad(out[:len(out)-1], rows-1), hint, rows)
	}
	for i := range out {
		out[i] = out[i].truncate(w)
	}
	return out, *pg
}

func paint(p span, o Options) string {
	if !o.Color {
		return p.text
	}
	var codes []string
	if p.bold {
		codes = append(codes, "1")
	}
	if p.role == RoleMuted {
		codes = append(codes, "2")
	} else if c := colourCode(p.role, o.Depth); c != "" {
		codes = append(codes, c)
	}
	if len(codes) == 0 {
		return p.text
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + p.text + "\x1b[0m"
}

// Render draws one whole screen as text, its rows separated by newlines. It
// is the screen as a file would hold it — the golden screens are this — and
// not what is sent to a terminal; Frame is that.
func Render(o Options, v View, st Snapshot) string {
	var b strings.Builder
	for i, row := range Lines(o, v, st) {
		if i > 0 {
			b.WriteByte('\n')
		}
		for _, p := range row {
			b.WriteString(paint(p, o))
		}
	}
	return b.String()
}

// Frame is one whole screen as the bytes a raw terminal is sent.
//
// A terminal in raw mode does no output processing: a bare newline moves the
// cursor down and leaves it in the same column, so a screen sent with
// newlines alone comes out as a staircase, each row starting where the one
// above it ended. Every row here but the last ends with a carriage return
// before its newline, and the frame begins by homing the cursor rather than
// clearing the screen — each row erases what was left of the previous frame
// beyond its own end, and the last erases everything below it. The last row
// is the terminal's own last row and is not followed by a line break, which
// would scroll the whole screen up by one. Redrawing this way leaves nothing
// behind and does not blank the screen between frames.
func Frame(o Options, v View, st Snapshot) string {
	var b strings.Builder
	b.WriteString("\x1b[H")
	for i, row := range Lines(o, v, st) {
		if i > 0 {
			b.WriteString("\r\n")
		}
		for _, p := range row {
			b.WriteString(paint(p, o))
		}
		b.WriteString("\x1b[K")
	}
	b.WriteString("\x1b[J")
	return b.String()
}

func digits(n int) string { return strconv.Itoa(n) }
