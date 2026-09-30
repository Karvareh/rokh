package working

import "sort"

// Memory keeps drafts in memory and nowhere else. It is the default, and it
// is the whole of the argument in T4.5: swap it for a durable store sealed by
// the carrier and not one rule above this line changes.
//
//	— T4.5
type Memory struct{ m map[string][]byte }

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory { return &Memory{m: map[string][]byte{}} }

func (s *Memory) Put(name string, b []byte) error {
	s.m[name] = append([]byte(nil), b...)
	return nil
}

func (s *Memory) Get(name string) ([]byte, bool, error) {
	b, ok := s.m[name]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), b...), true, nil
}

func (s *Memory) Delete(name string) error {
	delete(s.m, name)
	return nil
}

func (s *Memory) List() ([]string, error) {
	out := make([]string, 0, len(s.m))
	for k := range s.m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}
