package lineage

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"rokh/carrier"
	"rokh/frame"
	"rokh/vessel"
)

// Step 4 (R5): the answer names both halves, and a half that was not
// recorded is given no count of records added, since it added none; what was
// recorded is counted.
func TestAHalfNotRecordedIsGivenNoCount(t *testing.T) {
	a, _ := newSide(t, 1, nil, true)
	b, _ := newSide(t, 2, nil, false)
	b.Own = &holder{no: true}
	res, err := Reconcile(a, b)
	if err == nil || res.OK() || res.ExitCode() != 3 {
		t.Fatalf("a failed second half: %v %s", err, res)
	}
	s := res.String()
	if !strings.Contains(s, "local recorded (+0, 0 withheld)") || !strings.Contains(s, "remote not-recorded: ") || strings.Contains(s, "remote not-recorded (+") {
		t.Fatalf("the halves as named: %q", s)
	}
	for _, c := range []struct {
		h    Half
		want string
	}{
		{Half{Outcome: vessel.Recorded, Added: 3, Held: 1}, "recorded (+3, 1 withheld)"},
		{Half{Outcome: vessel.NotRecorded, Added: 3}, "not-recorded"},
		{Half{Outcome: vessel.NotRecorded, Added: 3, Held: 2, Err: errors.New("synthetic")}, "not-recorded (2 withheld): synthetic"},
		{Half{Outcome: vessel.Unknown, Added: 3, Err: errors.New("synthetic")}, "unknown: synthetic"},
	} {
		got := Result{Local: Half{Outcome: vessel.Recorded}, Remote: c.h}.String()
		if want := "local recorded (+0, 0 withheld); remote " + c.want; got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
}

// flaky is a medium that, once armed, does not answer the reading back of
// the next head file written: the commit's ending cannot be known (contract
// 2.6, step 6). Every other call is the memory's own.
type flaky struct {
	*vessel.Memory
	armed   bool
	written string
}

func (f *flaky) Write(name string, b []byte) error {
	err := f.Memory.Write(name, b)
	if n := strings.ToLower(name); f.armed && strings.HasPrefix(n, vessel.Dir+"/head") {
		f.written = n
	}
	return err
}

func (f *flaky) Read(name string, max int) ([]byte, error) {
	if n := strings.ToLower(name); f.written != "" && n == f.written {
		f.written = ""
		return nil, errors.New("synthetic: the medium did not answer")
	}
	return f.Memory.Read(name, max)
}

// Step 4 (R5, B5): a second half whose ending is unknown is exit 4,
// neither success nor a clean failure; the first half is recorded and named,
// and the unknown half is given no count.
func TestASecondHalfWhoseEndingIsUnknownIsExitFour(t *testing.T) {
	a, _ := newSide(t, 1, nil, true)
	write(t, a, 1, "home/a", "one")
	f := &flaky{Memory: vessel.NewMemory()}
	c, err := carrier.Create(f, vessel.Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: stream(2)}, bytes.Repeat([]byte{2}, 32), nil,
		anchor, frame.Zero, nil, kSealer{k: sharedK, rnd: stream(102)})
	if err != nil {
		t.Fatal(err)
	}
	b := Side{C: c, Own: &holder{}, Unjudged: true}
	f.armed = true
	res, err := Reconcile(a, b)
	if err == nil || res.OK() || res.ExitCode() != 4 {
		t.Fatalf("a second half whose ending is unknown: %v %s, exit %d", err, res, res.ExitCode())
	}
	if res.Local.Outcome != vessel.Recorded || res.Remote.Outcome != vessel.Unknown {
		t.Fatalf("the halves are not named: %s", res)
	}
	if s := res.String(); !strings.Contains(s, "local recorded (+") || !strings.Contains(s, "remote unknown: ") || strings.Contains(s, "remote unknown (+") {
		t.Fatalf("the halves as named: %q", s)
	}
}
