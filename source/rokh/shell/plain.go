package shell

import (
	"errors"
	"io/fs"
	"regexp"
	"strings"

	"rokh/carrier"
	"rokh/key"
	"rokh/lineage"
	"rokh/turn"
	"rokh/vessel"
)

// One place that says the plain thing.
//
// What comes up from below the surface is the core's own words: the vessel,
// the key layer, the writer's turn. They are exact, and they are written for
// programs. A person is told what happened, whether anything was recorded and
// what to do next, in one or two lines, and the stable code follows in
// brackets for a script that reads it. The core's words are never edited to
// suit a person; they are translated here, at the surface, and nowhere else.

// plainErr is a sentence for a person that still answers errors.Is for the
// error it stands for, so the code beneath it is never lost.
type plainErr struct {
	text  string
	cause error
}

func (p plainErr) Error() string { return p.text }
func (p plainErr) Unwrap() error { return p.cause }

// plainWords is what an error from below means to a person.
type plainWords struct {
	code   string // the stable code, when the error has one
	record string // "not recorded" when the error itself proves nothing was
	text   string
}

// Plain turns an error into what a person reads: a refusal with a plain
// sentence and its code in brackets. The command line may print its errors
// through it too, so that both surfaces say one thing.
func Plain(err error) error { return plainIn(err, nil) }

// plain is Plain for a session: the ledger it stands at is where a full Rokh
// is grown, so the way out names that folder and a size that would hold more.
func (s *session) plain(err error) error {
	if s == nil || s.current == nil {
		return plainIn(err, nil)
	}
	return plainIn(err, s.current)
}

func plainIn(err error, l *openLedger) error {
	if err == nil {
		return nil
	}
	if r, ok := err.(refusal); ok {
		var said plainErr
		if errors.As(r.err, &said) {
			return r // said plainly where it happened, with what only that place knew
		}
		w, known := wordsFor(r.err, l)
		if !known {
			return refusal{code: r.code, record: r.record, err: plainErr{text: tidy(r.err.Error()), cause: r.err}}
		}
		code, record := r.code, r.record
		// A failure the vessel named more exactly than the surface did keeps
		// the vessel's code: a full Rokh is vessel_full, not a failed store.
		if w.code != "" && (code == "" || code == "storage_failed") {
			code = w.code
		}
		if record == "" {
			record = w.record
		}
		return refusal{code: code, record: record, err: plainErr{text: w.text, cause: r.err}}
	}
	var said plainErr
	if errors.As(err, &said) {
		return refusal{code: "refused", err: err}
	}
	if w, known := wordsFor(err, l); known {
		return refusal{code: w.code, record: w.record, err: plainErr{text: w.text, cause: err}}
	}
	// Every refusal carries a code a script can branch on; one whose cause
	// has no stable code of its own carries refused.
	return refusal{code: "refused", err: plainErr{text: tidy(err.Error()), cause: err}}
}

// wordsFor finds what a person should be told for an error, by what it is and
// not by how it is spelled. The most particular answer comes first.
func wordsFor(err error, l *openLedger) (plainWords, bool) {
	switch {
	case errors.Is(err, vessel.ErrLocked), errors.Is(err, key.ErrPassphrase):
		return plainWords{"passphrase_refused", "", "that passphrase does not open this Rokh. Nothing was changed."}, true
	case errors.Is(err, vessel.ErrFull):
		return plainWords{"vessel_full", "not recorded", "this Rokh is full, so nothing was recorded. " + growAdvice(l)}, true
	case errors.Is(err, vessel.ErrCorrupt):
		return plainWords{"vessel_corrupt", "not recorded", "a part of this Rokh did not verify, so nothing was recorded. " +
			"Copy the folder as it is before anything else, then run: rokh verify " + folderOf(l)}, true
	case errors.Is(err, vessel.ErrTurnLost):
		return plainWords{"turn_lost", "not recorded", "another program took this Rokh while this was being recorded, so nothing was recorded. Say it again."}, true
	case errors.Is(err, vessel.ErrTurnUnavailable), errors.Is(err, turn.ErrUnavailable):
		return plainWords{"turn_unavailable", "not recorded", "this Rokh can only be read here: this system gives no way to hold it for writing. Nothing was recorded."}, true
	case errors.Is(err, turn.ErrBusy):
		return plainWords{"turn_busy", "not recorded", "another program is recording in this Rokh, so nothing was recorded. Try again in a moment."}, true
	case errors.Is(err, vessel.ErrNotAVessel):
		return plainWords{"not_a_vessel", "", "that folder is not a Rokh. Nothing was changed."}, true
	case errors.Is(err, vessel.ErrExists):
		return plainWords{"already_exists", "", "there is already a Rokh in that folder. Nothing was changed."}, true
	case errors.Is(err, errOwnerEmptied):
		return plainWords{"owner_unknown", "", "every owner key of this Rokh was taken back, so it opens for nobody here. Nothing was changed."}, true
	case errors.Is(err, key.ErrOwnerUnknown), errors.Is(err, key.ErrNoOwner):
		return plainWords{"owner_unknown", "", "the keys recorded in this Rokh do not say who owned it at one of its events, so it was not opened. Nothing was changed."}, true
	case errors.Is(err, key.ErrKeyNotLive):
		return plainWords{"key_revoked", "", "the key this passphrase opens was taken back; it opens nothing here."}, true
	case errors.Is(err, carrier.ErrForged), errors.Is(err, key.ErrForged):
		return plainWords{"record_forged", "", "a record in this Rokh was changed after it was made, so it is not shown. Nothing was changed."}, true
	case errors.Is(err, carrier.ErrConflict):
		return plainWords{"content_conflict", "", "two copies of one piece of this Rokh's content differ, so it is not shown."}, true
	case errors.Is(err, carrier.ErrIncomplete):
		return plainWords{"content_incomplete", "", "a part of that content is missing from this Rokh, so it is not shown."}, true
	case errors.Is(err, lineage.ErrAnchorDiffers):
		return plainWords{"anchor_differs", "not recorded", "these are two different Rokhs; they do not meet. Nothing was recorded."}, true
	case errors.Is(err, lineage.ErrAncestryUnproven):
		return plainWords{"ancestry_unproven", "not recorded", "where these events come from could not be proven, so nothing was moved or recorded."}, true
	case errors.Is(err, lineage.ErrSeedRefused):
		return plainWords{"seed_refused", "not recorded", "that seed was refused; nothing was recorded."}, true
	}
	// The key layer says this one in words of its own and has no sentinel
	// for it: a key made where the Rokh gives keys no way into its records.
	if strings.Contains(err.Error(), "gives it no system reader") {
		return plainWords{"view_denied", "", "this passphrase is a key's, and this Rokh gives that key no way to open its records, so nothing was opened."}, true
	}
	// A file the system would not read or write: the system's own reason,
	// without the path inside the Rokh, which says nothing to a person.
	var pe *fs.PathError
	if errors.As(err, &pe) {
		switch pe.Op {
		case "open", "read", "stat", "lstat", "readdir":
			return plainWords{"storage_failed", "", "the disk would not read this Rokh's folder (" + pe.Err.Error() + "). Nothing was changed."}, true
		}
		return plainWords{"storage_failed", "", "the disk would not write to this Rokh's folder (" + pe.Err.Error() + "). " +
			"Check that the disk has room and can be written, and try again."}, true
	}
	return plainWords{}, false
}

// growAdvice is the way out of a full Rokh: its folder, and twice its size.
func growAdvice(l *openLedger) string {
	to := "SIZE"
	if l != nil && l.car != nil {
		info := l.car.Vessel().Info()
		to = sizeFlag(2 * int64(info.Slabs) * int64(info.SlabSize))
	}
	return "To make room, leave and run: rokh grow " + folderOf(l) + " --to " + to
}

func folderOf(l *openLedger) string {
	if l == nil || l.dir == "" {
		return "FOLDER"
	}
	if strings.ContainsAny(l.dir, " \t'\"") {
		return "'" + strings.ReplaceAll(l.dir, "'", `'\''`) + "'"
	}
	return l.dir
}

// sizeFlag writes a size the way a command's flag takes it: 16M, 2G, 512K.
func sizeFlag(n int64) string {
	switch {
	case n >= 1<<30 && n%(1<<30) == 0:
		return itoa64(n>>30) + "G"
	case n >= 1<<20 && n%(1<<20) == 0:
		return itoa64(n>>20) + "M"
	}
	return itoa64((n+1023)>>10) + "K"
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// packagePrefix is a layer's name in front of its own error, which is for
// the programmer who reads a log; internalID is a ruling's or a review's
// number, which is for the people who wrote the code.
var (
	packagePrefix = regexp.MustCompile(`\b(vessel|key|carrier|turn|content|lineage|ledger|event|frame|medium|working|seal|covenant|bundle): `)
	internalID    = regexp.MustCompile(`\s*\((?:(?:[A-Z][0-9]{1,3}(?:\.[0-9]+)?|[a-z]+_[a-z_]+)(?:,\s*)?)+\)`)
)

// tidy is the last resort for an error nothing above recognised: its words
// stay, but without the layer names and the numbers of rulings.
func tidy(msg string) string {
	msg = packagePrefix.ReplaceAllString(msg, "")
	return internalID.ReplaceAllString(msg, "")
}
