package vessel

import (
	"sync"
	"testing"
)

// Door readers scan and read bodies while the writer commits.
// Under -race this reproduced a race on the index, the slab cache and the
// state a commit replaces. Readers run the whole time; nothing is disabled
// and nobody waits on anybody but the lock's own short hold.
func TestReadersAndOneWriterShareAVessel(t *testing.T) {
	m := NewMemory()
	v := newVessel(t, m, 60)
	own := &holder{}
	mustRecord(t, v, own, 0)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = v.Scan(func(h Header, ref Ref) error {
					_, _ = v.Body(ref) // a stale reference after a commit is an answer, not a race
					return nil
				})
				_ = v.Info()
				_ = v.Report()
				_ = v.Slots()
			}
		}()
	}
	for i := 1; i <= 40; i++ {
		mustRecord(t, v, own, i)
	}
	close(stop)
	wg.Wait()
	ev, ptr := contents(t, v)
	if len(ev) != 41 || ptr != "main -> 40" {
		t.Fatalf("after concurrent reading: %d events, %q", len(ev), ptr)
	}
}
