package shell

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh/size"
	"rokh/vessel"
)

// The first event states no room: it is the sentence that made the ledger
// and nothing is invented beside it. The room is the vessel's, and a Rokh
// made without naming one has the vessel's own default, which the screen
// shows as it is.
func TestTheFirstEventStatesNoRoom(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "plain")
	s, err := makeRokh(vault, "", "", "pass", "home", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.closeAll()
	g, ok := s.current.led.Get(s.current.led.Genesis())
	if !ok {
		t.Fatal("no genesis")
	}
	if got := string(g.Event.Payload); got != newLedgerStem+"home" {
		t.Fatalf("the first event says %q", got)
	}
	info := s.current.car.Vessel().Info()
	st := s.snapshot()
	if int64(info.Slabs)*int64(info.SlabSize) != defaultRoom || st.Room != size.Write(defaultRoom) ||
		st.Free != size.Write(int64(info.Free)*int64(info.SlabSize)) || !strings.HasPrefix(st.Growth, "fixed") || st.Full {
		t.Fatalf("the screen was told %+v for a vessel of %d slabs of %d, %d free", st, info.Slabs, info.SlabSize, info.Free)
	}
}

// A room is a vessel in whole slabs, at least the vessel's minimum.
func TestARoomIsWholeSlabs(t *testing.T) {
	for room, want := range map[int64][2]int{
		4 << 20: {18, 16}, 5 << 20: {18, 20}, 8 << 20: {18, 32}, (8 << 20) + 1: {18, 33},
		16 << 20: {20, 16}, 128 << 20: {20, 128}, 5 << 40: {21, 5 << 19},
	} {
		p, err := roomParams(room)
		if err != nil || p.SlabLog2 != want[0] || p.Slabs != want[1] {
			t.Errorf("room %d: slabs of 2^%d, %d of them (%v); want 2^%d, %d", room, p.SlabLog2, p.Slabs, err, want[0], want[1])
		}
		if roomOf(p) < room {
			t.Errorf("room %d was made %d", room, roomOf(p))
		}
	}
	if _, err := roomParams(3 << 20); err == nil || !strings.Contains(err.Error(), "the smallest Rokh is 4 MB") {
		t.Fatalf("3 MB: %v", err)
	}
}

// The gate asks the size and whether it grows, holds it to the smallest
// Rokh, and makes the vessel exactly that size; the screen shows what the
// vessel holds.
func TestTheGateMakesTheRoomItIsTold(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "sized")
	tm := scripted(t, "y", "3M", "8M", "y", "pass", "pass", "home")
	s, err := enter(tm, vault, "", "")
	if err != nil {
		t.Fatalf("%v\n%s", err, tm.said.String())
	}
	defer s.closeAll()
	if !strings.Contains(tm.said.String(), "the smallest Rokh is 4 MB") {
		t.Fatalf("a room under the smallest was not asked again:\n%s", tm.said.String())
	}
	info := s.current.car.Vessel().Info()
	if int64(info.Slabs)*int64(info.SlabSize) != 8<<20 || !info.Growth.Auto || info.Growth.Max < info.Slabs {
		t.Fatalf("8M growing made %d slabs of %d, growth %+v", info.Slabs, info.SlabSize, info.Growth)
	}
	st := s.snapshot()
	if st.Room != "8 MB" || !strings.HasPrefix(st.Growth, "by itself, up to ") {
		t.Fatalf("the screen was told room %q, growth %q", st.Room, st.Growth)
	}
}

// A Rokh filled to its size still opens, shows its room as the vessel holds
// it, and answers the next recording that does not fit with vessel_full and
// the way to make room.
func TestAFullRokhStillOpensAndSaysSo(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "small")
	s, err := makeRokh(vault, "", "", "pass", "home", minRoom)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "megabyte.bin")
	body := make([]byte, 1<<20)
	if _, err := rand.Read(body); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, body, 0o600); err != nil {
		t.Fatal(err)
	}
	bring := func(s *session, n int) error {
		_, err := s.execute(command{Op: opImport, Path: file, Address: "files/f" + digits(n)})
		return err
	}
	full := false
	for i := 0; i < 8 && !full; i++ {
		if err := bring(s, i); err != nil {
			if !errors.Is(err, vessel.ErrFull) {
				t.Fatalf("bring %d failed otherwise: %v", i, err)
			}
			full = true
		}
	}
	if !full {
		t.Fatal("eight megabytes went into a Rokh of four")
	}
	s.closeAll()

	again := newSession(vault, "", "", "pass")
	defer again.closeAll()
	if err := again.openByName("home"); err != nil {
		t.Fatalf("a full Rokh did not open: %v", err)
	}
	info := again.current.car.Vessel().Info()
	st := again.snapshot()
	if st.Room != "4 MB" || st.Free != size.Write(int64(info.Free)*int64(info.SlabSize)) || st.Full != (info.Free == 0) {
		t.Fatalf("the full Rokh shows %+v; its vessel holds %d free slabs", st, info.Free)
	}
	said := again.plain(bring(again, 99)).Error()
	if !strings.Contains(said, "rokh grow") || !strings.HasSuffix(said, "[vessel_full; not recorded]") {
		t.Fatalf("the next bring was answered %q", said)
	}
	if _, err := again.execute(command{Op: opSeeLedger}); err != nil {
		t.Fatalf("a full Rokh was not read: %v", err)
	}
}
