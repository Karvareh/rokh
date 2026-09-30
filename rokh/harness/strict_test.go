package harness_test

import (
	"testing"

	"rokh/harness"
)

// A harness stamp is read by the same exact rule: it is what decides whether a
// payload is this harness's business at all.
//
//	— T10.6, N4.1
func TestAStampIsReadByItsExactKeys(t *testing.T) {
	if s := harness.StampOf([]byte(`{"type":"clerk.ask.v1","version":"0.7"}`)); s.Type !=
		"clerk.ask.v1" || s.Version != "0.7" {
		t.Fatalf("a proper stamp read as %+v", s)
	}
	for _, spelling := range []string{
		`{"TYPE":"clerk.ask.v1","VERSION":"0.7"}`,
		`{"Type":"clerk.ask.v1","version":"0.7"}`,
		`{"type":"clerk.ask.v1","Version":"0.7"}`,
		`{"type":"clerk.ask.v1"}`,
	} {
		if s := harness.StampOf([]byte(spelling)); s.Type != "" || s.Version != "" {
			t.Errorf("%s read as %+v", spelling, s)
		}
	}
}
