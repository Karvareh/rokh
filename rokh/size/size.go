// Package size is how a number of bytes is written by a person and written
// back to them, in one place so that two parts of Rokh cannot disagree about
// what "2G" means.
//
// Units are 1024-based and said as they are meant: 2G is 2^31 bytes, and the
// surface says GB where it asks. The parser is deliberately narrow — a number
// and a unit — because a size a person types is a decision, and a clever
// parser that guesses at "a couple of gigs" would be guessing at a decision.
package size

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var units = []struct {
	suffix string
	scale  int64
}{
	{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10},
}

// Parse reads a size: a number, then a unit spelled K, KB, M, MB, G, GB, T or
// TB, in any case, with or without a space. A bare number is bytes. Zero and
// less are refused: nothing is not a size anybody means.
func Parse(v string) (int64, error) {
	text := strings.TrimSpace(strings.ToUpper(v))
	if text == "" {
		return 0, errors.New("no size")
	}
	i := 0
	for i < len(text) && (text[i] >= '0' && text[i] <= '9' || text[i] == '.') {
		i++
	}
	number, suffix := text[:i], strings.TrimSpace(text[i:])
	if number == "" {
		return 0, fmt.Errorf("%q does not begin with a number", v)
	}
	n, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number I can read", number)
	}
	scale := int64(1)
	switch strings.TrimSuffix(suffix, "B") {
	case "":
		if suffix != "" && suffix != "B" {
			return 0, fmt.Errorf("%q is not a size I know; say it as 500M, 2G or 1T", v)
		}
	case "K":
		scale = 1 << 10
	case "M":
		scale = 1 << 20
	case "G":
		scale = 1 << 30
	case "T":
		scale = 1 << 40
	default:
		return 0, fmt.Errorf("%q is not a size I know; say it as 500M, 2G or 1T", v)
	}
	bytes := int64(n * float64(scale))
	if bytes <= 0 {
		return 0, errors.New("a size of nothing is not a size")
	}
	return bytes, nil
}

// Write gives a size back the way it is read: the largest unit that leaves a
// number at least one, with a single decimal only where it says something.
func Write(n int64) string {
	for _, u := range units {
		if n >= u.scale {
			whole, tenths := n/u.scale, (n%u.scale)*10/u.scale
			if tenths == 0 {
				return fmt.Sprintf("%d %s", whole, u.suffix)
			}
			return fmt.Sprintf("%d.%d %s", whole, tenths, u.suffix)
		}
	}
	if n == 1 {
		return "1 byte"
	}
	return fmt.Sprintf("%d bytes", n)
}
