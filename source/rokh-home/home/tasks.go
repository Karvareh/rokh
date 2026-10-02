package home

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"rokh-home/authority"
	"rokh/daemon"
	"rokh/event"
	"rokh/receipt"
)

// A task is how the owner hands a unit of work to a program, and it is an
// event: the owner's own word, signed with the root key, on a shelf the home
// keeps for that program — home/tasks/<program>. The program reads its shelf
// through its own aperture (T10.6), and everything it does about a task is
// the receipt ritual it already keeps (T10): an intent that names the task as
// its origin before the step, an outcome after it. The task grants nothing —
// what the program may do is what the owner entrusted to it before, and the
// gate decides every act against that, task or no task. A task asking for
// more than was entrusted ends as an outcome that says so.
//
// So the four questions stay apart in the data: the task's author is the
// owner (root key, no authority field); the intent and the outcome are
// signed by the program's own key under the grant the owner made; the door
// that recorded each is in its attestation; and who composed the words the
// program acted on is in the task's payload, which the owner wrote.
//
//	— T4.1, T6, T10, T10.1, T10.2, T10.6, T11.11, T12.5

const (
	// TaskVerb is the verb of a task event on a program's shelf.
	TaskVerb = "task"
	// TaskFormat names the payload's shape.
	TaskFormat = "rokh-home.task/1"
	// taskOrigin is how a receipt names the task it answers: the witness of
	// origin is "task:" and the task's own name.
	taskOrigin = "task:"
	// maxTaskInput bounds what a task may carry inline; heavier input goes
	// through the home as an item and the task names its path.
	maxTaskInput = 2048
)

// taskPayload is what the task event carries.
type taskPayload struct {
	Type     string         `json:"type"`
	Consumer string         `json:"consumer"`
	Doing    string         `json:"doing"`
	Input    map[string]any `json:"input,omitempty"`
}

// TaskView is a task as the owner or the program sees it: the task, and how
// far its receipt has come.
type TaskView struct {
	Task     string         `json:"task"`
	Consumer string         `json:"consumer"`
	Doing    string         `json:"doing"`
	Input    map[string]any `json:"input,omitempty"`
	// State is open (no receipt yet), taken (an intent names it, no outcome
	// yet), or the outcome's own word: done, failed, unknown.
	State   string `json:"state"`
	Intent  string `json:"intent,omitempty"`
	Outcome string `json:"outcome,omitempty"`
	Saying  string `json:"saying,omitempty"`
	// Signer and Authority are the program's key and the grant its receipt
	// stands on; Door is where the receipt was recorded. Empty until taken.
	Signer    string `json:"signer,omitempty"`
	Authority string `json:"authority,omitempty"`
	Door      string `json:"door,omitempty"`
}

// TaskShelf is the address of a program's shelf of tasks.
func TaskShelf(consumer string) string { return TasksAddress + "/" + consumer }

// GiveTask records a task for a program. Only the owner gives one, and only to
// a program in standing that holds a namespace, since a program without one
// can leave no receipt and so could never answer.
func (h *Home) GiveTask(a Actor, consumer, doing string, input map[string]any) (TaskView, error) {
	if err := ownerOnly(a, "task.give"); err != nil {
		return TaskView{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.Health(); err != nil {
		return TaskView{}, err
	}
	if h.replica.Kind == MirrorReplica {
		return TaskView{}, &Denied{Decision: authority.Decision{Consumer: OwnerID, Action: "task.give", Path: TaskShelf(consumer), Reason: "read_only_replica"}}
	}
	c, ok := h.reg.Get(consumer)
	if !ok || c.Revoked {
		return TaskView{}, fmt.Errorf("home: no program in standing by that identifier")
	}
	if strings.TrimSpace(doing) == "" {
		return TaskView{}, errors.New("home: a task says what is to be done")
	}
	if len(h.reg.Scopes(consumer, authority.Ledger)) == 0 {
		return TaskView{}, &Denied{Decision: authority.Decision{Consumer: consumer, Action: "task.give", Path: TaskShelf(consumer),
			Reason: "no_namespace"}}
	}
	payload, err := json.Marshal(taskPayload{Type: TaskFormat, Consumer: consumer, Doing: doing, Input: input})
	if err != nil {
		return TaskView{}, err
	}
	if len(payload) > maxTaskInput+len(doing)+256 || len(payload) > event.MaxPayload {
		return TaskView{}, fmt.Errorf("home: a task carries at most %d bytes of input; put the rest in the home and name its path", maxTaskInput)
	}
	r := ask(h.ownerDoor, map[string]any{"op": "write", "address": TaskShelf(consumer), "verb": TaskVerb,
		"payload": base64.StdEncoding.EncodeToString(payload), "attempt": "home:task:" + newID()})
	if r["record"] != daemon.Recorded {
		return TaskView{}, fmt.Errorf("home: the task was not recorded: %v (%v)", r["error"], r["code"])
	}
	return TaskView{Task: fmt.Sprint(r["id"]), Consumer: consumer, Doing: doing, Input: input, State: "open"}, nil
}

// Tasks lists a program's shelf with the state of each task's receipt. The
// owner may ask about any program; a program asks only about itself.
func (h *Home) Tasks(a Actor, consumer string) ([]TaskView, error) {
	if !a.Owner {
		if consumer != "" && consumer != a.Consumer {
			return nil, &Denied{Decision: authority.Decision{Consumer: a.Consumer, Action: "task.list", Path: TaskShelf(consumer), Reason: "owner_only"}}
		}
		consumer = a.Consumer
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if err := h.Health(); err != nil {
		return nil, err
	}
	c, ok := h.reg.Get(consumer)
	if !ok {
		return nil, ErrNotFound
	}
	if !a.Owner && (c.Revoked || a.Session != c.Version) {
		return nil, &Denied{Decision: h.reg.Decide(a.Consumer, a.Session, authority.Ledger, TaskShelf(consumer))}
	}
	return h.taskViews(c)
}

// taskViews reads the shelf and every receipt in the program's namespaces
// that names a task, and joins them. It reads the ledger and nothing else.
func (h *Home) taskViews(c *authority.Consumer) ([]TaskView, error) {
	r := ask(h.ownerDoor, map[string]any{"op": "log", "address": TaskShelf(c.ID), "verb": TaskVerb})
	if r["ok"] != true {
		return nil, fmt.Errorf("home: the shelf could not be read: %v", r["error"])
	}
	rows, _ := r["events"].([]map[string]any)
	byTask := map[string]*TaskView{}
	var order []string
	for _, row := range rows {
		if row["key"] != "root" {
			continue // only the owner's word is a task; nothing else on the shelf counts
		}
		var p taskPayload
		if b, err := base64.StdEncoding.DecodeString(fmt.Sprint(row["payload"])); err != nil || json.Unmarshal(b, &p) != nil || p.Type != TaskFormat {
			continue
		}
		id := fmt.Sprint(row["id"])
		byTask[id] = &TaskView{Task: id, Consumer: c.ID, Doing: p.Doing, Input: p.Input, State: "open"}
		order = append(order, id)
	}
	// Receipts: intents that name a task as their origin, and outcomes that
	// answer those intents.
	intents := map[string]string{} // intent id -> task id
	for _, ns := range h.reg.Scopes(c.ID, authority.Ledger) {
		r := ask(h.ownerDoor, map[string]any{"op": "log", "address": ns})
		rows, _ := r["events"].([]map[string]any)
		for _, row := range rows {
			if fmt.Sprint(row["address"]) != ns {
				continue // receipts sit on the shelf, the exact root, and nowhere under it
			}
			b, err := base64.StdEncoding.DecodeString(fmt.Sprint(row["payload"]))
			if err != nil {
				continue
			}
			switch fmt.Sprint(row["verb"]) {
			case receipt.VerbIntent:
				in, err := receipt.Read[receipt.Intent](b)
				if err != nil || !strings.HasPrefix(in.Witness.Origin, taskOrigin) {
					continue
				}
				task := strings.TrimPrefix(in.Witness.Origin, taskOrigin)
				tv, mine := byTask[task]
				if !mine {
					continue
				}
				intents[fmt.Sprint(row["id"])] = task
				if tv.Intent == "" {
					tv.State, tv.Intent = "taken", fmt.Sprint(row["id"])
					tv.Authority, _ = row["authority"].(string)
					tv.Signer = h.keyAlias(fmt.Sprint(row["author"]), tv.Authority)
					tv.Door, _ = row["door"].(string)
				}
			case receipt.VerbOutcome:
				res, err := receipt.Read[receipt.Result](b)
				if err != nil {
					continue
				}
				task, answers := intents[res.Witness.Receipt]
				if !answers {
					continue
				}
				tv := byTask[task]
				tv.State, tv.Outcome, tv.Saying = string(res.Outcome), fmt.Sprint(row["id"]), res.Saying
				tv.Intent = res.Witness.Receipt
			}
		}
	}
	out := make([]TaskView, 0, len(order))
	for _, id := range order {
		out = append(out, *byTask[id])
	}
	return out, nil
}

// TakeTask opens a receipt for a task: the program's intent, signed with its
// own key under its grant, naming the task as its origin. It refuses a task
// that is not on this program's shelf, one already taken, and one already
// closed. What it does not do is decide anything about the work: the program
// decides, within what it holds, and the gate weighs each act on its own.
func (h *Home) TakeTask(a Actor, task, doing string) (map[string]any, error) {
	if a.Owner {
		return nil, errors.New("home: the owner gives tasks; a program takes them")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.Health(); err != nil {
		return nil, err
	}
	c, ok := h.reg.Get(a.Consumer)
	if !ok || c.Revoked || a.Session != c.Version {
		return nil, &Denied{Decision: h.reg.Decide(a.Consumer, a.Session, authority.Ledger, TaskShelf(a.Consumer))}
	}
	namespaces := h.reg.Scopes(c.ID, authority.Ledger)
	if len(namespaces) == 0 {
		return nil, &Denied{Decision: authority.Decision{Consumer: c.ID, Action: authority.Ledger, Path: TaskShelf(c.ID), Reason: authority.NoGrant}}
	}
	ns := namespaces[0]
	views, err := h.taskViews(c)
	if err != nil {
		return nil, err
	}
	var tv *TaskView
	for i := range views {
		if views[i].Task == task {
			tv = &views[i]
		}
	}
	if tv == nil {
		return nil, fmt.Errorf("home: %w: no task %s on this program's shelf", ErrNotFound, task)
	}
	if tv.State != "open" {
		return nil, fmt.Errorf("home: task %s is %s already (intent %s); it is not taken twice", short(task), tv.State, short(tv.Intent))
	}
	if strings.TrimSpace(doing) == "" {
		doing = tv.Doing
	}
	key := keyName(c.ID, ns)
	grant := ""
	if k, ok := h.keys.Keys[key]; ok {
		grant = k.Grant
	}
	r := ask(h.progDoor, map[string]any{"op": "intent", "address": ns, "key": key, "doing": doing,
		"attempt": "home:task:take:" + task[:min(16, len(task))] + ":" + c.ID[:min(16, len(c.ID))],
		"witness": map[string]any{
			"origin":    taskOrigin + task,
			"authority": grant,
			"audience":  OwnerID,
			"state":     "taken",
			"wayBack":   "an outcome of failed or unknown closes this receipt; the owner may give the task again",
		}})
	if r["record"] != daemon.Recorded {
		return r, fmt.Errorf("home: the intent was not recorded: %v (%v)", r["error"], r["code"])
	}
	return map[string]any{"task": task, "intent": r["id"], "address": ns, "signer": key, "authority": grant,
		"door": ProgramDoor, "record": daemon.Recorded}, nil
}

// FinishTask closes a task's receipt with the program's outcome: done, failed,
// or unknown — and unknown is not a softer failed; it is the ending that was
// never learned, said out loud (T10.4). The outcome names the intent through
// the ledger's own parent link, and state and wayBack are the program's word
// on where the thing stands now and how it can be undone or appealed.
func (h *Home) FinishTask(a Actor, task, intent, outcome, saying, once, state, wayBack string) (map[string]any, error) {
	if a.Owner {
		return nil, errors.New("home: a program finishes its own task")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.Health(); err != nil {
		return nil, err
	}
	c, ok := h.reg.Get(a.Consumer)
	if !ok || c.Revoked || a.Session != c.Version {
		return nil, &Denied{Decision: h.reg.Decide(a.Consumer, a.Session, authority.Ledger, TaskShelf(a.Consumer))}
	}
	views, err := h.taskViews(c)
	if err != nil {
		return nil, err
	}
	var tv *TaskView
	for i := range views {
		if views[i].Task == task {
			tv = &views[i]
		}
	}
	switch {
	case tv == nil:
		return nil, fmt.Errorf("home: %w: no task %s on this program's shelf", ErrNotFound, task)
	case tv.State == "open":
		return nil, fmt.Errorf("home: task %s was never taken; take it first", short(task))
	case tv.State != "taken":
		return nil, fmt.Errorf("home: task %s is closed already (%s, outcome %s)", short(task), tv.State, short(tv.Outcome))
	case intent != "" && intent != tv.Intent:
		return nil, fmt.Errorf("home: task %s was taken by intent %s, not %s", short(task), short(tv.Intent), short(intent))
	}
	intent = tv.Intent
	ns := ""
	for _, n := range h.reg.Scopes(c.ID, authority.Ledger) {
		ns = n
		break
	}
	key := keyName(c.ID, ns)
	grant := ""
	if k, ok := h.keys.Keys[key]; ok {
		grant = k.Grant
	}
	if strings.TrimSpace(state) == "" {
		state = outcome
	}
	if strings.TrimSpace(wayBack) == "" {
		wayBack = "the owner may give the task again"
	}
	r := ask(h.progDoor, map[string]any{"op": "outcome", "address": ns, "key": key, "intent": intent,
		"outcome": outcome, "saying": saying, "once": once,
		"attempt": "home:task:done:" + task[:min(16, len(task))] + ":" + c.ID[:min(16, len(c.ID))],
		"witness": map[string]any{
			"origin":    taskOrigin + task,
			"authority": grant,
			"audience":  OwnerID,
			"state":     state,
			"wayBack":   wayBack,
		}})
	if r["record"] != daemon.Recorded {
		return r, fmt.Errorf("home: the outcome was not recorded: %v (%v)", r["error"], r["code"])
	}
	return map[string]any{"task": task, "intent": intent, "outcome": r["id"], "ending": outcome,
		"signer": key, "authority": grant, "door": ProgramDoor, "record": daemon.Recorded}, nil
}

// WaitTasks holds until a task appears on the program's shelf after the one
// named (or from the beginning, when after is empty), or until timeout
// seconds pass. It is the program's way of receiving work without asking
// every second; the home's lock is not held while it waits.
func (h *Home) WaitTasks(a Actor, after string, timeout int) (map[string]any, error) {
	if a.Owner {
		return nil, errors.New("home: programs wait on their shelves")
	}
	h.mu.RLock()
	c, ok := h.reg.Get(a.Consumer)
	if !ok || c.Revoked || a.Session != c.Version {
		d := h.reg.Decide(a.Consumer, a.Session, authority.Ledger, TaskShelf(a.Consumer))
		h.mu.RUnlock()
		return nil, &Denied{Decision: d}
	}
	h.mu.RUnlock()
	if after == "" {
		// The zero id is "from the beginning" for the door's cursor.
		after = hex.EncodeToString(make([]byte, 32))
	}
	r := ask(h.progDoor, map[string]any{"op": "wait", "address": TaskShelf(c.ID), "verb": TaskVerb,
		"after": after, "timeout": timeout})
	if r["ok"] != true {
		return r, fmt.Errorf("home: the shelf could not be watched: %v (%v)", r["error"], r["code"])
	}
	// Hand back tasks, not rows: the same shape the listing gives.
	views, err := func() ([]TaskView, error) {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return h.taskViews(c)
	}()
	if err != nil {
		return nil, err
	}
	rows, _ := r["events"].([]map[string]any)
	seen := map[string]bool{}
	for _, row := range rows {
		seen[fmt.Sprint(row["id"])] = true
	}
	out := []TaskView{}
	for _, v := range views {
		if seen[v.Task] {
			out = append(out, v)
		}
	}
	res := map[string]any{"tasks": out}
	if r["timed_out"] == true {
		res["timed_out"] = true
	}
	if last, ok := r["last"]; ok {
		res["last"] = last
	}
	return res, nil
}

// keyAlias names the key the home holds for a program by its public key and
// the grant it signed under, as the owner's door cannot: the program keys live
// in the home's own keyring, not the carrier's. One program's key may be held
// under several names, one per grant; the grant says which name signed.
func (h *Home) keyAlias(authorHex, grant string) string {
	fallback := ""
	for name, k := range h.keys.Keys {
		if len(k.Priv) != ed25519.PrivateKeySize {
			continue
		}
		if hex.EncodeToString(ed25519.PrivateKey(k.Priv).Public().(ed25519.PublicKey)) != authorHex {
			continue
		}
		if k.Grant == grant {
			return name
		}
		if fallback == "" || name < fallback {
			fallback = name
		}
	}
	if fallback != "" {
		return fallback
	}
	if authorHex == hex.EncodeToString(h.rootPub) {
		return "root"
	}
	return ""
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
