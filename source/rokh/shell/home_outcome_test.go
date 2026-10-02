package shell

import (
	"strings"
	"testing"
)

func TestAnUnknownRecordingDoesNotBecomeAWrittenSentence(t *testing.T) {
	hs, _, _, _ := openSession(t, func(op string, req map[string]any) map[string]any {
		if op == "draft.record" {
			return map[string]any{"ok": true, "result": map[string]any{"record": "unknown"}}
		}
		return draftGate("uncertain-draft")(op, req)
	})
	if _, err := say(t, hs, "write at journal/tuesday: preserve my sentence"); err != nil {
		t.Fatal(err)
	}
	reply, err := say(t, hs, "write")
	if err == nil || hs.draft == nil || strings.HasPrefix(reply, "written.") {
		t.Fatalf("unknown recording was completed: reply=%q error=%v draft=%+v", reply, err, hs.draft)
	}
}

func TestHomeLinesKeepAnUnknownEndingAfterLaterSuccess(t *testing.T) {
	g, _ := fakeGate(t, func(op string, req map[string]any) map[string]any {
		if op == "hello" || op == "whoami" {
			return hello()
		}
		if op == "draft.record" {
			return map[string]any{"ok": true, "result": map[string]any{"record": "unknown"}}
		}
		return draftGate("uncertain-draft")(op, req)
	})
	var output, notes strings.Builder
	code := runHome(g.rw, "", strings.NewReader("write at journal/tuesday: keep this\nwrite\nsee the grants\n"), &output, &notes)
	if code != 4 || strings.Contains(output.String(), "written.") || strings.Contains(notes.String(), "left unrecorded") {
		t.Fatalf("unknown was hidden: exit=%d output=%q notes=%q", code, output.String(), notes.String())
	}
}
