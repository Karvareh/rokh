// Command rokh-home holds a Rokh home open behind its gate, and speaks to that
// gate as the owner or as a program.
//
//	rokh-home init    HOME --message TEXT [--passphrase-file F|-] [--passphrase-fd N]
//	rokh-home serve   HOME --run DIR      [--passphrase-file F|-] [--passphrase-fd N] [--native-dir DIR]
//	rokh-home owner   HOME --run DIR      [--passphrase-file F|-] [--passphrase-fd N] OP [JSON]
//	rokh-home program (--fd N | --socket PATH --credential-file F) [--lines] [OP [JSON]]
//	rokh-home version
//
// The passphrase follows the one rule of every Rokh command: --passphrase-file,
// then ROKH_PASSPHRASE_FILE, then ROKH_PASSPHRASE, then the terminal with its
// echo off; --passphrase-fd reads an inherited descriptor the same way. Never
// an argument. The gate refuses to serve if it cannot harden its own process.
//
// Nothing here scans, watches or brings in anything by itself. A file enters a
// home only when an import names its source and its path, and is run.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"rokh-home/gate"
	"rokh-home/home"
	"rokh-home/native"
	"rokh-home/store"
	"rokh/passphrase"
)

// Release is set at build time: -ldflags "-X main.Release=0.1.0".
var Release = ""

func main() {
	passphrase.TerminalSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	code := 0
	switch os.Args[1] {
	case "init":
		err = cmdInit(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	case "owner":
		code, err = cmdOwner(os.Args[2:])
	case "program":
		code, err = cmdProgram(os.Args[2:])
	case "version", "--version", "-v":
		rel := Release
		if rel == "" {
			rel = "(no release name; built from source)"
		}
		fmt.Printf("rokh-home   %s\nprotocol    %s\nenclosure   %s\ntarget      %s/%s\ngo          %s\n",
			rel, gate.Protocol, gate.Enclosure, runtime.GOOS, runtime.GOARCH, runtime.Version())
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rokh-home:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func usage() {
	fmt.Fprint(os.Stderr, `rokh-home - a Rokh home behind its gate

  rokh-home init    HOME --message TEXT [--passphrase-file F|-] [--passphrase-fd N]
  rokh-home serve   HOME --run DIR      [--passphrase-file F|-] [--passphrase-fd N] [--native-dir DIR]
  rokh-home owner   HOME --run DIR      [--passphrase-file F|-] [--passphrase-fd N] OP [JSON]
  rokh-home program (--fd N | --socket PATH --credential-file F) [--lines] [OP [JSON]]
  rokh-home version

The installed entry point is also: rokh home <command>.
serve accepts --host-offer FILE for the host's execution ceiling.
owner accepts --request-fd N instead of OP [JSON] for private request content.
Use separate descriptors for the passphrase and the complete JSON request.
program --lines exits nonzero if any request fails; unknown durability exits 4.
`)
}

func splitDir(name string, args []string) (*flag.FlagSet, string, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fs, "", args, fmt.Errorf("rokh-home %s: name the home's folder first", name)
	}
	return fs, args[0], args[1:], nil
}

func passFlags(fs *flag.FlagSet) (*string, *int) {
	return fs.String("passphrase-file", "", "a file holding the passphrase, or - for standard input"),
		fs.Int("passphrase-fd", -1, "an inherited descriptor to read the passphrase from")
}

// readPassphrase follows the one rule of every Rokh command (contract section
// 6): --passphrase-file (- is standard input), then ROKH_PASSPHRASE_FILE, then
// ROKH_PASSPHRASE, then the terminal. --passphrase-fd names an inherited
// descriptor, read by the same rule as a file.
func readPassphrase(file string, fd int) (string, error) {
	if fd >= 0 {
		f := os.NewFile(uintptr(fd), "passphrase")
		if f == nil {
			return "", errors.New("no such descriptor")
		}
		defer f.Close()
		return passphrase.Read(f)
	}
	return passphrase.From(file, os.Stdin)
}

func cmdInit(args []string) error {
	fs, dir, rest, err := splitDir("init", args)
	if err != nil {
		return err
	}
	message := fs.String("message", "", "the first page of the home's ledger")
	file, fd := passFlags(fs)
	iter := fs.Int("test-iterations", 0, "lower the key derivation cost; for synthetic test homes only")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *message == "" {
		return errors.New("--message is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	pass, err := readPassphrase(*file, *fd)
	if err != nil {
		return err
	}
	var iters []int
	if *iter > 0 {
		iters = []int{*iter}
	}
	if err := home.Create(dir, pass, *message, iters...); err != nil {
		return err
	}
	fmt.Printf("{\"ok\":true,\"home\":%q}\n", dir)
	return nil
}

func cmdServe(args []string) error {
	fs, dir, rest, err := splitDir("serve", args)
	if err != nil {
		return err
	}
	run := fs.String("run", "", "the private folder for the gate's sockets")
	hostFile := fs.String("host-offer", "", "the trusted host's explicit execution ceiling (JSON)")
	nativeDir := fs.String("native-dir", "", "a folder holding an engine's files and its manifest")
	engineManifest := fs.String("engine-manifest", "", "the engine manifest (default: engine.json in --native-dir)")
	file, fd := passFlags(fs)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *run == "" {
		return errors.New("--run is required")
	}
	if err := gate.Harden(); err != nil {
		return fmt.Errorf("the gate could not harden its own process, and will not serve: %w", err)
	}
	pass, err := readPassphrase(*file, *fd)
	if err != nil {
		return err
	}
	h, err := home.Open(dir, pass, Release)
	pass = ""
	if err != nil {
		return err
	}
	offer := gate.LocalHostOffer()
	if *hostFile != "" {
		b, readErr := os.ReadFile(*hostFile)
		if readErr != nil {
			h.Close()
			return readErr
		}
		offer = gate.HostOffer{}
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&offer); err != nil {
			h.Close()
			return err
		}
	}
	s, err := gate.StartWithHost(h, *run, offer)
	if err != nil {
		h.Close()
		return err
	}
	// The native engine is opt-in and named on the command line. Without it the
	// home serves exactly as before and says there is no engine here.
	if *nativeDir != "" {
		bin, err := native.LoadEngine(*nativeDir, *engineManifest)
		if err != nil {
			s.Close()
			h.Close()
			return fmt.Errorf("the engine manifest: %w", err)
		}
		s.EnableNative(bin)
	}
	ready, _ := json.Marshal(map[string]any{"ready": true, "pid": os.Getpid(), "protocol": gate.Protocol,
		"owner_socket": filepath.Join(*run, "owner.sock"), "gate_socket": filepath.Join(*run, "gate.sock"),
		"enclosure": gate.Enclosure, "native": *nativeDir != ""})
	fmt.Println(string(ready))
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(stop)
running:
	for {
		select {
		case sig := <-stop:
			if sig == syscall.SIGHUP {
				s.RevokeHosting("host supervisor withdrew execution")
				continue
			}
			break running
		case <-s.Done():
			break running
		}
	}
	// The engine is put away before the home closes, and what happened to it is
	// said out loud. A signal that could not seal the last state exits non-zero
	// and leaves the runtime folder where it is, so the owner can open this home
	// again and finish the job rather than discover the loss later.
	report := s.CloseReporting("home:signal")
	h.Close()
	if report != nil {
		line, _ := json.Marshal(map[string]any{"closed": report["ok"] == true, "quiesce": report})
		if report["ok"] == true {
			fmt.Println(string(line))
		} else {
			fmt.Fprintln(os.Stderr, string(line))
			return errors.New("the engine was not put away; the runtime folders are kept, reopen this home to finish")
		}
	}
	return nil
}

func request(rest []string) (map[string]any, error) {
	if len(rest) == 0 {
		return nil, errors.New("name an op")
	}
	if len(rest) > 2 {
		return nil, errors.New("a request takes one op and at most one JSON object")
	}
	req := map[string]any{}
	if len(rest) > 1 {
		if err := json.Unmarshal([]byte(rest[1]), &req); err != nil {
			return nil, fmt.Errorf("the request is not a JSON object: %w", err)
		}
	}
	if req == nil {
		return nil, errors.New("the request must be a JSON object, not null")
	}
	req["op"] = rest[0]
	return req, nil
}

// Read private request content from an inherited pipe instead of process
// arguments or the shell's command history. The whole request includes op.
func requestFD(fd int, rest []string) (map[string]any, error) {
	if fd < 0 {
		return request(rest)
	}
	if len(rest) != 0 {
		return nil, errors.New("use either --request-fd or a positional request")
	}
	f := os.NewFile(uintptr(fd), "request")
	if f == nil {
		return nil, errors.New("no such request descriptor")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, gate.MaxLine+1))
	if err != nil {
		return nil, err
	}
	if len(b) > gate.MaxLine {
		return nil, errors.New("request exceeds the gate's size limit")
	}
	var req map[string]any
	if err := json.Unmarshal(b, &req); err != nil {
		return nil, fmt.Errorf("the request is not a JSON object: %w", err)
	}
	if op, ok := req["op"].(string); !ok || op == "" {
		return nil, errors.New("the request needs an op")
	}
	return req, nil
}

func printAnswer(r map[string]any) int {
	b, _ := json.Marshal(r)
	fmt.Println(string(b))
	if r["record"] == "unknown" || r["code"] == "durability_unknown" {
		return 4
	}
	if r["ok"] == true {
		return 0
	}
	if res, ok := r["result"].(map[string]any); ok && res["record"] == "unknown" {
		return 4
	}
	return 1
}

func cmdOwner(args []string) (int, error) {
	fs, dir, rest, err := splitDir("owner", args)
	if err != nil {
		return 2, err
	}
	run := fs.String("run", "", "the gate's private folder")
	file, fd := passFlags(fs)
	requestFile := fs.Int("request-fd", -1, "read the complete JSON request from an inherited descriptor")
	if err := fs.Parse(rest); err != nil {
		return 2, err
	}
	if *requestFile >= 0 && (*requestFile == *fd || *requestFile == 0 && *file == "-") {
		return 2, errors.New("passphrase and request need separate descriptors")
	}
	req, err := requestFD(*requestFile, fs.Args())
	if err != nil {
		return 2, err
	}
	pass, err := readPassphrase(*file, *fd)
	if err != nil {
		return 1, err
	}
	key, err := store.OwnerKey(dir, pass)
	pass = ""
	if err != nil {
		return 1, err
	}
	oc, err := gate.DialOwner(filepath.Join(*run, "owner.sock"), key)
	if err != nil {
		return 1, err
	}
	defer oc.Close()
	r, err := oc.Call(req)
	if err != nil {
		return 1, err
	}
	return printAnswer(r), nil
}

func cmdProgram(args []string) (int, error) {
	fs := flag.NewFlagSet("program", flag.ContinueOnError)
	fd := fs.Int("fd", -1, "the descriptor the gate handed this program")
	sock := fs.String("socket", "", "the gate's socket, when no descriptor was handed")
	credFile := fs.String("credential-file", "", "a file holding this program's credential, for --socket")
	lines := fs.Bool("lines", false, "read requests line by line from standard input")
	if err := fs.Parse(args); err != nil {
		return 2, err
	}
	var c *gate.Client
	switch {
	case *fd >= 0:
		f := os.NewFile(uintptr(*fd), "gate")
		conn, err := net.FileConn(f)
		f.Close()
		if err != nil {
			return 1, err
		}
		c = gate.NewClient(conn)
		// The gate bound this connection to this program before it ran: the
		// empty credential names it (rokh.booth/1: hello, then prove).
		r, err := c.Bind("")
		if err != nil {
			return 1, err
		}
		if r["ok"] != true {
			return printAnswer(r), errors.New("the gate did not bind this program")
		}
	case *sock != "":
		cred, err := os.ReadFile(*credFile)
		if err != nil {
			return 1, err
		}
		if c, err = gate.Dial(*sock); err != nil {
			return 1, err
		}
		r, err := c.Bind(strings.TrimSpace(string(cred)))
		if err != nil {
			return 1, err
		}
		if r["ok"] != true {
			return printAnswer(r), errors.New("the gate did not bind this program")
		}
	default:
		if v := os.Getenv("ROKH_GATE_FD"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return 2, err
			}
			return cmdProgram(append([]string{"--fd", strconv.Itoa(n)}, args...))
		}
		return 2, errors.New("give --fd or --socket")
	}
	defer c.Close()
	if *lines {
		in := bufio.NewScanner(os.Stdin)
		in.Buffer(make([]byte, 0, 64<<10), gate.MaxLine)
		result := 0
		for in.Scan() {
			var req map[string]any
			if err := json.Unmarshal(in.Bytes(), &req); err != nil {
				return max(result, 2), err
			}
			r, err := c.Call(req)
			if err != nil {
				return max(result, 1), err
			}
			result = max(result, printAnswer(r))
		}
		if err := in.Err(); err != nil {
			return max(result, 1), err
		}
		return result, nil
	}
	req, err := request(fs.Args())
	if err != nil {
		return 2, err
	}
	r, err := c.Call(req)
	if err != nil {
		return 1, err
	}
	return printAnswer(r), nil
}
