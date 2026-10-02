package home

import (
	"fmt"
	"sync"
	"testing"

	"rokh-home/authority"
)

// Between the home's two doors (T10's "Seen, not done", given to T2): the
// status a program is answered names under branches_not_accepted only a
// branch whose head its view holds as not accepted, never a branch another
// door recorded a moment ago. Before T2 the home's doors read one opening of
// the vessel, and one door's recording moved the references under the other
// door's reading: for one answer, status named the new head as not accepted,
// though nothing was wrong with it. Now each door, and each program's own
// view, reads an opening of its own, and a status is of one reading.
func TestWhileTheOwnerRecordsAProgramsStatusNamesNoBranchAsNotAccepted(t *testing.T) {
	f := newFixture(t)
	worker := f.consumer("worker", Places{}, map[authority.Action]string{authority.Ledger: "worker"})
	const writes = 120
	stop := make(chan struct{})
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		seen  []string
		asked int
	)
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				st := f.h.LedgerOp(worker, map[string]any{"op": "status"})
				mu.Lock()
				asked++
				if st["ok"] != true {
					seen = append(seen, fmt.Sprintf("refused: %v", st))
				} else if nb, ok := st["branches_not_accepted"]; ok {
					seen = append(seen, fmt.Sprintf("%v", nb))
				}
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < writes; i++ {
		r := f.h.OwnerLedger(map[string]any{"op": "write", "address": "worker/notes", "verb": "note",
			"message": fmt.Sprintf("a synthetic line %d, recorded by the owner", i)})
		if r["record"] != "recorded" {
			close(stop)
			wg.Wait()
			t.Fatalf("the owner's write %d: %v", i, r)
		}
	}
	close(stop)
	wg.Wait()
	if len(seen) > 0 {
		t.Errorf("%d of %d statuses the program was answered while the owner recorded named a branch as not accepted; the first: %s",
			len(seen), asked, seen[0])
	}
	t.Logf("%d statuses asked by the program during %d recordings by the owner", asked, writes)
}
