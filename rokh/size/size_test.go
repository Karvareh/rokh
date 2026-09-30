package size

import "testing"

// A size is read the way a person writes one and written back the way a
// person reads one. These pairs are the whole of what the two promise, and
// they are here rather than beside either caller because a chest's size and a
// ledger's reservation must mean the same thing by "2G".
func TestASizeIsReadAndWrittenTheWayAPersonSaysIt(t *testing.T) {
	read := map[string]int64{
		"2G": 2 << 30, "2g": 2 << 30, "2 GB": 2 << 30, "2gb": 2 << 30,
		"500M": 500 << 20, "800K": 800 << 10, "1T": 1 << 40,
		"1.5G": 1<<30 + 1<<29, "4096": 4096, "4096B": 4096,
	}
	for text, want := range read {
		got, err := Parse(text)
		if err != nil || got != want {
			t.Errorf("%q read as %d, %v; want %d", text, got, err, want)
		}
	}
	for _, text := range []string{"", "G", "0", "0G", "-2G", "two gigs", "2X", "2 buckets"} {
		if n, err := Parse(text); err == nil {
			t.Errorf("%q was accepted as %d", text, n)
		}
	}
	written := map[int64]string{
		2 << 30: "2 GB", 1 << 40: "1 TB", 500 << 20: "500 MB",
		1<<30 + 1<<29: "1.5 GB", 1023: "1023 bytes", 1: "1 byte",
	}
	for n, want := range written {
		if got := Write(n); got != want {
			t.Errorf("%d written as %q, want %q", n, got, want)
		}
	}
	// What is read and written back is the same size.
	for _, n := range []int64{1 << 30, 3 << 20, 700 << 10} {
		back, err := Parse(Write(n))
		if err != nil || back != n {
			t.Errorf("%d written as %q read back as %d, %v", n, Write(n), back, err)
		}
	}
}
