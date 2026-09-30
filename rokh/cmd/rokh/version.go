package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"rokh/frame"
	"rokh/generation"
)

// Release is the release name, set at build time:
//
//	go build -ldflags "-X main.Release=1.0.0" ./cmd/rokh
//
// Unset it says so rather than claiming a number. A binary that reports a
// version it was not given is worse than one that admits it does not know.
var Release = ""

// cmdVersion prints what this binary is, in enough detail that a report about
// it can be acted on.
//
// A shipped binary that cannot say which one it is makes every bug report
// useless: the first question is always "which build", and without an answer
// there is nothing to compare against. So this prints the release if it was
// given one, the commit and whether the tree was clean when it was built, the
// toolchain, the target, and the generation of the byte format it writes —
// because a ledger written by one generation is read by whoever kept that
// generation's verifier.
func cmdVersion(args []string) error {
	rel := Release
	if rel == "" {
		rel = "(no release name; built from source)"
	}
	fmt.Printf("rokh        %s\n", rel)

	info, ok := debug.ReadBuildInfo()
	if ok {
		var rev, when, dirty string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.time":
				when = s.Value
			case "vcs.modified":
				if s.Value == "true" {
					dirty = " (with uncommitted changes)"
				}
			}
		}
		if rev != "" {
			if len(rev) > 12 {
				rev = rev[:12]
			}
			fmt.Printf("commit      %s%s\n", rev, dirty)
		}
		if when != "" {
			fmt.Printf("committed   %s\n", when)
		}
		fmt.Printf("go          %s\n", info.GoVersion)
	} else {
		fmt.Printf("go          %s\n", runtime.Version())
	}
	fmt.Printf("target      %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("writes      %s\n", generation.Current)
	fmt.Printf("name size   %d bytes\n", frame.IDSize)

	if len(args) > 0 && args[0] != "" {
		fmt.Fprintln(os.Stderr, "version takes no arguments")
	}
	return nil
}
