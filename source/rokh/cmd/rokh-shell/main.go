// Command rokh-shell is the sentence surface on its own.
//
// It is a thin front over package shell, which the rokh command also carries:
// somebody who types "rokh" with nothing after it gets this, because a person
// installing one thing should not have to remember a second program's name.
// This command stays for anyone who wants only the surface and nothing else.
package main

import (
	"fmt"
	"io"
	"os"
	"syscall"

	"rokh/shell"
)

func main() {
	shell.TerminalSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}
	os.Exit(shell.RunWithHome("rokh-shell", os.Args[1:], inheritedHomeStream))
}

func inheritedHomeStream(fd int) (io.ReadWriteCloser, error) {
	if fd < 3 {
		return nil, fmt.Errorf("the home gate must be an inherited descriptor above stderr")
	}
	f := os.NewFile(uintptr(fd), "rokh-home-gate")
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		f.Close()
		return nil, fmt.Errorf("descriptor %d is not a connected home socket", fd)
	}
	if err := closeOnExec(fd); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
