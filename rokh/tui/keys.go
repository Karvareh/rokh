package tui

import (
	"strconv"
	"unicode/utf8"
)

// Key is one thing a person did at the keyboard, read out of the bytes the
// terminal sent. Text carries the rune for KindText and nothing otherwise.
type Key struct {
	Kind KeyKind
	Text string
}

type KeyKind int

const (
	KindNone KeyKind = iota // bytes consumed, nothing meant
	KindText
	KindEnter
	KindBackspace
	KindEscape
	KindTab
	KindUp
	KindDown
	KindLeft
	KindRight
	KindQuit       // ctrl-c
	KindEOF        // ctrl-d: leaves when nothing is typed
	KindHome       // home, ctrl-a
	KindEnd        // end, ctrl-e
	KindDelete     // delete: the character under the cursor
	KindPageUp     // page up
	KindPageDown   // page down
	KindKillStart  // ctrl-u: everything before the cursor
	KindKillEnd    // ctrl-k: everything from the cursor on
	KindKillWord   // ctrl-w: the word before the cursor
	KindPasteStart // the terminal says a paste begins
	KindPasteEnd   // and that it ended
)

// decode reads one key from the front of buf and says how many bytes it
// used. Zero means the bytes so far are the beginning of something longer —
// an escape sequence, or a multi-byte rune — and the caller should read more
// before asking again.
//
// An escape has two lives: alone, it is the Escape key; followed by "[" or
// "O" it begins a sequence a key sends. The terminal sends both the same way
// and only time tells them apart, so the caller says whether enough time has
// passed for a lone escape to be one.
//
// A rune is taken whole or not at all. What a person types into a sentence
// is content, in whatever script they write, and a byte of it must never be
// shown or judged before the rest of it has arrived.
func decode(buf []byte, escapeExpired bool) (Key, int) {
	if len(buf) == 0 {
		return Key{}, 0
	}
	b := buf[0]
	if b == 0x1b {
		if len(buf) >= 2 && buf[1] == '[' {
			// CSI: parameters, then one final byte in 0x40..0x7e.
			for i := 2; i < len(buf); i++ {
				if buf[i] >= 0x40 && buf[i] <= 0x7e {
					return Key{Kind: csiKey(string(buf[2:i]), buf[i])}, i + 1
				}
			}
			if escapeExpired {
				return Key{Kind: KindEscape}, 1
			}
			return Key{}, 0
		}
		if len(buf) >= 2 && buf[1] == 'O' {
			// SS3: what the arrows, Home and End send in the terminal's
			// application mode.
			if len(buf) >= 3 {
				return Key{Kind: csiKey("", buf[2])}, 3
			}
			if escapeExpired {
				return Key{Kind: KindEscape}, 1
			}
			return Key{}, 0
		}
		if len(buf) >= 2 || escapeExpired {
			return Key{Kind: KindEscape}, 1
		}
		return Key{}, 0
	}
	switch b {
	case 0x01:
		return Key{Kind: KindHome}, 1
	case 0x03:
		return Key{Kind: KindQuit}, 1
	case 0x04:
		return Key{Kind: KindEOF}, 1
	case 0x05:
		return Key{Kind: KindEnd}, 1
	case 0x0b:
		return Key{Kind: KindKillEnd}, 1
	case 0x15:
		return Key{Kind: KindKillStart}, 1
	case 0x17:
		return Key{Kind: KindKillWord}, 1
	case '\r':
		// A carriage return and the line feed after it are one line ending,
		// as a pasted text from another system sends it.
		if len(buf) >= 2 && buf[1] == '\n' {
			return Key{Kind: KindEnter}, 2
		}
		return Key{Kind: KindEnter}, 1
	case '\n':
		return Key{Kind: KindEnter}, 1
	case 0x08, 0x7f:
		return Key{Kind: KindBackspace}, 1
	case '\t':
		return Key{Kind: KindTab}, 1
	}
	if b < 0x20 {
		return Key{Kind: KindNone}, 1 // other control bytes mean nothing here
	}
	if b < 0x80 {
		return Key{Kind: KindText, Text: string(rune(b))}, 1
	}
	if !utf8.FullRune(buf) {
		if escapeExpired {
			return Key{Kind: KindNone}, 1 // a fragment nothing completed
		}
		return Key{}, 0
	}
	r, n := utf8.DecodeRune(buf)
	if r == utf8.RuneError && n == 1 {
		return Key{Kind: KindNone}, 1 // not UTF-8; dropped, never drawn
	}
	return Key{Kind: KindText, Text: string(r)}, n
}

// csiKey names the key a sequence stands for. The modifiers a terminal adds
// after a semicolon (ctrl-right is "1;5C") do not change which key it is.
func csiKey(params string, final byte) KeyKind {
	switch final {
	case 'A':
		return KindUp
	case 'B':
		return KindDown
	case 'C':
		return KindRight
	case 'D':
		return KindLeft
	case 'H':
		return KindHome
	case 'F':
		return KindEnd
	case '~':
		first := params
		for i := 0; i < len(params); i++ {
			if params[i] == ';' {
				first = params[:i]
				break
			}
		}
		switch n, _ := strconv.Atoi(first); n {
		case 1, 7:
			return KindHome
		case 4, 8:
			return KindEnd
		case 3:
			return KindDelete
		case 5:
			return KindPageUp
		case 6:
			return KindPageDown
		case 200:
			return KindPasteStart
		case 201:
			return KindPasteEnd
		}
	}
	return KindNone // a sequence this surface has no use for
}

// trimRune removes the last rune of a string, not the last byte: a person
// pressing backspace after a Persian letter expects the letter to go.
func trimRune(v string) string {
	if v == "" {
		return v
	}
	_, n := utf8.DecodeLastRuneInString(v)
	return v[:len(v)-n]
}
