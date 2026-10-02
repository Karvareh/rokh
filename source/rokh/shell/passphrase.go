package shell

import (
	"crypto/rand"
	"io"
	"os"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/passphrase"
	"rokh/vessel"
)

// The shell is a host: it gives the core its system's randomness (C5), and
// the key layer sets the shell's opening hooks (v1_open.go).
func init() {
	if event.Entropy == nil {
		event.Entropy = rand.Reader
	}
	UnlockV1 = func(pass string) vessel.Unlock { return vessel.Unlock(key.Unlock(pass)) }
	// The session of an opened carrier and the judge and owner check of a
	// union, read from the carrier's own ledger (v1_keylayer.go).
	SealerV1 = func(c *carrier.Carrier, pass string) (carrier.Sealer, error) {
		sess, _, _, err := keyLayerOf(c, pass)
		if err != nil {
			return nil, err
		}
		return sess, nil
	}
	JudgeV1 = func(c *carrier.Carrier, pass string) (func(frame.ID) string, func(frame.ID, []byte) bool, error) {
		_, _, k, err := keyLayerOf(c, pass)
		if err != nil {
			return nil, nil, err
		}
		return k.judge, k.covers, nil
	}
}

// TerminalSignals is the command's termination policy while a secret is
// being read. Set it before starting the shell.
var TerminalSignals = []os.Signal{os.Interrupt}

// Passphrase gets the passphrase by the one rule, with no file named.
func Passphrase() (string, error) { return PassphraseFrom("", os.Stdin) }

// PassphraseFrom is the one passphrase rule of every command (package
// passphrase).
func PassphraseFrom(file string, stdin io.Reader) (string, error) {
	passphrase.TerminalSignals = TerminalSignals
	return passphrase.From(file, stdin)
}

// ReadPassphrase reads a passphrase from a stream by the one rule.
func ReadPassphrase(r io.Reader) (string, error) { return passphrase.Read(r) }

// ask reads a secret from the terminal with the echo off. It is a variable
// so that a test can answer the gate's questions and read what it asked.
var ask = func(prompt string) (string, error) {
	passphrase.TerminalSignals = TerminalSignals
	return passphrase.Hidden(prompt)
}

// askVisible reads one visible line from the terminal; a variable for the
// same reason as ask.
var askVisible = func(prompt string) (string, error) { return passphrase.Visible(prompt) }
