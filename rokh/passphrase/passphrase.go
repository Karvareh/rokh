// Package passphrase is the one passphrase rule of every command of Rokh
// (contract section 6): --passphrase-file ("-" is standard input), then
// ROKH_PASSPHRASE_FILE, then ROKH_PASSPHRASE, then the terminal with the echo
// off. It is host code: it reads files, the environment and the terminal.
package passphrase

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
)

// TerminalSignals is the command's termination policy while a secret is
// being read. Set it before starting the shell. The portable shell knows the
// interrupt; an executable can also name its platform's termination signals.
var TerminalSignals = []os.Signal{os.Interrupt}

// Passphrase gets the passphrase to open a carrier by the one rule every
// command of Rokh follows, with no file named on the command line.
func Ask() (string, error) { return From("", os.Stdin) }

// PassphraseFrom is the one passphrase rule of every command of rokh,
// rokh-courier, the shell and rokh-home (contract section 6):
//
//  1. the file named by --passphrase-file, "-" being standard input;
//  2. the file named by ROKH_PASSPHRASE_FILE;
//  3. ROKH_PASSPHRASE;
//  4. the terminal, with the echo off.
//
// A file or a stream is read to its end, at most 4096 bytes; its line ending
// is dropped; an empty passphrase and a longer one are refused, never
// truncated. An environment variable is fine for a test and wrong for a
// person: visible to anything that lists processes and inherited by every
// child. Nothing is guessed and nothing is stored.
func From(file string, stdin io.Reader) (string, error) {
	switch {
	case file == "-":
		return Read(stdin)
	case file != "":
		return passphraseFile(file)
	}
	if f := os.Getenv("ROKH_PASSPHRASE_FILE"); f != "" {
		return passphraseFile(f)
	}
	if p := os.Getenv("ROKH_PASSPHRASE"); p != "" {
		return checkPassphrase(p)
	}
	return Hidden("passphrase: ")
}

func passphraseFile(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return Read(f)
}

// ReadPassphrase reads a passphrase from a file, a stream or a descriptor by
// the one rule: to its end, at most 4096 bytes, without its line ending.
func Read(r io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, 4097))
	if err != nil {
		return "", err
	}
	if len(b) > 4096 {
		return "", errors.New("the passphrase exceeds 4096 bytes; it was not truncated")
	}
	return checkPassphrase(strings.TrimRight(string(b), "\r\n"))
}

func checkPassphrase(p string) (string, error) {
	if p == "" {
		return "", errors.New("the passphrase is empty")
	}
	if len(p) > 4096 {
		return "", errors.New("the passphrase exceeds 4096 bytes; it was not truncated")
	}
	return p, nil
}

// ask reads one line from the terminal with the echo turned off.
//
// It reads from /dev/tty rather than standard input, so that a passphrase can
// still be typed when something is piped in — and so that a passphrase never
// arrives from a pipe by accident, which would put it somewhere the person did
// not choose.
//
// Turning the echo off is done with stty, because Go's standard library has no
// portable way to do it and this project takes no dependencies. If stty is
// unavailable, the secret is not requested. The caller can use its explicit
// file or descriptor input instead.
func Hidden(prompt string) (string, error) {
	pass, err := FromTTY(prompt, true)
	if err != nil {
		return "", err
	}
	if pass == "" {
		return "", errors.New("no passphrase was given")
	}
	return pass, nil
}

// askVisible reads one line from the terminal with the echo left on: a folder,
// a name, an answer — the things a person is meant to see themselves type. It
// reads from the same place ask does, and for the same reason: the person is
// at the terminal, whatever is on standard input.
func Visible(prompt string) (string, error) {
	return FromTTY(prompt, false)
}

// fromTTY is the one reader behind ask and askVisible. The line comes back
// without its ending; an empty line is an empty answer, and the caller says
// what that means.
func FromTTY(prompt string, hidden bool) (line string, err error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		if hidden {
			return "", errors.New("a passphrase is needed and there is no terminal to ask on; " +
				"set ROKH_PASSPHRASE_FILE")
		}
		return "", errors.New("there is no terminal to ask on")
	}
	defer tty.Close()

	if !hidden {
		fmt.Fprint(tty, prompt)
		line, err = bufio.NewReader(io.LimitReader(tty, 4097)).ReadString('\n')
		if len(line) > 4096 {
			return "", errors.New("terminal input exceeds 4096 bytes")
		}
		if err != nil && line == "" {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}

	// Catch termination before changing the terminal, and restore its exact
	// previous mode before the signal handler is removed. In particular, do
	// not turn echo on if it was already off when this command arrived.
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, TerminalSignals...)
	defer signal.Stop(interrupted)
	stty := func(arg string) ([]byte, error) {
		cmd := exec.Command("/bin/stty", arg)
		cmd.Stdin = tty
		return cmd.Output()
	}
	mode, err := stty("-g")
	if err != nil {
		return "", errors.New("could not read terminal mode; passphrase was not requested")
	}
	if _, err = stty("-echo"); err != nil {
		return "", errors.New("could not disable terminal echo; passphrase was not requested")
	}
	defer func() {
		if _, restoreErr := stty(strings.TrimSpace(string(mode))); restoreErr != nil {
			line = ""
			err = errors.Join(err, errors.New("could not restore terminal mode; run stty sane"))
		}
		fmt.Fprintln(tty)
	}()
	type input struct {
		line string
		err  error
	}
	read := make(chan input, 1)
	fmt.Fprint(tty, prompt)
	go func() {
		line, err := bufio.NewReader(io.LimitReader(tty, 4097)).ReadString('\n')
		read <- input{line, err}
	}()
	select {
	case <-interrupted:
		return "", errors.New("passphrase entry interrupted")
	case got := <-read:
		if len(got.line) > 4096 {
			return "", errors.New("the passphrase exceeds 4096 bytes; it was not truncated")
		}
		if got.err != nil {
			return "", got.err
		}
		return strings.TrimRight(got.line, "\r\n"), nil
	}
}
