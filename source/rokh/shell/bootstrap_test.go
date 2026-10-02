package shell

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Step 6, through real processes: a ledger the sentence surface makes
// records what `rokh init` records, the owner's keyring generation and the
// system reader, so a key the owner adds there opens its own view with its
// own passphrase. Before T2 the surface recorded the genesis alone, and the
// key's passphrase opened nothing: "this key's generation gives it no system
// reader in this carrier" (E2 survey, runs e2-nontty-3 and e2-nontty-4).
func TestALedgerTheSurfaceMakesTakesAKeyThatOpensItsOwnView(t *testing.T) {
	bin := buildRokhCommand(t)
	base := t.TempDir()
	vault := filepath.Join(base, "vault")
	if err := os.Mkdir(vault, 0o700); err != nil {
		t.Fatal(err)
	}
	passFile := filepath.Join(base, "owner.pass")
	if err := os.WriteFile(passFile, []byte("synthetic owner passphrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keyPass := filepath.Join(base, "reader.pass")
	if err := os.WriteFile(keyPass, []byte("synthetic reader passphrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(vault, LedgersFolder, "main")
	// surface runs the sentence surface on the vault with a passphrase file,
	// its sentences on standard input.
	surface := func(pass string, sentences ...string) (string, string) {
		t.Helper()
		cmd := exec.Command(bin, vault)
		cmd.Env = append(os.Environ(), "ROKH_PASSPHRASE_FILE="+pass)
		cmd.Stdin = strings.NewReader(strings.Join(sentences, "\n") + "\n")
		var out, errs bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errs
		if err := cmd.Run(); err != nil {
			t.Fatalf("the surface, %q: %v\n%s%s", sentences, err, out.String(), errs.String())
		}
		return out.String(), errs.String()
	}
	commandLine := func(pass string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, append(args, "--passphrase-file", pass)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("rokh %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	bodies := func(out string) map[string]string {
		got := map[string]string{}
		for _, line := range strings.Split(out, "\n") {
			var row map[string]any
			if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &row) != nil {
				continue
			}
			p, _ := base64.StdEncoding.DecodeString(fmt.Sprint(row["payload"]))
			got[fmt.Sprint(row["address"])+" "+fmt.Sprint(row["verb"])] = string(p)
		}
		return got
	}

	surface(passFile, "open a new ledger named main", "write at journal/before: written before the key was added", "write")

	// The first page is `rokh init`'s: the genesis, then the owner's keyring
	// generation, and the owner is listed as a key.
	first := bodies(commandLine(passFile, "log", dir, "--json"))
	if _, ok := first["rokh rokh.genesis"]; !ok {
		t.Fatalf("the surface's ledger holds no genesis: %v", first)
	}
	if _, ok := first["rokh rokh.keyring"]; !ok {
		t.Fatalf("the surface's ledger records no owner keyring generation: %v", first)
	}
	if list := commandLine(passFile, "key", "list", dir); !strings.Contains(list, "owner") || !strings.Contains(list, "gen 1") {
		t.Fatalf("the surface's ledger lists no owner generation:\n%s", list)
	}

	// A key the owner adds there, and a line written after it.
	commandLine(passFile, "key", "add", dir, "--name", "reader", "--reads", "journal", "--key-passphrase-file", keyPass)
	surface(passFile, "write at journal/after: written after the key was added", "write",
		"write at private/after: outside the key's reads", "write")

	// The key's own passphrase opens its own view: on the command line,
	got := bodies(commandLine(keyPass, "log", dir, "--json"))
	if got["journal/after note"] != "written after the key was added" {
		t.Errorf("the key's passphrase does not read what was sealed to it: %v", got)
	}
	for addr, body := range got {
		if strings.Contains(body, "outside the key's reads") || strings.Contains(body, "written before the key was added") {
			t.Errorf("the key's view shows %s, which its reader does not open", addr)
		}
	}
	// and on the sentence surface.
	said, _ := surface(keyPass, "read journal")
	if !strings.Contains(said, "written after the key was added") || strings.Contains(said, "written before the key was added") {
		t.Errorf("the surface with the key's passphrase:\n%s", said)
	}
}
