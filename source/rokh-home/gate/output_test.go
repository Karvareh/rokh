package gate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A permitted program's stdout can contain home content. Capturing it for the
// owner must not create an unencrypted persistent copy alongside the socket.
func TestProgramOutputDoesNotLeaveAPlaintextRunLog(t *testing.T) {
	f := newGate(t)
	a, _ := f.program("output-test", map[string]string{"read": "a"}, nil)
	marker := "SYNTHETIC-PRIVATE-OUTPUT-7f9894f4"
	r, err := f.s.run(RunSpec{Consumer: a, Argv: []string{"/bin/cat"}, Dir: t.TempDir(), Wait: true, Stdin: marker})
	if err != nil || r["exit"] != 0 || r["stdout"] != marker {
		t.Fatalf("run: %+v %v", r, err)
	}
	err = filepath.WalkDir(f.s.runDir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if bytes.Contains(b, []byte(marker)) {
			t.Errorf("private output persisted without encryption: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProgramOutputIsBoundedAndReportsTruncation(t *testing.T) {
	f := newGate(t)
	a, _ := f.program("large-output", nil, nil)
	body := strings.Repeat("original-private-bytes;", 10000) + "END"
	r, err := f.s.run(RunSpec{Consumer: a, Argv: []string{"/bin/cat"}, Dir: t.TempDir(), Wait: true, Stdin: body})
	if err != nil || r["exit"] != 0 || r["stdout"] != body[len(body)-outputLimit:] || r["stdout_truncated"] != true {
		t.Fatalf("bounded output failed: exit=%v truncated=%v err=%v", r["exit"], r["stdout_truncated"], err)
	}
	if r["stderr"] != "" || r["stderr_truncated"] != false {
		t.Fatalf("empty stderr: %+v", r)
	}
	if _, exists := r["stdout_log"]; exists {
		t.Fatal("a plaintext log path was advertised")
	}
}
