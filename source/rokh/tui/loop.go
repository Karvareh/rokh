package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"
	"unicode/utf8"
)

// Hooks are the two things this surface asks of the shell, and the only two.
//
// Say hands over the line the person typed, exactly as typed, and gets back
// what the shell answered, whether the session is now over, and whether the
// shell refused. Snapshot asks for the state to draw. Neither is called from
// anywhere but a person's key press.
type Hooks struct {
	Say      func(sentence string) (reply string, done bool, err error)
	Snapshot func() Snapshot
	// Signals are the signals that end the program, as the command names
	// them for its system. On any of them the screen puts the terminal back
	// as it found it and returns Signalled; a sentence being said finishes
	// first. This package names no signal itself.
	Signals []os.Signal
}

// NoScreen is why the screen was not started: this terminal cannot be drawn
// on. The terminal was not touched; the caller offers the line surface and
// says why.
type NoScreen struct{ Why string }

func (n NoScreen) Error() string { return "no screen here (" + n.Why + ")" }

// Signalled is a screen ended by a signal, after it put the terminal back.
type Signalled struct{ Signal os.Signal }

func (s Signalled) Error() string { return "stopped by a signal (" + s.Signal.String() + ")" }

// step is the whole of the surface's behaviour, as a function: a view and a
// key go in, a view comes out, and two flags say whether the session ended
// and whether the ledger may have changed. It touches no terminal, so it can
// be tested without one, and Run is the thin loop around it.
func step(v View, k Key, h Hooks) (out View, quit, changed bool) {
	v.Back = min(max(v.Back, 0), len(v.Input))
	switch k.Kind {
	case KindQuit:
		return v, true, false
	case KindNone:
		return v, false, false
	case KindPasteStart:
		v.Pasting = true
		return v, false, false
	case KindPasteEnd:
		v.Pasting = false
		return v, false, false
	}
	// Inside a paste every key is text. A line break is kept as a line
	// break in the sentence and drawn as \n; it never presses Enter, so a
	// pasted paragraph cannot say, let alone record, anything by itself.
	if v.Pasting {
		switch k.Kind {
		case KindText:
			v = insert(v, k.Text)
		case KindEnter:
			v = insert(v, "\n")
		case KindTab:
			v = insert(v, "\t")
		}
		return v, false, false
	}
	if k.Kind == KindEOF {
		// Ctrl-D on an empty prompt is the end of input, as it is on every
		// line a terminal reads: it leaves. With something typed it does
		// nothing, so a stray key never costs a person their sentence.
		return v, v.Input == "" && !v.MenuOpen, false
	}

	if v.MenuOpen {
		switch k.Kind {
		case KindEscape:
			v.MenuOpen, v.Query, v.Selected = false, "", 0
		case KindUp, KindDown:
			if n := len(matching(v.Query)); n > 0 {
				if k.Kind == KindUp {
					v.Selected = (v.Selected + n - 1) % n
				} else {
					v.Selected = (v.Selected + 1) % n
				}
			}
		case KindBackspace:
			v.Query, v.Selected = trimRune(v.Query), 0
		case KindText:
			v.Query, v.Selected = v.Query+k.Text, 0
		case KindEnter:
			if idx := matching(v.Query); len(idx) > 0 {
				sn := Sentences[idx[min(max(v.Selected, 0), len(idx)-1)]]
				v.Screen = sn.Screen
				v.Input = Stem(sn.Say)
			}
			v.MenuOpen, v.Query, v.Selected = false, "", 0
		}
		return v, false, false
	}

	switch k.Kind {
	case KindText:
		// The list opens on "/" or "?" only when nothing is being typed:
		// "read home/journal" has a slash in it and means what it says.
		if v.Input == "" {
			switch k.Text {
			case "/", "?":
				v.MenuOpen, v.Query, v.Selected = true, "", 0
				return v, false, false
			case "1", "2", "3", "4":
				v.Screen = int(k.Text[0] - '1')
				return v, false, false
			}
		}
		v = insert(v, k.Text)
	case KindBackspace:
		at := len(v.Input) - v.Back
		before := trimRune(v.Input[:at])
		v.Input = before + v.Input[at:]
	case KindDelete:
		at := len(v.Input) - v.Back
		if v.Back > 0 {
			_, n := utf8.DecodeRuneInString(v.Input[at:])
			v.Input, v.Back = v.Input[:at]+v.Input[at+n:], v.Back-n
		}
	case KindEscape:
		// Esc steps back one thing at a time: out of a long answer being
		// read, then the answer and whatever was typed.
		if v.Reading {
			v.Reading, v.AnswerAt = false, 0
			break
		}
		v.Input, v.Back, v.Reply, v.Failed, v.Recalled, v.Typed = "", 0, nil, false, 0, ""
	case KindTab:
		if v.Input == "" {
			v.Screen = (v.Screen + 1) % len(ScreenNames)
		}
	case KindRight:
		// With nothing typed the arrows move between the views; once a
		// sentence has begun they move through it.
		if v.Input == "" {
			v.Screen = (v.Screen + 1) % len(ScreenNames)
		} else if v.Back > 0 {
			_, n := utf8.DecodeRuneInString(v.Input[len(v.Input)-v.Back:])
			v.Back -= n
		}
	case KindLeft:
		if v.Input == "" {
			v.Screen = (v.Screen + len(ScreenNames) - 1) % len(ScreenNames)
		} else if at := len(v.Input) - v.Back; at > 0 {
			_, n := utf8.DecodeLastRuneInString(v.Input[:at])
			v.Back += n
		}
	case KindHome:
		v.Back = len(v.Input)
	case KindEnd:
		v.Back = 0
	case KindKillStart:
		v.Input = v.Input[len(v.Input)-v.Back:]
		v.Back = len(v.Input)
	case KindKillEnd:
		v.Input, v.Back = v.Input[:len(v.Input)-v.Back], 0
	case KindKillWord:
		at := len(v.Input) - v.Back
		cut := strings.TrimRight(v.Input[:at], " ")
		if i := strings.LastIndexByte(cut, ' '); i >= 0 {
			cut = cut[:i+1]
		} else {
			cut = ""
		}
		v.Input = cut + v.Input[at:]
	case KindUp:
		// A long answer is paged; the first press opens it at full height.
		if v.readingKeys() {
			if v.Reading {
				v.AnswerAt = max(0, v.AnswerAt-1)
			}
			v.Reading = true
			break
		}
		// The sentences of this session, newest first; what was being typed
		// is kept and comes back at the end of the walk.
		if v.Recalled < len(v.History) {
			if v.Recalled == 0 {
				v.Typed = v.Input
			}
			v.Recalled++
			v.Input, v.Back = v.History[len(v.History)-v.Recalled], 0
		}
	case KindDown:
		if v.readingKeys() {
			if v.Reading {
				v.AnswerAt++
			}
			v.Reading = true
			break
		}
		if v.Recalled > 0 {
			v.Recalled--
			if v.Recalled == 0 {
				v.Input, v.Typed = v.Typed, ""
			} else {
				v.Input = v.History[len(v.History)-v.Recalled]
			}
			v.Back = 0
		}
	case KindPageUp, KindPageDown:
		if len(v.Reply) > 0 && !v.MenuOpen {
			if v.Reading {
				step := max(1, v.Page-1)
				if k.Kind == KindPageUp {
					step = -step
				}
				v.AnswerAt = max(0, v.AnswerAt+step)
			}
			v.Reading = true
		}
	case KindEnter:
		line := strings.TrimSpace(v.Input)
		if line == "" {
			return v, false, false
		}
		v.Recalled, v.Typed, v.Back = 0, "", 0
		v.Reading, v.AnswerAt, v.Overflow = false, 0, false
		if n := len(v.History); n == 0 || v.History[n-1] != line {
			v.History = append(v.History, line)
		}
		// Asking for the sentences opens the list, as it prints the list on
		// the line surface. It is not a sentence and the shell never hears it.
		if Asking(line) {
			v.Input, v.MenuOpen, v.Query, v.Selected = "", true, "", 0
			return v, false, false
		}
		reply, done, err := h.Say(line)
		v.Input = ""
		if err != nil {
			v.Reply, v.Failed = []string{"no — " + err.Error()}, true
		} else {
			v.Reply, v.Failed = splitReply(reply), false
		}
		return v, done, true
	}
	return v, false, false
}

// insert puts text where the cursor stands and leaves the cursor after it.
func insert(v View, text string) View {
	at := len(v.Input) - v.Back
	v.Input = v.Input[:at] + text + v.Input[at:]
	return v
}

func splitReply(reply string) []string {
	reply = strings.TrimRight(reply, "\n")
	if reply == "" {
		return nil
	}
	return strings.Split(reply, "\n")
}

// Run draws the surface on a terminal and runs it until the person leaves.
//
// It needs a terminal on both ends: a person is at the other side of this, or
// nobody is, and a pipe is nobody. The alternate screen is used so that what
// was on the terminal before is there again after, and the cursor is hidden
// because the prompt draws its own.
func Run(in, out *os.File, h Hooks) error {
	if !isTerminal(in) || !isTerminal(out) {
		return NoScreen{"standard input and output are not both a terminal"}
	}
	switch os.Getenv("TERM") {
	case "":
		return NoScreen{"TERM is not set"}
	case "dumb":
		return NoScreen{"TERM is dumb"}
	}
	restore, err := raw(in)
	if err != nil {
		return NoScreen{err.Error()}
	}
	defer restore()
	var stop chan os.Signal
	if len(h.Signals) > 0 {
		stop = make(chan os.Signal, 1)
		signal.Notify(stop, h.Signals...)
		defer signal.Stop(stop)
	}
	// The alternate screen, the cursor hidden (the prompt draws its own), and
	// bracketed paste, so the terminal marks what was pasted rather than
	// typed. Each is undone on the way out.
	fmt.Fprint(out, "\x1b[?1049h\x1b[?25l\x1b[?2004h\x1b[H\x1b[2J")
	defer fmt.Fprint(out, "\x1b[?2004l\x1b[?25h\x1b[?1049l\x1b[0m")

	v := View{}
	st := h.Snapshot()
	opts := detect(in, out)
	v = settle(opts, v, st)
	dirty := true
	measured := time.Now()

	var pending []byte
	var pendingAt time.Time
	buf := make([]byte, 64)

	for {
		// A signal to end is answered between keys, never in the middle of
		// a sentence being said; the deferred calls above put the terminal
		// back before the program ends.
		select {
		case sig := <-stop:
			return Signalled{sig}
		default:
		}
		// The size is asked for after every key and once a second while idle:
		// often enough that a resized window is caught, rarely enough that
		// asking is not the main thing this loop does.
		if time.Since(measured) > time.Second {
			if o := detect(in, out); o.Columns != opts.Columns || o.Rows != opts.Rows {
				opts, dirty = o, true
				v = settle(opts, v, st)
			}
			measured = time.Now()
		}
		if dirty {
			fmt.Fprint(out, Frame(opts, v, st))
			dirty = false
		}

		// The terminal is set to hand back whatever has arrived after a tenth
		// of a second, and when nothing has, that is a read of zero bytes —
		// which os.File reports as io.EOF. So EOF here means "quiet", not
		// "gone", and the loop goes round again. A terminal that has really
		// gone fails the read with an error that is not EOF, and that ends it.
		n, rerr := in.Read(buf)
		if rerr != nil && !errors.Is(rerr, io.EOF) {
			return rerr
		}
		if n > 0 {
			if len(pending) == 0 {
				pendingAt = time.Now()
			}
			pending = append(pending, buf[:n]...)
		}
		if len(pending) == 0 {
			continue
		}
		for len(pending) > 0 {
			k, used := decode(pending, time.Since(pendingAt) > 120*time.Millisecond)
			if used == 0 {
				break // the rest of this key has not arrived yet
			}
			pending = pending[used:]
			var quit, changed bool
			v, quit, changed = step(v, k, h)
			if quit {
				return nil
			}
			if changed {
				st = h.Snapshot()
			}
			v = settle(opts, v, st)
			dirty = true
		}
		if dirty {
			opts, measured = detect(in, out), time.Now()
		}
	}
}
