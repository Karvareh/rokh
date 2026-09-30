//go:build linux

// rokh-chest keeps a Rokh in one file instead of one folder.
//
// The base profile is a folder, and everything in it is sealed — but a folder
// says how many events are in it, how large each one is and when it was last
// written. A chest says none of that: one file, a size its owner fixes, filled
// with random before anything is written to it, with an encrypted filesystem
// inside. Open it and the vault is at a path; close it and there is a file.
//
// It is a separate command on purpose. Mapping an encrypted device needs
// privilege, and Rokh itself holds none: it opens folders, and this makes a
// folder appear. Neither knows anything about the other.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"rokh/chest"
	"rokh/size"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "make":
		err = make_(os.Args[2:])
	case "open":
		err = open(os.Args[2:])
	case "close":
		err = closeChest(os.Args[2:])
	case "status":
		err = status()
	case "enter":
		err = enter(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "no —", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `rokh-chest - a Rokh in one file

  rokh-chest make PATH --size 2G   make a chest of exactly that size
  rokh-chest open PATH             unlock it and say where the vault is
  rokh-chest enter PATH            open it, run rokh in it, close it after
  rokh-chest close PATH            unmount, lock, and let go of the file
  rokh-chest status                what is open

The size is fixed when the chest is made and the file is filled with random
bytes first, so the file is the same size and looks the same whether it holds
one sentence or ten thousand.

On your own desktop this needs neither sudo nor a password: the disk service
your desktop already trusts does the privileged part for the session you are
sitting at. Over ssh or in a script there is no such session, and then it
needs root.

The passphrase is the chest's own, not the ledger's. It is asked for on the
terminal, or comes from the file named by ROKH_CHEST_PASSPHRASE_FILE.
`)
}

// owner is the person this chest belongs to: the one who invoked us, even
// when they went through sudo to do it.
func owner() int {
	if v := os.Getenv("SUDO_UID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return os.Getuid()
}

func pathAnd(args []string, flags map[string]*string) (string, error) {
	path := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			name := strings.TrimLeft(a, "-")
			value := ""
			if j := strings.IndexByte(name, '='); j >= 0 {
				name, value = name[:j], name[j+1:]
			} else if i+1 < len(args) {
				i++
				value = args[i]
			}
			p, known := flags[name]
			if !known {
				return "", fmt.Errorf("I do not know the flag %q", a)
			}
			*p = value
			continue
		}
		if path != "" {
			return "", fmt.Errorf("I do not know %q", a)
		}
		path = a
	}
	if path == "" {
		return "", errors.New("name the chest's file")
	}
	return filepath.Abs(path)
}

func make_(args []string) error {
	sizeText := ""
	path, err := pathAnd(args, map[string]*string{"size": &sizeText})
	if err != nil {
		return err
	}
	if sizeText == "" {
		return errors.New(`say how large it is: --size 2G. The size is fixed for good`)
	}
	bytes, err := size.Parse(sizeText)
	if err != nil {
		return err
	}
	// The machine is asked before the filling, not after it: a refusal is
	// cheap and a few gigabytes of random are not.
	if err := chest.Ready(); err != nil {
		return err
	}
	pass, err := passphrase("choose a passphrase for the chest — there is no recovery without it: ")
	if err != nil {
		return err
	}
	again, err := passphrase("the same passphrase again: ")
	if err != nil {
		return err
	}
	if string(pass) != string(again) {
		return errors.New("the two passphrases differ; nothing was made")
	}
	k := chest.Keeper(os.Geteuid(), chest.System{})
	say(fmt.Sprintf("making a %s chest at %s, through %s.", size.Write(bytes), path, k.Name()))
	if err := chest.Make(path, bytes, pass, owner(), k, chest.System{}, say); err != nil {
		return err
	}
	fmt.Printf("made. %s is a chest of %s.\n", path, size.Write(bytes))
	fmt.Printf("Open it with:  rokh-chest enter %s\n", path)
	return nil
}

func open(args []string) error {
	path, err := pathAnd(args, map[string]*string{})
	if err != nil {
		return err
	}
	o, err := openIt(path)
	if err != nil {
		return err
	}
	fmt.Printf("open at %s\n", o.Mount)
	fmt.Printf("Your vault is there:  rokh %s\n", o.Mount)
	fmt.Printf("Close it after:       rokh-chest close %s\n", path)
	return nil
}

func openIt(path string) (chest.Opened, error) {
	if err := chest.Ready(); err != nil {
		return chest.Opened{}, err
	}
	pass, err := passphrase("chest passphrase: ")
	if err != nil {
		return chest.Opened{}, err
	}
	return chest.Open(path, pass, owner(), chest.Keeper(os.Geteuid(), chest.System{}))
}

func closeChest(args []string) error {
	path, err := pathAnd(args, map[string]*string{})
	if err != nil {
		return err
	}
	if err := chest.Close(path)(chest.Keeper(os.Geteuid(), chest.System{})); err != nil {
		return err
	}
	fmt.Printf("closed. %s is one file again.\n", path)
	return nil
}

// enter opens a chest, runs Rokh on what is inside it, and closes it again
// however that ends. A chest left open is a chest that is not closed, and the
// one command a person runs should not depend on their remembering the second.
func enter(args []string) error {
	path, err := pathAnd(args, map[string]*string{})
	if err != nil {
		return err
	}
	o, err := openIt(path)
	if err != nil {
		return err
	}
	// The chest is open now, so the closing must happen whatever comes next:
	// Rokh failing, the person leaving, or an interrupt.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	rokh := "rokh"
	if beside, err := os.Executable(); err == nil {
		if candidate := filepath.Join(filepath.Dir(beside), "rokh"); exists(candidate) {
			rokh = candidate
		}
	}
	cmd := exec.Command(rokh, o.Mount)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	runErr := cmd.Run()

	closeErr := chest.Close(path)(chest.Keeper(os.Geteuid(), chest.System{}))
	if closeErr != nil {
		return fmt.Errorf("rokh has finished, and the chest would not close: %w", closeErr)
	}
	fmt.Printf("closed. %s is one file again.\n", path)
	var exit *exec.ExitError
	if errors.As(runErr, &exit) {
		os.Exit(exit.ExitCode())
	}
	return runErr
}

func exists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func status() error {
	open := chest.Openings()
	if len(open) == 0 {
		fmt.Println("nothing is open.")
		return nil
	}
	for _, o := range open {
		where := o.Mount
		if where == "" {
			where = "not mounted"
		}
		fmt.Printf("%s\n  %s -> %s at %s\n", o.Path, o.Loop, o.Clear, where)
	}
	return nil
}

func say(line string) { fmt.Fprintln(os.Stderr, line) }

// passphrase asks on the terminal with the echo off, or reads the file a
// script named. It is the chest's own secret and has its own name for it: the
// ledger inside has another, and one is not the other.
func passphrase(prompt string) ([]byte, error) {
	if f := os.Getenv("ROKH_CHEST_PASSPHRASE_FILE"); f != "" {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		return []byte(strings.TrimRight(string(raw), "\r\n")), nil
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, errors.New("a passphrase is needed and there is no terminal to ask on; " +
			"set ROKH_CHEST_PASSPHRASE_FILE")
	}
	defer tty.Close()
	quiet := stty(tty, "-echo") == nil
	fmt.Fprint(tty, prompt)
	line, err := readLine(tty)
	if quiet {
		_ = stty(tty, "echo")
		fmt.Fprintln(tty)
	}
	if err != nil && line == "" {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil, errors.New("no passphrase was given")
	}
	return []byte(line), nil
}

func stty(tty *os.File, arg string) error {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = tty
	return cmd.Run()
}

func readLine(f *os.File) (string, error) {
	var b []byte
	one := make([]byte, 1)
	for {
		n, err := f.Read(one)
		if n > 0 {
			if one[0] == '\n' {
				break
			}
			b = append(b, one[0])
		}
		if err != nil {
			return string(b), err
		}
	}
	return string(b), nil
}
