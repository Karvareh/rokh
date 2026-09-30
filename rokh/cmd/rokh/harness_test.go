package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh/daemon"
)

// startBoundDaemon opens a ledger and serves it, because a binding is a live
// thing on a running daemon and not a file anywhere. The carrier is a v1
// vessel made by the command itself.
func startBoundDaemon(t *testing.T) string {
	t.Helper()
	bondSetup(t)
	dir := filepath.Join(t.TempDir(), "c")
	if _, _, err := bondRun(t, cmdInit, dir, "--message", "genesis"); err != nil {
		t.Fatal(err)
	}
	car := openForTest(t, dir, bondPass)
	led := ledgerOf(t, car)
	// Short, because a Unix socket path is bounded by sun_path and the usual
	// test temporary directory is not.
	sockDir, err := shortTemp(t, "rk")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(sockDir, "s")
	ln, err := daemon.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := daemon.New(car, led, daemon.Options{})
	go srv.Serve(ln)
	t.Cleanup(func() {
		ln.Close()
		os.RemoveAll(sockDir)
	})
	return socket
}

// wakeHarnessArgs is the case the design keeps in mind: one machine waking
// another. Waking a machine that is already awake is harmless, so a repeat is
// idempotent and the harness may send again on its own. It cannot be undone,
// because a machine cannot be un-woken. An ending it never learns goes to a
// person rather than being closed and forgotten.
//
//	— T10.7, T10.4, T11.10
func wakeHarnessArgs(socket string) []string {
	return []string{
		"--socket", socket,
		"--namespace", "wake",
		"--version", "1",
		"--can", "wake.machine,wake.ask",
		"--unknown", "refuse",
		"--repeat", "idempotent",
		"--retry", "may-retry",
		"--compensate", "irreversible",
		"--ending", "ask-a-person",
	}
}

// bindArgsWithout drops one flag and the answer that followed it.
func bindArgsWithout(args []string, flag string) []string {
	out := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == flag {
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// A harness says four things about meaning and four about the effects it lands
// outside Rokh, and all eight go in one command.
//
//	— T11.10, T10.7
func TestAllEightDeclarationsBindAHarness(t *testing.T) {
	socket := startBoundDaemon(t)
	var out bytes.Buffer
	if err := bindHarness(&out, wakeHarnessArgs(socket)); err != nil {
		t.Fatalf("all eight were said and it did not bind: %v", err)
	}
	for _, said := range []string{
		"wake", "1", "wake.ask", "wake.machine", "refuse",
		"idempotent", "may-retry", "irreversible", "ask-a-person",
	} {
		if !strings.Contains(out.String(), said) {
			t.Errorf("what was bound does not show %q:\n%s", said, out.String())
		}
	}
}

// Seven of eight is not a covenant with a default. Whichever one is missing is
// named, and nothing is sent: the socket below is a path nothing listens on,
// so a refusal that needed the daemon would fail differently.
//
//	— T11.10, T10.7
func TestOneMissingDeclarationRefusesAndSaysWhich(t *testing.T) {
	for _, flag := range []string{
		"--namespace", "--version", "--can", "--unknown",
		"--repeat", "--retry", "--compensate", "--ending",
	} {
		var out bytes.Buffer
		err := bindHarness(&out, bindArgsWithout(
			wakeHarnessArgs("/tmp/rokh-nothing-listens-here.sock"), flag))
		if err == nil {
			t.Errorf("%s was missing and it bound anyway", flag)
			continue
		}
		if !strings.Contains(err.Error(), flag) {
			t.Errorf("%s was missing and the refusal does not say which: %v", flag, err)
		}
		if out.Len() != 0 {
			t.Errorf("%s was missing and something was printed as bound: %s", flag, out.String())
		}
	}
}

// Two harnesses in one space would give a verb two meanings and leave the
// ledger to choose, which is the one thing it must never do. The core refuses,
// and the refusal is printed as it came rather than softened.
//
//	— T11.10
func TestTwoHarnessesCannotTakeOneSpace(t *testing.T) {
	socket := startBoundDaemon(t)
	if err := bindHarness(io.Discard, wakeHarnessArgs(socket)); err != nil {
		t.Fatalf("the first did not bind: %v", err)
	}
	var out bytes.Buffer
	err := bindHarness(&out, []string{
		"--socket", socket,
		"--namespace", "wake",
		"--version", "2",
		"--can", "wake.machine",
		"--unknown", "ignore",
		"--repeat", "duplicates",
		"--retry", "never-retry",
		"--compensate", "compensable",
		"--ending", "close-unknown",
	})
	if err == nil {
		t.Fatal("two harnesses took one space")
	}
	if !strings.Contains(err.Error(), "two harnesses cannot share one space") {
		t.Fatalf("the core's own refusal did not survive: %v", err)
	}
	if !strings.Contains(err.Error(), "wake") {
		t.Fatalf("the refusal does not say which space: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("a refused binding printed as bound: %s", out.String())
	}
}

// A reader can see which meanings are answered for here, by which version, and
// what each said about effects — including whether sending one again is safe,
// which is the arithmetic of two of the eight and not a ninth declaration.
//
//	— T11.10, T10.7
func TestTheListingShowsWhatEachHarnessDeclared(t *testing.T) {
	socket := startBoundDaemon(t)

	var empty bytes.Buffer
	if err := listHarnesses(&empty, []string{"--socket", socket}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty.String(), "no harness is bound") {
		t.Fatalf("nothing is bound and the listing does not say so: %s", empty.String())
	}

	if err := bindHarness(io.Discard, wakeHarnessArgs(socket)); err != nil {
		t.Fatal(err)
	}
	// A second one whose destination duplicates and which never retries on its
	// own. Nothing forbids either combination; the listing reports what each
	// harness declared and calls neither of them more than it is.
	//   — T10.7
	if err := bindHarness(io.Discard, []string{
		"--socket", socket,
		"--namespace", "post",
		"--version", "0.3",
		"--can", "post.send",
		"--unknown", "ignore",
		"--repeat", "duplicates",
		"--retry", "never-retry",
		"--compensate", "compensable",
		"--ending", "close-unknown",
	}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := listHarnesses(&out, []string{"--socket", socket}); err != nil {
		t.Fatal(err)
	}
	for _, said := range []string{
		"2 harnesses", "wake", "wake.machine", "wake.ask", "refuse",
		"idempotent", "may-retry", "irreversible", "ask-a-person",
		"post", "0.3", "post.send", "ignore", "duplicates",
		"never-retry", "compensable", "close-unknown",
	} {
		if !strings.Contains(out.String(), said) {
			t.Errorf("the listing does not show %q:\n%s", said, out.String())
		}
	}
	if !strings.Contains(out.String(), "resending: safe") {
		t.Errorf("a safe resend is not shown as one:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "resending: not safe") {
		t.Errorf("an unsafe resend is not shown as one:\n%s", out.String())
	}
}

// The list of what a harness can do is exhaustive and it is the harness's own.
// A verb outside its space is not its to declare, and the core says so.
//
//	— T11.10, T11.3
func TestAVerbOutsideTheNamespaceIsRefused(t *testing.T) {
	socket := startBoundDaemon(t)
	var out bytes.Buffer
	err := bindHarness(&out, []string{
		"--socket", socket,
		"--namespace", "wake",
		"--version", "1",
		"--can", "wake.machine,sleep.machine",
		"--unknown", "refuse",
		"--repeat", "idempotent",
		"--retry", "may-retry",
		"--compensate", "irreversible",
		"--ending", "ask-a-person",
	})
	if err == nil {
		t.Fatal("a verb outside the harness's own space was declared")
	}
	if !strings.Contains(err.Error(), "a verb must live in the harness's own space") {
		t.Fatalf("the core's own refusal did not survive: %v", err)
	}
	if !strings.Contains(err.Error(), "sleep.machine") {
		t.Fatalf("the refusal does not say which verb: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("a refused binding printed as bound: %s", out.String())
	}
}

// shortTemp is a folder for sockets with a short path, inside the work tree's
// short folder (ROKH_SHORT_TMP, or the work tree's .t); never a literal /tmp.
func shortTemp(t *testing.T, prefix string) (string, error) {
	t.Helper()
	base := os.Getenv("ROKH_SHORT_TMP")
	if base == "" {
		base = filepath.Join("..", "..", "..", "..", "..", "..", "..", ".t")
	}
	if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
		t.Skip("no short socket folder in this work tree")
	}
	return os.MkdirTemp(base, prefix)
}
