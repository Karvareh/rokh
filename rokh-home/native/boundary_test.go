package native

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBridge answers every request line with the same answer line, echoing the
// id the request carried. It is a stand-in for the bridge process and for
// nothing else: there is no conductor behind it, which is the point — what is
// under test is how this package reads an answer, not what a conductor would
// have said.
//
// The id is echoed because the real protocol requires it: an answer that does
// not name the request it belongs to ends the line, which is its own test
// below.
func fakeBridge(t *testing.T, answer string) *bridge {
	t.Helper()
	return scriptedBridge(t, func(id float64) string {
		var m map[string]any
		if err := json.Unmarshal([]byte(answer), &m); err != nil {
			return answer
		}
		m["id"] = id
		out, _ := json.Marshal(m)
		return string(out)
	})
}

// scriptedBridge lets a test decide, per request, what comes back.
func scriptedBridge(t *testing.T, reply func(id float64) string) *bridge {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		defer outW.Close()
		r := bufio.NewReader(inR)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			var req map[string]any
			_ = json.Unmarshal([]byte(line), &req)
			id, _ := req["id"].(float64)
			if _, err := outW.Write([]byte(reply(id) + "\n")); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { inW.Close(); inR.Close(); outR.Close() })
	return &bridge{in: inW, out: bufio.NewReaderSize(outR, 1<<20)}
}

// An answer that names another request, or names none, is not this call's
// answer. Taking it would attribute a result to the wrong request, which is
// worse than having no result: the caller would act on it.
func TestAnAnswerToAnotherRequestEndsTheLine(t *testing.T) {
	for _, name := range []string{"another id", "no id"} {
		m := manager(t)
		p := newProfile(t, m)
		m.live[p.ID] = &session{bridge: scriptedBridge(t, func(id float64) string {
			if name == "no id" {
				return `{"ok":true,"result":{}}`
			}
			return `{"ok":true,"id":999,"result":{}}`
		})}
		res, err := m.Publish(PublishInput{Profile: p.ID, Attempt: "a", ItemID: "i", Version: "v", Title: "t"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Record != "unknown" {
			t.Fatalf("%s: want unknown, got %+v", name, res)
		}
		if !strings.Contains(res.Error, "line is closed") {
			t.Fatalf("%s: the line should have been closed: %q", name, res.Error)
		}
	}
}

// A request that never left this process is the one case where the chain is
// certainly untouched, and the only one reported that way.
func TestARequestThatNeverLeftIsTheOnlyNotRecorded(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	b := fakeBridge(t, `{"ok":true,"result":{}}`)
	b.dead = true
	m.live[p.ID] = &session{bridge: b}
	res, err := m.Publish(PublishInput{Profile: p.ID, Attempt: "a", ItemID: "i", Version: "v", Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Record != "not-recorded" {
		t.Fatalf("want not-recorded, got %+v", res)
	}
}

func publishThrough(t *testing.T, answer string) PublishResult {
	t.Helper()
	m := manager(t)
	p := newProfile(t, m)
	m.live[p.ID] = &session{bridge: fakeBridge(t, answer)}
	res, err := m.Publish(PublishInput{Profile: p.ID, Attempt: "native:publish:synthetic",
		ItemID: "item", Version: "v1", Title: "title"})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// A refusal that crossed the pipe is not evidence that the chain is untouched.
// Two of the zome's own refusals mean the opposite — the attempt is already on
// the chain — so reporting "not-recorded" for them would put a false ending in
// the home's ledger, where the gate writes this result as the receipt's outcome.
func TestARefusalThatCrossedThePipeIsNotReportedAsNothingWritten(t *testing.T) {
	for _, answer := range []string{
		`{"ok":false,"code":"zome_error","error":"attempt_conflict: attempt 'native:publish:synthetic' already recorded different content at uhCkkSYNTHETIC"}`,
		`{"ok":false,"code":"zome_error","error":"attempt_incomplete: private body not found"}`,
		`{"ok":false,"code":"app_error","error":"the conductor refused"}`,
	} {
		res := publishThrough(t, answer)
		if res.Record == "not-recorded" {
			t.Fatalf("a refusal that crossed the pipe was reported as nothing written: %+v", res)
		}
		if res.Record != "unknown" {
			t.Fatalf("want unknown, got %+v", res)
		}
		if res.Code == "" {
			t.Fatalf("the conductor's own code should survive into the result: %+v", res)
		}
	}
}

// A refusal's text goes two ways the bridge knows nothing about: back to the
// program, and into the home's ledger as the receipt's saying, whose payload is
// capped. It is kept to a size both can carry.
func TestARefusalsTextIsKeptToASizeAReceiptCanCarry(t *testing.T) {
	long := strings.Repeat("the private body was quoted back ", 400)
	res := publishThrough(t, `{"ok":false,"code":"app_error","error":"`+long+`"}`)
	if len(res.Error) > 600 {
		t.Fatalf("a refusal of %d bytes reached the result", len(res.Error))
	}
	if !strings.HasSuffix(res.Error, "(cut)") {
		t.Fatalf("a cut refusal should say it was cut: %q", res.Error)
	}
	if res.Code != "app_error" {
		t.Fatalf("the code an operator reads was lost: %+v", res)
	}
}

// Nothing left this process, so nothing can be on the chain.
func TestAPublicationThatWasNeverSentIsNotRecorded(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	res, err := m.Publish(PublishInput{Profile: p.ID, Attempt: "native:publish:synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Record != "not-recorded" {
		t.Fatalf("a call with no bridge at all is not-recorded, got %+v", res)
	}
}

// An answer shaped like a success but naming no action is not a success.
func TestAnAnswerThatNamesNoActionIsNotCalledRecorded(t *testing.T) {
	res := publishThrough(t, `{"ok":true,"result":{"existing":false}}`)
	if res.Record == "recorded" {
		t.Fatalf("an answer naming no manuscript was called recorded: %+v", res)
	}
	if res.Record != "unknown" {
		t.Fatalf("want unknown, got %+v", res)
	}
}

// The keystore's configuration names the folder it was in, and is rewritten.
// The keystore's store beside it is binary; a string replacement there changes
// its length and leaves a keystore that only fails later, at the next start.
func TestRestoreRewritesTheKeystoreConfigurationAndNotItsStore(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	source := t.TempDir()
	// A store with the runtime path inside it, between bytes that are not text.
	store := append([]byte{0x00, 0x01, 0xff, 0xfe}, []byte("storeFile="+source+"/ks/store")...)
	store = append(store, 0x00, 0x7f, 0xfe)
	for name, body := range map[string][]byte{
		"ks/lair-keystore-config.yaml": []byte("connectionUrl: unix://" + source + "/ks/socket\n"),
		"ks/store":                     store,
		"data/conductor.sqlite3":       []byte("SQLite format 3\x00 synthetic body bytes"),
	} {
		full := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeStamp(source, stamp{Format: RuntimeFormat, Profile: p.ID, Generation: p.Generation,
		AppID: p.AppID, WrittenUTC: nowUTC()}); err != nil {
		t.Fatal(err)
	}
	p.RuntimeDir, p.Quiesced, p.HeadRead = source, true, true
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Checkpoint(p.ID, "attempt-1"); err != nil {
		t.Fatal(err)
	}
	// A fresh folder whose name is a different length from the retired one, so a
	// replacement inside a binary file would be visible as a changed size.
	fresh := filepath.Join(t.TempDir(), "root-with-a-longer-name-than-before")
	out, err := m.Restore(p.ID, fresh)
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(fresh, "ks/lair-keystore-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), source) {
		t.Fatal("the keystore configuration still names the retired folder")
	}
	if !strings.Contains(string(config), fresh) {
		t.Fatal("the keystore configuration does not name the folder it was restored into")
	}
	restored, err := os.ReadFile(filepath.Join(fresh, "ks/store"))
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != len(store) || string(restored) != string(store) {
		t.Fatalf("the keystore's store was rewritten: %d bytes sealed, %d restored", len(store), len(restored))
	}
	// What still names the retired folder is reported, so it is seen before the
	// conductor is started rather than after it fails.
	still, _ := out["paths_still_named"].([]string)
	found := false
	for _, s := range still {
		if s == "ks/store" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the answer does not say that ks/store still names the retired folder: %v", out)
	}
}
