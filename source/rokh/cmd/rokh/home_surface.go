package main

import (
	"fmt"
	"io"
	"os"
)

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
