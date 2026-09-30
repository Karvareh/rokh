package main

import (
	"bufio"
	"crypto/ecdh"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"rokh/bundle"
	"rokh/frame"
	"rokh/seal"
)

// cmdOpen is the recipient's side of a sealed bundle: it turns one back into a
// plain one, using the reading key in their own carrier.
//
// It is a third act rather than part of apply, and that is the whole design
// held to. Sealing happens where the sender's keys and covenants are. Opening
// happens where the recipient's keys are. Apply stays keyless and untrusted in
// between, because a courier that could open what it carries would not be a
// courier — it would be a reader.
//
// So this needs the carrier and its passphrase, and apply still does not.
//
//	— T13.1, T7.4, T8
func cmdOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	dir := fs.String("dir", "", "your own carrier, which holds the reading key")
	in := fs.String("in", "", "the sealed bundle")
	out := fs.String("out", "", "where to write the opened bundle")
	fs.StringVar(&passphraseFile, "passphrase-file", "", "a file holding the passphrase, or - for standard input")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" || *in == "" || *out == "" {
		return errors.New("--dir, --in and --out are required")
	}

	pass, err := passphrase()
	if err != nil {
		return err
	}
	// The owner's reader is the reading key: it is in the owner's cell of
	// every v1 vessel, so there is nothing to make first.
	_, sec, err := openOwner(*dir, pass)
	if err != nil {
		return err
	}
	if sec.Reader == ([32]byte{}) {
		return errors.New("this passphrase holds no reading key in this carrier, so nothing was sealed to it")
	}
	priv, err := seal.ReadingFrom(sec.Reader[:])
	if err != nil {
		return fmt.Errorf("the stored reading key is not one: %w", err)
	}

	src, err := os.Open(*in)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%s: %w (refusing to overwrite)", *out, err)
	}
	defer dst.Close()
	w := bufio.NewWriter(dst)

	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	opened, plain, first := 0, 0, true
	for sc.Scan() {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		if first {
			first = false
			var h bundle.Have
			if err := json.Unmarshal([]byte(text), &h); err == nil && h.Kind == bundle.KindHave {
				if _, err := w.WriteString(text + "\n"); err != nil {
					return err
				}
				continue
			}
		}
		var ln line
		if err := json.Unmarshal([]byte(text), &ln); err != nil {
			return fmt.Errorf("bad bundle line: %w", err)
		}
		if ln.Box == "" {
			// Already in the clear. It is copied through rather than refused:
			// a bundle whose covenant named no reading key is a perfectly good
			// bundle, and running this on one should not be an error.
			plain++
			if _, err := w.WriteString(text + "\n"); err != nil {
				return err
			}
			continue
		}
		raw, err := unseal(priv, ln)
		if err != nil {
			return fmt.Errorf("%s: %w", short(ln.ID), err)
		}
		out := line{ID: ln.ID, Raw: hex.EncodeToString(raw)}
		b, err := json.Marshal(out)
		if err != nil {
			return err
		}
		if _, err := w.WriteString(string(b) + "\n"); err != nil {
			return err
		}
		opened++
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "%d sealed, %d already in the clear, written to %s\n",
		opened, plain, *out)
	if opened == 0 && plain > 0 {
		fmt.Fprintln(os.Stderr, "Nothing in this bundle was sealed; it is a copy.")
	}
	return nil
}

// unseal opens one line and checks that what came out is the event it claimed
// to be.
//
// The name check is not belt and braces. The seal binds the event's name as
// context, so a box cannot be re-offered under another name — but the *label*
// on the line is the courier's to write, and a courier that relabels lines
// would otherwise have its labels believed. What the bytes hash to is what the
// event is.
//
//	— T3.1, T3.2
func unseal(priv *ecdh.PrivateKey, ln line) ([]byte, error) {
	id, err := frame.ParseID(ln.ID)
	if err != nil {
		return nil, fmt.Errorf("the line's name is not a name: %w", err)
	}
	eph, err := hex.DecodeString(ln.Eph)
	if err != nil {
		return nil, errors.New("the ephemeral key is not hex")
	}
	box, err := hex.DecodeString(ln.Box)
	if err != nil {
		return nil, errors.New("the sealed box is not hex")
	}
	raw, err := seal.Open(priv, seal.Sealed{Ephemeral: eph, Box: box}, id[:])
	if err != nil {
		return nil, fmt.Errorf("would not open: %w", err)
	}
	if got := frame.Hash(raw); got != id {
		return nil, fmt.Errorf("opened to %s, and the line calls it %s",
			got.Short(), id.Short())
	}
	return raw, nil
}
