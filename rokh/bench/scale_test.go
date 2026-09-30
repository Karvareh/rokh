package bench

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"rokh/vessel"
)

// The scale measurements. Each is a benchmark, run only when asked for:
//
//	go test ./bench -run '^$' -bench Scale -benchtime 1x -timeout 0
//
// They measure one thing each and report it with b.ReportMetric, so the
// numbers come out in the benchmark format and can be read side by side:
// the size of a vessel (Vessel, VesselDisk), the length of a history (Chain),
// the number of people a ledger names (Authority, Envelope, Leaf), the doors
// that write at once (Writers) and what one door costs as the ledger grows
// (Door). docs/11-scale.md holds the numbers this tree gave and the model
// they fit.

// sparseMedium is a vessel.Medium held in memory that keeps the bytes of a
// file only when they say something. A vessel fills every free slab, and each
// head file it has not written yet, with randomness that nothing reads back;
// quickRandom leaves such a fill zero, and this medium keeps an all-zero file
// of that size as its length alone. So a vessel of millions of slabs can be
// made, opened and written in the memory of one machine, and every byte the
// vessel reads or writes is still read and written. Every call is counted.
type sparseMedium struct {
	mu    sync.Mutex
	files map[string][]byte
	blank map[string]int
	dirs  map[string]map[string]bool // directory: its entries
	io    ioCount
}

// ioCount is what a medium was asked to do.
type ioCount struct {
	Reads, Writes, Lists, Mkdirs, Removes int64
	ReadBytes, WriteBytes                 int64
	HeadWrites, SlabWrites                int64
}

func (a ioCount) minus(b ioCount) ioCount {
	return ioCount{a.Reads - b.Reads, a.Writes - b.Writes, a.Lists - b.Lists, a.Mkdirs - b.Mkdirs,
		a.Removes - b.Removes, a.ReadBytes - b.ReadBytes, a.WriteBytes - b.WriteBytes,
		a.HeadWrites - b.HeadWrites, a.SlabWrites - b.SlabWrites}
}

func newSparse() *sparseMedium {
	return &sparseMedium{files: map[string][]byte{}, blank: map[string]int{},
		dirs: map[string]map[string]bool{"": {}}}
}

// blankMin is the smallest all-zero write kept as a length: a head file.
const blankMin = 64 << 10

var zeros = make([]byte, blankMin)

func allZero(b []byte) bool {
	for len(b) > 0 {
		n := min(len(b), len(zeros))
		if !bytes.Equal(b[:n], zeros[:n]) {
			return false
		}
		b = b[n:]
	}
	return true
}

func split(name string) (dir, base string) {
	i := strings.LastIndexByte(name, '/')
	if i < 0 {
		return "", name
	}
	return name[:i], name[i+1:]
}

func (m *sparseMedium) Read(name string, max int) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.io.Reads++
	var out []byte
	if b, ok := m.files[name]; ok {
		out = append([]byte(nil), b...)
	} else if n, ok := m.blank[name]; ok {
		out = make([]byte, n)
	} else {
		return nil, fmt.Errorf("%w: %s", vessel.ErrNotFound, name)
	}
	if len(out) > max {
		return nil, fmt.Errorf("%w: %s", vessel.ErrTooLarge, name)
	}
	m.io.ReadBytes += int64(len(out))
	return out, nil
}

func (m *sparseMedium) Write(name string, b []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dir, base := split(name)
	entries, ok := m.dirs[dir]
	if !ok {
		return fmt.Errorf("%w: no directory %s", vessel.ErrNotFound, dir)
	}
	entries[base] = true
	m.io.Writes++
	m.io.WriteBytes += int64(len(b))
	if strings.HasPrefix(base, "head") {
		m.io.HeadWrites++
	} else {
		m.io.SlabWrites++
	}
	if len(b) >= blankMin && allZero(b) {
		delete(m.files, name)
		m.blank[name] = len(b)
		return nil
	}
	delete(m.blank, name)
	m.files[name] = append([]byte(nil), b...)
	return nil
}

func (m *sparseMedium) Names(dir string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.io.Lists++
	entries, ok := m.dirs[dir]
	if !ok {
		return nil, fmt.Errorf("%w: %s", vessel.ErrNotFound, dir)
	}
	out := make([]string, 0, len(entries))
	for e := range entries {
		out = append(out, e)
	}
	sort.Strings(out)
	return out, nil
}

func (m *sparseMedium) Mkdir(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.io.Mkdirs++
	if _, ok := m.dirs[name]; ok {
		return vessel.ErrExists
	}
	dir, base := split(name)
	parent, ok := m.dirs[dir]
	if !ok {
		return fmt.Errorf("%w: no directory %s", vessel.ErrNotFound, dir)
	}
	parent[base] = true
	m.dirs[name] = map[string]bool{}
	return nil
}

func (m *sparseMedium) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.io.Removes++
	dir, base := split(name)
	delete(m.files, name)
	delete(m.blank, name)
	if entries, ok := m.dirs[dir]; ok {
		delete(entries, base)
	}
	return nil
}

// counted is what the medium has done so far.
func (m *sparseMedium) counted() ioCount {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.io
}

// held is how many bytes the medium keeps in memory, and how many it stands
// for: the size of the vessel as files would hold it.
func (m *sparseMedium) held() (kept, virtual int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.files {
		kept += int64(len(b))
	}
	virtual = kept
	for _, n := range m.blank {
		virtual += int64(n)
	}
	return kept, virtual
}

// quickRandom is a vessel's randomness for a measurement: a real generator
// for every draw a vessel keys or chooses with (salts, tokens, cells, slab
// choices, all a few hundred bytes at most), and nothing at all for a draw of
// blankMin bytes or more, which a vessel makes only to fill a slab or a head
// file nothing reads. The fill stays as it was allocated, zero, and
// sparseMedium keeps it as a length. It is for the measurements alone: a
// vessel filled this way shows which slabs are free.
type quickRandom struct{ c *rand.ChaCha8 }

func newQuickRandom(seed byte) *quickRandom {
	return &quickRandom{c: rand.NewChaCha8([32]byte{seed})}
}

func (q *quickRandom) Read(p []byte) (int, error) {
	if len(p) >= blankMin {
		return len(p), nil
	}
	return q.c.Read(p)
}

// holding is an owner that holds the vessel: a measurement has one writer.
type holding struct{}

func (holding) Holds() error { return nil }

// unlockWith opens a vessel with its key, without a passphrase: the cost of
// the passphrase is measured on its own (BenchmarkScalePassphrase).
func unlockWith(vk []byte) vessel.Unlock {
	return func(slots [][]byte, salt []byte, iter int) ([]byte, error) {
		return append([]byte(nil), vk...), nil
	}
}

// liveHeap is the heap in use after a collection, in bytes.
func liveHeap() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// peakRSS is the most memory this process has held, in bytes, where the
// host says; zero where it does not.
func peakRSS() uint64 {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmHWM:") {
			var kb uint64
			fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, "VmHWM:")), "%d", &kb)
			return kb << 10
		}
	}
	return 0
}

// mib is n bytes in MiB, for a metric.
func mib(n int64) float64 { return float64(n) / (1 << 20) }

// sizeName writes a number of bytes the way a person reads it.
func sizeName(n int64) string {
	for _, u := range []struct {
		shift int
		name  string
	}{{50, "PiB"}, {40, "TiB"}, {30, "GiB"}, {20, "MiB"}, {10, "KiB"}} {
		if n >= 1<<u.shift && n%(1<<u.shift) == 0 {
			return fmt.Sprintf("%d%s", n>>u.shift, u.name)
		}
	}
	return fmt.Sprintf("%dB", n)
}

func TestTheSparseMediumKeepsWhatItIsGivenAndCountsIt(t *testing.T) {
	m := newSparse()
	if err := m.Mkdir("rokh"); err != nil {
		t.Fatal(err)
	}
	if err := m.Mkdir("rokh"); !errors.Is(err, vessel.ErrExists) {
		t.Fatalf("a second mkdir: %v", err)
	}
	fill := make([]byte, blankMin)
	if err := m.Write("rokh/head0.rkh", fill); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte{1}, blankMin)
	if err := m.Write("rokh/head1.rkh", data); err != nil {
		t.Fatal(err)
	}
	kept, virtual := m.held()
	if kept != blankMin || virtual != 2*blankMin {
		t.Fatalf("kept %d, stands for %d", kept, virtual)
	}
	got, err := m.Read("rokh/head0.rkh", blankMin)
	if err != nil || len(got) != blankMin || !allZero(got) {
		t.Fatalf("the blank file read back as %d bytes, %v", len(got), err)
	}
	got, err = m.Read("rokh/head1.rkh", blankMin)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("the written file read back wrong: %v", err)
	}
	names, _ := m.Names("rokh")
	if strings.Join(names, ",") != "head0.rkh,head1.rkh" {
		t.Fatalf("names %v", names)
	}
	c := m.counted()
	if c.Writes != 2 || c.HeadWrites != 2 || c.Reads != 2 || c.ReadBytes != 2*blankMin {
		t.Fatalf("counted %+v", c)
	}
	q := newQuickRandom(1)
	small := make([]byte, 32)
	q.Read(small)
	if allZero(small) {
		t.Fatal("a small draw came out zero")
	}
}
