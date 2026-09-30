package vessel

import (
	"sort"
	"strings"
	"sync"
)

// Call is one Medium call, as Memory logs it.
type Call struct {
	Op   string // read, write, names, mkdir, remove
	Name string
	Len  int
}

// Memory is a Medium held in memory. It is portable and serves tests and
// hosts that keep a vessel somewhere other than files. It can log every call
// and refuse writes on demand, which is how a cut is made without a power
// failure.
type Memory struct {
	mu     sync.Mutex
	files  map[string][]byte
	dirs   map[string]bool
	record bool
	log    []Call
	// Fail, when set, is asked before every Write; a non-nil answer stops the
	// write before a byte lands, and is returned.
	Fail func(name string, writes int) error
	// Torn, when set, is asked before every Write; a positive answer n makes
	// the write land only its first n bytes over the old ones and then fail,
	// as a medium does when the power goes in the middle of a file.
	Torn   func(name string, writes int) int
	writes int
}

// NewMemory returns an empty medium.
func NewMemory() *Memory {
	return &Memory{files: map[string][]byte{}, dirs: map[string]bool{"": true}}
}

// Record switches the call log on or off and empties it.
func (m *Memory) Record(on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record, m.log = on, nil
}

// Log returns the calls recorded since Record(true).
func (m *Memory) Log() []Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Call(nil), m.log...)
}

// Writes counts every Write call ever made.
func (m *Memory) Writes() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writes
}

func (m *Memory) note(op, name string, n int) {
	if m.record {
		m.log = append(m.log, Call{op, name, n})
	}
}

func parent(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[:i]
	}
	return ""
}

func (m *Memory) Read(name string, max int) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = strings.ToLower(name)
	m.note("read", name, 0)
	b, ok := m.files[name]
	if !ok {
		return nil, ErrNotFound
	}
	if len(b) > max {
		return nil, ErrTooLarge
	}
	return append([]byte(nil), b...), nil
}

func (m *Memory) Write(name string, b []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = strings.ToLower(name)
	m.writes++
	m.note("write", name, len(b))
	if !m.dirs[parent(name)] {
		return ErrNotFound
	}
	if m.Fail != nil {
		if err := m.Fail(name, m.writes); err != nil {
			return err
		}
	}
	if m.Torn != nil {
		if n := m.Torn(name, m.writes); n > 0 && n < len(b) {
			old := m.files[name]
			nb := append([]byte(nil), old...)
			if len(nb) < n {
				nb = append(nb, make([]byte, n-len(nb))...)
			}
			copy(nb, b[:n])
			m.files[name] = nb
			return ErrTorn
		}
	}
	m.files[name] = append([]byte(nil), b...)
	return nil
}

// ErrTorn is returned by a write that Memory tore on purpose.
var ErrTorn = &codeError{"torn", "the write was torn"}

func (m *Memory) Names(dir string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dir = strings.ToLower(dir)
	m.note("names", dir, 0)
	if !m.dirs[dir] {
		return nil, ErrNotFound
	}
	seen := map[string]bool{}
	for f := range m.files {
		if parent(f) == dir {
			seen[f[len(dir)+1:]] = true
		}
	}
	for d := range m.dirs {
		if d != "" && parent(d) == dir {
			seen[d[len(dir)+1:]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}

func (m *Memory) Mkdir(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = strings.ToLower(name)
	m.note("mkdir", name, 0)
	if m.dirs[name] {
		return ErrExists
	}
	if !m.dirs[parent(name)] {
		return ErrNotFound
	}
	m.dirs[name] = true
	return nil
}

func (m *Memory) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = strings.ToLower(name)
	m.note("remove", name, 0)
	if _, ok := m.files[name]; !ok {
		return ErrNotFound
	}
	delete(m.files, name)
	return nil
}

// Clone copies the whole medium, the way a folder is copied at one instant.
func (m *Memory) Clone() *Memory {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := NewMemory()
	for k, v := range m.files {
		c.files[k] = append([]byte(nil), v...)
	}
	for k := range m.dirs {
		c.dirs[k] = true
	}
	return c
}

// CopyFile copies one file from another medium, the way a copier that walks
// a folder file by file does.
func (m *Memory) CopyFile(from *Memory, name string) {
	from.mu.Lock()
	b, ok := from.files[name]
	b = append([]byte(nil), b...)
	from.mu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	for d := parent(name); d != ""; d = parent(d) {
		m.dirs[d] = true
	}
	if ok {
		m.files[name] = b
	}
}

// Sizes lists every file with its length.
func (m *Memory) Sizes() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]int{}
	for k, v := range m.files {
		out[k] = len(v)
	}
	return out
}

// FileNames lists every file, sorted.
func (m *Memory) FileNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.files))
	for k := range m.files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Flip inverts one bit of a file, for tamper tests.
func (m *Memory) Flip(name string, bit int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.files[name]
	if len(b) == 0 {
		return
	}
	b[(bit/8)%len(b)] ^= 1 << (bit % 8)
}

// Put sets a file's bytes directly, for tests that move or plant files.
func (m *Memory) Put(name string, b []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for d := parent(name); d != ""; d = parent(d) {
		m.dirs[d] = true
	}
	m.files[name] = append([]byte(nil), b...)
}

// Get returns a file's bytes.
func (m *Memory) Get(name string) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.files[name]...)
}
