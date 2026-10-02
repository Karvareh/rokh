package home

import (
	"os"
	"testing"
	"time"
)

// Closing while a real streaming import is writing must join that work before
// zeroing the store, then leave a journal another broker can resume exactly once.
func TestCloseJoinsAnImportAndTheNextBrokerResumesIt(t *testing.T) {
	f := newFixture(t)
	source := f.src + "/large-synthetic.bin"
	file, err := os.OpenFile(source, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 1<<20)
	for i := range chunk {
		chunk[i] = byte(i % 251)
	}
	for i := 0; i < 64; i++ {
		if _, err := file.Write(chunk); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, id, err := f.h.ImportPreview(OwnerActor, source, "project/large")
	if err != nil {
		t.Fatal(err)
	}
	type ending struct {
		result RecordResult
		err    error
	}
	finished := make(chan ending, 1)
	go func() { r, e := f.h.ImportRun(OwnerActor, id); finished <- ending{r, e} }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.h.jobsMu.Lock()
		active := len(f.h.jobs) > 0
		f.h.jobsMu.Unlock()
		if active {
			break
		}
		select {
		case e := <-finished:
			t.Fatalf("copy ended before closure was exercised: %+v %v", e.result, e.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("import never became active")
		}
		time.Sleep(100 * time.Microsecond)
	}
	f.h.Close()
	e := <-finished
	if e.err == nil || e.result.Record == "recorded" {
		t.Fatalf("closing import claimed a commit: %+v %v", e.result, e.err)
	}
	h, err := Open(f.root, pass, "resume-test")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	r, err := h.ImportRun(OwnerActor, id)
	if err != nil || r.Record != "recorded" || r.Version != 1 {
		t.Fatalf("resume: %+v %v", r, err)
	}
	again, err := h.ImportRun(OwnerActor, id)
	if err != nil || again.Event != r.Event {
		t.Fatalf("resume duplicated the commit: %+v %v", again, err)
	}
}
