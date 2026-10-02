package home

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rokh-home/authority"
)

// A task goes round: the owner gives it as a signed event on the program's
// shelf; the program receives it, takes it with an intent, acts within what it
// holds, and finishes it with an outcome. Every step is an event with the four
// questions kept apart — who composed the task (the owner), who signed the
// receipt (the program's key), under what (the owner's grant), and which door
// recorded it.
//
//	— T4.1, T6, T10, T10.1, T10.2, T10.6, T12.5
func TestATaskGoesRoundAsSignedEvents(t *testing.T) {
	f := newFixture(t)
	worker := f.consumer("worker", Places{}, map[authority.Action]string{
		authority.Ledger: "worker", authority.Read: "notes", authority.Search: "notes", authority.Bytes: "notes",
		authority.Draft: "notes", authority.Record: "notes"})
	before := f.accepted()

	tv, err := f.h.GiveTask(OwnerActor, worker.Consumer, "summarize the synthetic note", map[string]any{"path": "notes/a"})
	if err != nil {
		t.Fatal(err)
	}
	if tv.State != "open" || len(tv.Task) != 64 {
		t.Fatalf("task: %+v", tv)
	}
	if f.accepted() != before+1 {
		t.Fatalf("a task is one recorded event; accepted went %d -> %d", before, f.accepted())
	}

	// The program sees exactly its own shelf; another program sees nothing of it.
	mine, err := f.h.Tasks(worker, "")
	if err != nil || len(mine) != 1 || mine[0].Task != tv.Task || mine[0].State != "open" {
		t.Fatalf("worker's shelf: %v %+v", err, mine)
	}
	other := f.consumer("other", Places{}, map[authority.Action]string{authority.Ledger: "other"})
	if theirs, err := f.h.Tasks(other, ""); err != nil || len(theirs) != 0 {
		t.Fatalf("the other program's shelf is empty: %v %+v", err, theirs)
	}
	if _, err := f.h.Tasks(other, worker.Consumer); err == nil {
		t.Fatal("a program does not read another program's shelf")
	}

	// Taking it is an intent signed by the program's key under its grant,
	// recorded through the program door.
	taken, err := f.h.TakeTask(worker, tv.Task, "")
	if err != nil {
		t.Fatal(err)
	}
	if taken["signer"] != keyName(worker.Consumer, "worker") || taken["authority"] == "" || taken["door"] != ProgramDoor {
		t.Fatalf("taken: %+v", taken)
	}
	if _, err := f.h.TakeTask(worker, tv.Task, ""); err == nil || !strings.Contains(err.Error(), "taken already") {
		t.Fatalf("a task is not taken twice: %v", err)
	}
	if _, err := f.h.TakeTask(other, tv.Task, ""); err == nil {
		t.Fatal("another program cannot take it")
	}
	listed, _ := f.h.Tasks(OwnerActor, worker.Consumer)
	if listed[0].State != "taken" || listed[0].Intent != taken["intent"] || listed[0].Door != ProgramDoor {
		t.Fatalf("owner's view after take: %+v", listed[0])
	}
	if listed[0].Signer != keyName(worker.Consumer, "worker") || listed[0].Authority != taken["authority"] {
		t.Fatalf("the receipt names the program's key and the owner's grant: %+v", listed[0])
	}

	// The program acts within what it holds: a note at notes/…, recorded by
	// its own key. Outside what it holds, the gate refuses as always.
	d, err := f.h.DraftOpen(worker, "notes/summary")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.DraftWrite(worker, d.ID, []byte("summary: synthetic"), d.Revision); err != nil {
		t.Fatal(err)
	}
	res, err := f.h.DraftRecord(worker, d.ID)
	if err != nil || res.Record != "recorded" {
		t.Fatalf("record: %v %+v", err, res)
	}
	if _, err := f.h.DraftOpen(worker, "private/x"); err == nil {
		t.Fatal("a task does not widen what the program holds")
	}

	// Finishing without the outcome word is refused; done closes it.
	if _, err := f.h.FinishTask(worker, tv.Task, "", "maybe", "", "", "", ""); err == nil {
		t.Fatal("an outcome is done, failed or unknown")
	}
	fin, err := f.h.FinishTask(worker, tv.Task, "", "done", "wrote notes/summary v1", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if fin["record"] != "recorded" || fin["intent"] != taken["intent"] {
		t.Fatalf("finished: %+v", fin)
	}
	if _, err := f.h.FinishTask(worker, tv.Task, "", "done", "again", "", "", ""); err == nil || !strings.Contains(err.Error(), "closed already") {
		t.Fatalf("a closed task is not finished twice: %v", err)
	}
	final, _ := f.h.Tasks(OwnerActor, worker.Consumer)
	if final[0].State != "done" || final[0].Saying != "wrote notes/summary v1" || final[0].Outcome == "" {
		t.Fatalf("owner's view at the end: %+v", final[0])
	}
	// One task, one record, one intent, one outcome, plus what the grants and
	// the two consumers' keys added: the ledger holds each as its own event.
	rows := f.h.OwnerLedger(map[string]any{"op": "log", "address": "worker"})["events"].([]map[string]any)
	if len(rows) != 2 || rows[0]["verb"] != "intent" || rows[1]["verb"] != "outcome" {
		t.Fatalf("the receipt on the shelf of worker: %v", rows)
	}
	for _, row := range rows {
		if row["key"] != keyName(worker.Consumer, "worker") || row["door"] != ProgramDoor || row["authority"] == nil {
			t.Fatalf("each half names key, door and authority: %v", row)
		}
	}
	shelf := f.h.OwnerLedger(map[string]any{"op": "log", "address": TaskShelf(worker.Consumer)})["events"].([]map[string]any)
	if len(shelf) != 1 || shelf[0]["key"] != "root" || shelf[0]["door"] != OwnerDoor || shelf[0]["authority"] != nil {
		t.Fatalf("the task is the owner's word through the owner's door: %v", shelf)
	}
}

// A program without a namespace can leave no receipt, so it is given no task;
// a task that was never taken cannot be finished; a revoked program's shelf is
// closed to it.
//
//	— T10.1, T6.3
func TestTasksRefuseWhatCannotBeAnswered(t *testing.T) {
	f := newFixture(t)
	mute := f.consumer("mute", Places{}, map[authority.Action]string{authority.Read: "notes"})
	_, err := f.h.GiveTask(OwnerActor, mute.Consumer, "anything", nil)
	denied(t, err, "no_namespace")
	if _, err := f.h.GiveTask(mute, mute.Consumer, "anything", nil); err == nil {
		t.Fatal("only the owner gives tasks")
	}
	worker := f.consumer("worker", Places{}, map[authority.Action]string{authority.Ledger: "worker"})
	tv, err := f.h.GiveTask(OwnerActor, worker.Consumer, "do a thing", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.FinishTask(worker, tv.Task, "", "done", "", "", "", ""); err == nil || !strings.Contains(err.Error(), "never taken") {
		t.Fatalf("finishing what was never taken: %v", err)
	}
	if _, err := f.h.GiveTask(OwnerActor, worker.Consumer, "   ", nil); err == nil {
		t.Fatal("a task says what is to be done")
	}
	if err := f.h.RevokeConsumer(OwnerActor, worker.Consumer); err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.Tasks(worker, ""); err == nil {
		t.Fatal("a revoked program's shelf is closed to it")
	}
	if _, err := f.h.TakeTask(worker, tv.Task, ""); err == nil {
		t.Fatal("a revoked program takes nothing")
	}
}

// A program receives work by waiting on its shelf, not by asking again and
// again; the wait lets go of the home, so the owner can record meanwhile.
//
//	— T4.1, T10.6
func TestAProgramWaitsOnItsShelfWhileTheHomeGoesOn(t *testing.T) {
	f := newFixture(t)
	worker := f.consumer("worker", Places{}, map[authority.Action]string{authority.Ledger: "worker"})
	got := make(chan map[string]any, 1)
	go func() {
		r, err := f.h.WaitTasks(worker, "", 5)
		if err != nil {
			r = map[string]any{"error": err.Error()}
		}
		got <- r
	}()
	time.Sleep(150 * time.Millisecond)
	if _, err := f.h.GiveTask(OwnerActor, worker.Consumer, "first", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-got:
		tasks, _ := r["tasks"].([]TaskView)
		if len(tasks) != 1 || tasks[0].Doing != "first" || r["timed_out"] == true {
			t.Fatalf("waited: %+v", r)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("the wait did not wake")
	}
	r, err := f.h.WaitTasks(worker, fmt.Sprint(f.h.OwnerLedger(map[string]any{"op": "log", "address": TaskShelf(worker.Consumer)})["last"]), 1)
	if err != nil || r["timed_out"] != true {
		t.Fatalf("nothing more arrives: %v %+v", err, r)
	}
}
