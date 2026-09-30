package ledger

import (
	"sort"
	"strings"

	"rokh/frame"
)

// idset is an ordered, duplicate-free set of ids.
//
// Ordered rather than a map because these sets are *shared*. Most events hold
// exactly their parent's authority set, so interning keeps one copy across
// thousands of events. A map could not be shared that way.
type idset []frame.ID

func (s idset) contains(id frame.ID) bool {
	i := sort.Search(len(s), func(i int) bool { return s[i].Compare(id) >= 0 })
	return i < len(s) && s[i] == id
}

func (s idset) add(id frame.ID) idset {
	i := sort.Search(len(s), func(i int) bool { return s[i].Compare(id) >= 0 })
	if i < len(s) && s[i] == id {
		return s
	}
	out := make(idset, 0, len(s)+1)
	out = append(out, s[:i]...)
	out = append(out, id)
	out = append(out, s[i:]...)
	return out
}

// union merges two ordered sets. Sets are never mutated in place, so sharing
// the result is safe.
func union(a, b idset) idset {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make(idset, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch a[i].Compare(b[j]) {
		case -1:
			out = append(out, a[i])
			i++
		case 1:
			out = append(out, b[j])
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	out = append(out, a[i:]...)
	out = append(out, b[j:]...)
	return out
}

// interner keeps one copy of each distinct set.
type interner map[string]idset

func (n interner) canon(s idset) idset {
	if len(s) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.Grow(len(s) * frame.IDSize)
	for _, id := range s {
		sb.Write(id[:])
	}
	k := sb.String()
	if got, ok := n[k]; ok {
		return got
	}
	n[k] = s
	return s
}
