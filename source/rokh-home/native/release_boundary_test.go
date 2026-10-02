package native

import (
	"errors"
	"math"
	"testing"
)

func validHashArray() []any {
	b := make([]any, 39)
	for i := range b {
		b[i] = float64(i)
	}
	return b
}

func TestReleaseRejectsMalformedNativeHeads(t *testing.T) {
	check := func(seq any, action any) bool {
		_, _, _, ok := readHead(map[string]any{"result": map[string]any{"seq": seq, "action": action}})
		return ok
	}
	for _, seq := range []float64{0, math.MaxUint32} {
		if !check(seq, validHashArray()) {
			t.Fatalf("valid head refused: %v", seq)
		}
	}
	for _, seq := range []float64{-1, 0.5, math.MaxUint32 + 1, math.NaN(), math.Inf(1)} {
		if check(seq, validHashArray()) {
			t.Errorf("malformed sequence accepted: %v", seq)
		}
	}
	for _, n := range []int{0, 1, 38, 40} {
		b := make([]any, n)
		for i := range b {
			b[i] = float64(0)
		}
		if check(float64(1), b) {
			t.Errorf("malformed hash length accepted: %d", n)
		}
	}
	for _, value := range []any{-1.0, 256.0, 0.5, math.NaN(), math.Inf(1), "1"} {
		b := validHashArray()
		b[3] = value
		if check(float64(1), b) {
			t.Errorf("malformed hash byte accepted: %v", value)
		}
	}
}

func TestReleaseQuiesceCannotHideUnreadableState(t *testing.T) {
	for _, kind := range []string{"index", "profile"} {
		t.Run(kind, func(t *testing.T) {
			m := manager(t)
			p := newProfile(t, m)
			name := ptrIndex
			if kind == "profile" {
				name = ptrProfile + p.ID
			}
			if err := m.seal.SetPointer(name, []byte("{broken")); err != nil {
				t.Fatal(err)
			}
			r := m.Quiesce("unreadable-state")
			if r["ok"] != false || r["nothing_to_put_away"] == true {
				t.Fatalf("unreadable state was reported as an empty successful close: %v", r)
			}
			if m.closing {
				t.Fatal("failed close must leave the manager available for repair")
			}
		})
	}
}

func TestReleaseStopMustReportProfileSaveFailure(t *testing.T) {
	m, h := hookedManager(t)
	p := newProfile(t, m)
	h.pointerErr = errors.New("synthetic: cannot persist stopped state")
	if _, err := m.Stop(p.ID); err == nil {
		t.Fatal("stop reported success after its profile save failed")
	}
}

func TestReleaseCloseAllReportsUnreadableLiveProfile(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	s := liveSession(t, m, p, headBridge(t, 1))
	if err := m.seal.SetPointer(ptrProfile+p.ID, []byte("{broken")); err != nil {
		t.Fatal(err)
	}
	defer func() {
		delete(m.live, p.ID)
		if r := recover(); r != nil {
			t.Errorf("unreadable profile caused a close panic: %v", r)
		}
	}()
	r := m.CloseAll()
	if len(r) != 1 || r[0]["error"] == nil {
		t.Fatalf("missing close failure: %v", r)
	}
	if processAlive(s.cmd.Process.Pid) || len(m.live) != 0 {
		t.Fatal("last-resort close left its own process running after the profile became unreadable")
	}
}
