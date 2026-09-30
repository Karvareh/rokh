package passphrase

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// One rule for every command (contract section 6): --passphrase-file ("-" is
// standard input), then ROKH_PASSPHRASE_FILE, then ROKH_PASSPHRASE, then the
// terminal.
func TestThePassphraseRule(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	flagFile := write("flag", "from the flag\n")
	envFile := write("env", "from the env file\r\n")
	empty := write("empty", "\n")
	long := write("long", strings.Repeat("x", 4097))
	cases := []struct {
		name, file, stdin, envFile, env, want, err string
	}{
		{"the flag wins over everything", flagFile, "", envFile, "from env", "from the flag", ""},
		{"standard input by -", "-", "from stdin\n", envFile, "from env", "from stdin", ""},
		{"the env file next", "", "", envFile, "from env", "from the env file", ""},
		{"the env variable next", "", "", "", "from env", "from env", ""},
		{"an empty file is refused", empty, "", "", "", "", "empty"},
		{"an empty stdin is refused", "-", "", "", "", "", "empty"},
		{"a long file is refused, not cut", long, "", "", "", "", "exceeds"},
		{"a missing file is an error", filepath.Join(dir, "none"), "", "", "", "", "no such file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("ROKH_PASSPHRASE_FILE", c.envFile)
			t.Setenv("ROKH_PASSPHRASE", c.env)
			got, err := From(c.file, strings.NewReader(c.stdin))
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("want an error with %q, got %q %v", c.err, got, err)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got %q %v, want %q", got, err, c.want)
			}
		})
	}
}
