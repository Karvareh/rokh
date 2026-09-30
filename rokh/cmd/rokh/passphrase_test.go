package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Every subcommand of rokh that opens a carrier reads its passphrase through
// the one rule: each is run here with --passphrase-file.
func TestEveryCommandTakesThePassphraseFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pass")
	if err := os.WriteFile(p, []byte("synthetic passphrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ROKH_PASSPHRASE_FILE", "")
	t.Setenv("ROKH_PASSPHRASE", "")
	for _, name := range []string{"init", "write", "grant", "revoke", "merge", "log", "verify", "branch",
		"keys", "share", "unshare", "covenants", "announce", "daemon", "attempt", "view", "key list",
		"bond leaf", "bond accept", "bond standing"} {
		fs, _, err := newFlags(name, []string{"DIR", "--passphrase-file", p})
		if err != nil {
			t.Fatal(err)
		}
		if err := fs.Parse([]string{"--passphrase-file", p}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := passphrase(fs)
		if err != nil || got != "synthetic passphrase" {
			t.Fatalf("%s: %q %v", name, got, err)
		}
	}
}
