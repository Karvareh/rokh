package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"rokh/booth"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/ledger"
	"rokh/transport"
)

// boothFixture is a carrier with a root, a reading key over "journal", and two
// events: one inside that view, one outside it.
//
// Since T2 a booth serves a key what its own reader opens, so the fixture is
// sealed as a v1 carrier is: every record to the readers of its own point
// (the owner of that point, the keys whose reads cover its address, and the
// system reader for the address rokh), and the key's add carries the system
// reader sealed to the key, with an envelope of its own for the key (E2). Its
// keyring holds one add, the reading key's (review probes count it): no
// owner generation is recorded, so the owner's cell's generation stands for
// the owner at every point, and the owner's door vouches for it.
type boothFixture struct {
	dir       string
	rootPriv  ed25519.PrivateKey
	readerKid [32]byte
	reader    key.Reader
	inside    frame.ID
	outside   frame.ID
}

func newBoothFixture(t *testing.T) (*boothFixture, *Server) {
	t.Helper()
	f := &boothFixture{dir: t.TempDir()}
	_, root, _ := ed25519.GenerateKey(rand.Reader)
	f.rootPriv = root
	gen, err := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("booth fixture")}, root)
	if err != nil {
		t.Fatal(err)
	}
	car, sec := createBare(t, f.dir, "synthetic passphrase", root, gen.ID)
	owner, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		t.Fatal(err)
	}
	sys, err := key.NewReader(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	anchor := gen.ID
	putSealed(t, f.dir, car, gen, "main", owner.Public(), sys.Public())
	f.reader, err = key.NewReader(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.readerKid = [32]byte{0x42}
	sysSeal, err := key.SealTo(f.reader.Public(), sys.Bytes(), key.InfoSystem, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kr, err := event.Keyring{Op: event.KeyringAdd, Key: f.readerKid, Gen: 1, Name: "journal-reader",
		Reader: f.reader.Public(), Reads: []string{"journal"}, System: sysSeal}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	add, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{gen.ID}, Address: event.AddressRoot, Verb: event.VerbKeyring, Payload: kr}, root)
	if err != nil {
		t.Fatal(err)
	}
	putSealed(t, f.dir, car, add, "main", owner.Public(), sys.Public(), f.reader.Public())
	in, _ := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{add.ID}, Address: "journal/today", Verb: "note", Payload: []byte("inside the view")}, root)
	putSealed(t, f.dir, car, in, "main", owner.Public(), f.reader.Public())
	out, _ := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{in.ID}, Address: "private/diary", Verb: "note", Payload: []byte("outside the view")}, root)
	putSealed(t, f.dir, car, out, "main", owner.Public())
	f.inside, f.outside = in.ID, out.ID
	return f, ownerDoor(t, car, sec, Options{AllowSign: true, Dir: f.dir, Door: "rokh", Root: root})
}

// The same script, over every transport this host offers.
func TestTheBoothScriptRunsOverEveryTransport(t *testing.T) {
	f, srv := newBoothFixture(t)
	transports := map[string]func(t *testing.T) (io.ReadWriteCloser, func()){
		"pipe": func(t *testing.T) (io.ReadWriteCloser, func()) {
			a, b := transport.Pipe()
			go srv.ServeBooth(b)
			return a, func() { a.Close(); b.Close() }
		},
		"standard streams": func(t *testing.T) (io.ReadWriteCloser, func()) {
			inR, inW, _ := os.Pipe()
			outR, outW, _ := os.Pipe()
			go func() { srv.ServeBooth(transport.Streams(inR, outW)); outW.Close() }()
			c := struct {
				io.Reader
				io.Writer
				io.Closer
			}{outR, inW, inW}
			return c, func() { inW.Close(); inR.Close(); outR.Close() }
		},
		"tcp 127.0.0.1": func(t *testing.T) (io.ReadWriteCloser, func()) {
			ln, err := transport.Listen(transport.TCP, "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			go srv.ListenBooth(ln)
			c, err := transport.Dial(transport.TCP, ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			return c, func() { c.Close(); ln.Close() }
		},
		"unix socket": func(t *testing.T) (io.ReadWriteCloser, func()) {
			path := filepath.Join(socketFolder(t), "b.sock")
			ln, err := transport.Listen(transport.Unix, path)
			if err != nil {
				t.Fatal(err)
			}
			go srv.ListenBooth(ln)
			c, err := transport.Dial(transport.Unix, path)
			if err != nil {
				t.Fatal(err)
			}
			return c, func() { c.Close(); ln.Close(); os.Remove(path) }
		},
	}
	for name, open := range transports {
		t.Run(name, func(t *testing.T) {
			conn, done := open(t)
			defer done()
			script(t, f, booth.NewClient(conn))
		})
	}
}

// socketFolder is a fresh short folder for a Unix socket, whose path has a
// short limit: under ROKH_SHORT_TMP, which the host supplies, or else under
// this work tree's .t. Nothing goes to a literal /tmp.
func socketFolder(t *testing.T) string {
	t.Helper()
	base := os.Getenv("ROKH_SHORT_TMP")
	if base == "" {
		base = filepath.Join("..", "..", "..", "..", "..", "..", ".t")
	}
	if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
		t.Skip("no short socket folder: set ROKH_SHORT_TMP to a short folder in the work tree")
	}
	abs, err := filepath.Abs(base)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(abs, "b")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func script(t *testing.T, f *boothFixture, c *booth.Client) {
	t.Helper()
	// B3: nothing is answered before the session is bound.
	if r, err := c.Call("log", nil); err != nil || r["code"] != booth.CodeHelloFirst {
		t.Fatalf("an unbound session was answered: %v %v", r, err)
	}
	// A wrong proof binds nothing.
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	if r, _ := c.ProveKey([32]byte{}, other); r["ok"] == true {
		t.Fatal("a key that is not the owner's proved itself as the owner")
	}
	// A reading key, proven by opening the challenge sealed to its reader.
	who, err := c.ProveReader(f.readerKid, f.reader)
	if err != nil || who["ok"] != true || who["owner"] == true {
		t.Fatalf("the reading key did not bind: %v %v", who, err)
	}
	log, _ := c.Call("log", nil)
	rows, _ := log["events"].([]any)
	seen := map[string]map[string]any{}
	for _, r := range rows {
		m := r.(map[string]any)
		seen[m["id"].(string)] = m
	}
	if in := seen[f.inside.String()]; in == nil || in["address"] != "journal/today" {
		t.Fatalf("the event inside the view is not shown whole: %v", in)
	}
	if out := seen[f.outside.String()]; out == nil || out["address"] != nil || out["head_only"] != true {
		t.Fatalf("the event outside the view showed more than its head: %v", out)
	}
	for _, m := range seen {
		if m["judged"] != "lineage" {
			t.Fatalf("a restricted view reported %v", m["judged"])
		}
	}
	got, _ := c.Call("get", map[string]any{"event": f.outside.String()})
	if got["head_only"] != true {
		t.Fatalf("get outside the view handed over the body: %v", got)
	}
	view, _ := c.Call("view", nil)
	planets, _ := view["planets"].([]any)
	if len(planets) != 1 || planets[0].(map[string]any)["planet"] != "journal" {
		t.Fatalf("the reading key's view: %v", planets)
	}
	if w, _ := c.Call("write", map[string]any{"address": "journal/x", "verb": "note"}); w["ok"] == true || w["record"] != NotRecorded {
		t.Fatalf("a reading key's write was not refused as not recorded: %v", w)
	}
	// The owner, on the same stream after a new hello.
	if r, err := c.ProveKey([32]byte{}, f.rootPriv); err != nil || r["owner"] != true || r["judged"] != "full" {
		t.Fatalf("the owner did not bind: %v %v", r, err)
	}
	view, _ = c.Call("view", nil)
	if planets, _ := view["planets"].([]any); len(planets) != 2 {
		t.Fatalf("the owner's view: %v", planets)
	}
	w, _ := c.Call("write", map[string]any{"address": "journal/booth", "verb": "note", "message": "through the booth", "attempt": "booth-" + hex.EncodeToString([]byte{byte(len(rows))})})
	if w["record"] != Recorded {
		t.Fatalf("the owner's write: %v", w)
	}
}

// The live view is the reopened view.
func TestTheBoothsLiveViewIsTheReopenedOne(t *testing.T) {
	f, srv := newBoothFixture(t)
	a, b := net.Pipe()
	defer a.Close()
	go srv.ServeBooth(b)
	c := booth.NewClient(a)
	if _, err := c.ProveKey([32]byte{}, f.rootPriv); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		w, _ := c.Call("write", map[string]any{"address": "journal/live", "verb": "note", "message": fmt.Sprint("line ", i)})
		if w["record"] != Recorded {
			t.Fatalf("write %d: %v", i, w)
		}
	}
	st, _ := c.Call("status", nil)
	car, _ := openV1(t, f.dir, "synthetic passphrase")
	heads, err := car.Heads()
	if err != nil {
		t.Fatal(err)
	}
	gen, _ := car.Get(car.Anchor())
	led, err := ledger.Load(gen, car.Get, heads)
	if err != nil {
		t.Fatal(err)
	}
	acc, _, _ := led.Tally()
	if fmt.Sprint(st["accepted"]) != fmt.Sprint(acc) {
		t.Fatalf("live %v accepted, reopened %d", st["accepted"], acc)
	}
}
