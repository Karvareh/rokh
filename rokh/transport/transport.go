// Package transport is the host adapter that carries rokh.booth/1: a Unix
// socket, a TCP listener on 127.0.0.1, a pipe, standard input and output, or a
// descriptor a parent handed down. It lives outside the core and outside
// package booth (contract B1, 2.9): the protocol needs an ordered byte stream
// and nothing else, and none of these is an identity.
package transport

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

// Networks this adapter offers.
const (
	Unix = "unix"
	TCP  = "tcp"
)

// Listen opens a listener. A Unix socket is made with mode 0600 and refuses to
// take over a path that exists; TCP listens on the loopback interface only.
func Listen(network, address string) (net.Listener, error) {
	switch network {
	case Unix:
		if len(address) >= 104 {
			return nil, fmt.Errorf("transport: a socket path is %d bytes; the limit is about 104", len(address))
		}
		if fi, err := os.Lstat(address); err == nil {
			if fi.Mode()&os.ModeSocket == 0 {
				return nil, fmt.Errorf("transport: %s exists and is not a socket", address)
			}
			if c, err := net.Dial(Unix, address); err == nil {
				c.Close()
				return nil, fmt.Errorf("transport: something is listening on %s", address)
			}
			if err := os.Remove(address); err != nil {
				return nil, err
			}
		}
		ln, err := net.Listen(Unix, address)
		if err != nil {
			return nil, err
		}
		if err := os.Chmod(address, 0o600); err != nil {
			ln.Close()
			return nil, err
		}
		return ln, nil
	case TCP:
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if host != "127.0.0.1" && host != "::1" && host != "localhost" {
			return nil, errors.New("transport: a booth listens on the loopback interface only")
		}
		return net.Listen(TCP, address)
	}
	return nil, fmt.Errorf("transport: no network %q; unix or tcp", network)
}

// Dial connects to a listener.
func Dial(network, address string) (net.Conn, error) {
	switch network {
	case Unix, TCP:
		return net.Dial(network, address)
	}
	return nil, fmt.Errorf("transport: no network %q; unix or tcp", network)
}

// Parse reads an address of the form unix:PATH or tcp:HOST:PORT.
func Parse(addr string) (network, address string, err error) {
	i := strings.IndexByte(addr, ':')
	if i < 0 {
		return Unix, addr, nil
	}
	network, address = addr[:i], addr[i+1:]
	if network != Unix && network != TCP {
		return "", "", fmt.Errorf("transport: no network %q in %q", network, addr)
	}
	return network, address, nil
}

// Pipe is an in-memory connected pair.
func Pipe() (net.Conn, net.Conn) { return net.Pipe() }

// Stdio is standard input and output as one stream.
func Stdio() io.ReadWriteCloser { return stdio{} }

type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return nil }

// Streams joins a reader and a writer into one stream: two pipes, a child's
// standard input and output.
func Streams(r io.Reader, w io.Writer) io.ReadWriter { return rw{r, w} }

type rw struct {
	io.Reader
	io.Writer
}

// Inherited is a descriptor a parent handed down, above standard error.
func Inherited(fd int) (io.ReadWriteCloser, error) {
	if fd < 3 {
		return nil, errors.New("transport: an inherited stream is a descriptor above standard error")
	}
	f := os.NewFile(uintptr(fd), "rokh-booth")
	if f == nil {
		return nil, fmt.Errorf("transport: no descriptor %d", fd)
	}
	if c, err := net.FileConn(f); err == nil {
		f.Close()
		return c, nil
	}
	return f, nil
}

// Serve accepts streams until the listener closes and hands each to fn.
func Serve(ln net.Listener, fn func(io.ReadWriteCloser)) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go func() {
			defer c.Close()
			fn(c)
		}()
	}
}
