package conformance

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Kind is the specification's own three-way distinction, from the frame line
// of رساله.md: design rulings bind every implementation; the base profile and
// the status of band 13 and the field guesses are reports, not requirements.
type Kind string

const (
	Ruling   Kind = "ruling"  // a design ruling — owes a production path and a test
	Profile  Kind = "profile" // the base profile — owes the same
	KindNote Kind = "note"    // a status line or a field guess — a report; owes neither
)

// Bearing reports whether this kind owes evidence. Forcing a status line into
// a code comment to make the map green is explicitly forbidden.
func (k Kind) Bearing() bool { return k == Ruling || k == Profile }

// readTSV strips comments and blank lines and splits the rest on tabs. The
// first column of every returned row is the line number, for error messages.
func readTSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows [][]string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
		rows = append(rows, append([]string{fmt.Sprint(n)}, cols...))
	}
	return rows, sc.Err()
}
