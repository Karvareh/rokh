package home

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sync"
	"testing"
	"time"

	"rokh-home/authority"
	"rokh/frame"
)

// The home's own part. The program's door names the key of every row
// it answers, reading the keyring through Key and Names, and it does so while
// a program waits on its shelf with the home's lock let go. The owner may
// meanwhile register a program, which publishes a new map of held keys. Run
// with -race: this one touches no vessel, so it is the keyring alone.
func TestTheKeyringIsReadWhileTheOwnerPublishesANewOne(t *testing.T) {
	k := &keyring{Keys: map[string]heldKey{}}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	grant := frame.ID{1}.String()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, n := range k.Names() {
				if p, g, ok := k.Key(n); !ok || len(p) != ed25519.PrivateKeySize || g == nil {
					t.Errorf("%s was named and then not held whole", n)
					return
				}
			}
		}
	}()
	const programs = 200
	for i := 0; i < programs; i++ {
		// As persistHeldKey does under the home's lock: copy, add, publish.
		next := make(map[string]heldKey, len(k.Keys)+1)
		for n, v := range k.Keys {
			next[n] = v
		}
		next[fmt.Sprint("program-", i)] = heldKey{Priv: append([]byte(nil), priv...), Grant: grant}
		k.publish(next)
	}
	close(stop)
	wg.Wait()
	if n := len(k.Names()); n != programs {
		t.Fatalf("%d keys held, not %d", n, programs)
	}
	k.wipe()
	for _, n := range k.Names() {
		if p, _, _ := k.Key(n); !p.Equal(ed25519.PrivateKey(make([]byte, ed25519.PrivateKeySize))) {
			t.Fatalf("%s was not wiped", n)
		}
	}
}

// The same, through the home: a program waits on its shelf while the owner
// registers another program, whose key the home mints and publishes, and then
// gives the first one a task. The wait wakes with it.
func TestAProgramWaitsWhileTheOwnerRegistersAnother(t *testing.T) {
	f := newFixture(t)
	worker := f.consumer("worker", Places{}, map[authority.Action]string{authority.Ledger: "worker"})
	got := make(chan map[string]any, 1)
	go func() {
		r, err := f.h.WaitTasks(worker, "", 10)
		if err != nil {
			r = map[string]any{"error": err.Error()}
		}
		got <- r
	}()
	time.Sleep(150 * time.Millisecond)
	f.consumer("helper", Places{}, map[authority.Action]string{authority.Ledger: "helper"})
	time.Sleep(700 * time.Millisecond) // at least one look at the shelf after the new key
	if _, err := f.h.GiveTask(OwnerActor, worker.Consumer, "after the helper came", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-got:
		tasks, _ := r["tasks"].([]TaskView)
		if len(tasks) != 1 || tasks[0].Doing != "after the helper came" || r["timed_out"] == true {
			t.Fatalf("waited: %+v", r)
		}
	case <-time.After(9 * time.Second):
		t.Fatal("the wait did not wake")
	}
}
