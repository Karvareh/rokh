package arch

import (
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// every package whose source must obey the architectural prohibitions.
var allPkgs = []string{
	"frame", "event", "ledger", "carrier", "announce", "oracle",
	"working", "generation", "covenant", "content", "bundle", "receipt",
	"seal", "harness", "bond", "answer", "selective", "daemon", "shell", "tui",
	"chest", "size", "turn", "keyview",
}

// wholeTree returns the tree's source with every comment removed.
//
// Stripping the comments is not tidiness; it is what keeps these tests from
// lying in both directions. A word search over prose fires on a sentence that
// *denies* the thing — a comment saying "there is no token here" is not a
// token — and it is silent about a synonym. These tests are an alarm, not
// evidence, and weak.tsv says so; reading only the code is the least they owe.
func wholeTree(t *testing.T) string {
	t.Helper()
	var sb strings.Builder
	for _, p := range allPkgs {
		sb.WriteString(codeOnly(t, filepath.Join("..", p)))
	}
	return sb.String()
}

// codeOnly reads a package's non-test source with comments stripped.
func codeOnly(t *testing.T, dir string) string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") ||
			strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		f, err := parser.ParseFile(fset, p, nil, 0) // no ParseComments: they are dropped
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		var out strings.Builder
		if err := printer.Fprint(&out, fset, f); err != nil {
			t.Fatal(err)
		}
		sb.WriteString(out.String())
		sb.WriteString("\n")
	}
	return sb.String()
}

// Being right is not a vote. There is no miner, no token, no fee, no stake and
// no longest chain, because there is no double spend to settle: an individual's
// ledger holds nothing two people can fight over. Rokh will not have a token —
// not out of piety, but because a ledger that needs one to be written is no
// longer the individual's.
//
//	— N-Axiom6, N7.4, N2.5, T11.1
func TestNoConsensusMachineryAndNoToken(t *testing.T) {
	body := strings.ToLower(wholeTree(t))
	for _, bad := range []string{
		"quorum", "consensus", "longest chain", "miner", "mining",
		"proof of work", "proof of stake", "token", "gas fee", "blockchain",
	} {
		if strings.Contains(body, bad) {
			t.Errorf("the tree mentions %q; consensus machinery needs its own ruling, "+
				"and a token is refused outright", bad)
		}
	}
}

// The ledger says what happened; it does not say it was good. A measure may
// report "it reads seventy hundredths"; no threshold turns that into
// "accepted". Acceptance is a discrete verdict about bytes, lineage and
// authority — never a number crossing a line.
//
//	— N-Axiom7, T12, T12.1, T12.2
func TestNoThresholdTurnsDegreeIntoAcceptance(t *testing.T) {
	// The code, with comments stripped. A sentence saying "no threshold here"
	// is not a threshold, and an alarm that cannot tell the difference forbids
	// the ledger from saying what it does not do.
	verdict := codeOnly(t, "../ledger")
	for _, bad := range []string{
		"float64", "float32", "threshold", "confidence", "score", "probability",
	} {
		if strings.Contains(strings.ToLower(verdict), bad) {
			t.Errorf("the ledger mentions %q; a verdict is not a number and no "+
				"threshold may produce one", bad)
		}
	}
	// The verdict is a closed set of three, and "unknown" is not "false".
	for _, want := range []string{"Pending", "Accepted", "Rejected"} {
		if !strings.Contains(verdict, want) {
			t.Errorf("the verdict %q is gone from the ledger", want)
		}
	}
}

// The receipt is a custom the harnesses keep on Rokh, not part of Rokh. The
// core knows events and nothing else, so no package in the core may name the
// receipt package or its verbs.
//
//	— T10.5, T10
func TestTheCoreKnowsNothingOfTheReceiptCustom(t *testing.T) {
	for _, pkg := range []string{"frame", "event", "ledger", "carrier", "working"} {
		body := readAll(t, filepath.Join("..", pkg))
		for _, bad := range []string{"rokh/receipt", "VerbIntent", "VerbOutcome"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s reaches for %q; the ledger knows events and nothing else", pkg, bad)
			}
		}
	}
}

// Rokh is upstream of meaning and lineage — not upstream of cryptography and
// the network. It is not a network path, not a content store and not an
// identity system: it takes those into service from the free wheels that
// already exist and does not rebuild them. What it says is which lineage a
// person acted in, and what addressed verb they performed.
//
//	— T11, T11.1, T11.2
func TestRokhRebuildsNoWheelItCanTakeIntoService(t *testing.T) {
	body := strings.ToLower(wholeTree(t))
	for what, bad := range map[string][]string{
		// The daemon's own Unix socket is the declared gateway and is
		// governed by TestNoNetworkListenerOutsideTheDaemon. What is refused
		// here is a *transport*: a way to reach another machine.
		"a network path":     {"net/http", "quic", "libp2p", "grpc", "websocket", "\"tcp\""},
		"a content store":    {"database/sql", "blobstore", "objectstore", "s3."},
		"an identity system": {"oauth", "openid", "saml", "jwt", "x509"},
	} {
		for _, b := range bad {
			if strings.Contains(body, b) {
				t.Errorf("the tree contains %q; Rokh does not rebuild %s, it takes one into service", b, what)
			}
		}
	}
	// What it does hold is the lineage and the addressed verb.
	ev := readAll(t, "../event")
	for _, want := range []string{"Parents", "Address", "Verb", "Authority"} {
		if !strings.Contains(ev, want) {
			t.Errorf("an event no longer carries %q", want)
		}
	}
}

// The place of decision — the seventeenth minute — belongs to a person and is
// not counted. Nothing in the tree decides on a person's behalf: there is no
// scheduler, no rule engine, no policy that fires by itself.
//
//	— T12.4, T4.2
func TestNothingDecidesOnAPersonsBehalf(t *testing.T) {
	body := strings.ToLower(wholeTree(t))
	for _, bad := range []string{
		"time.afterfunc", "time.tick", "time.newticker", "cron",
		"autoapprove", "auto_approve", "policyengine",
	} {
		if strings.Contains(body, bad) {
			t.Errorf("the tree contains %q; nothing may act for a person, and no "+
				"event is authored except by an explicit act", bad)
		}
	}
}

// What the ledger proves is exactly this: the author, the bytes, the lineage,
// and the authority that was presented. Not the outward truth of what was
// said, and not the completeness of what was shown. The core therefore holds
// no notion of "true", "verified fact" or "trust score".
//
//	— T12.5, T1.3
func TestTheLedgerProvesAuthorshipNotTruth(t *testing.T) {
	body := strings.ToLower(wholeTree(t))
	for _, bad := range []string{"istrue", "verifiedfact", "trustscore", "reputation"} {
		if strings.Contains(body, bad) {
			t.Errorf("the tree contains %q; the ledger witnesses the writer, "+
				"never the world", bad)
		}
	}
}

// Every event carries freshness, and there is one writing path that draws it.
// Fifteen places that each have to remember is fifteen that can forget, so
// nothing outside the event package signs directly: it calls SignFresh, and
// this reads the code — comments stripped — to keep it that way.
//
//	— T3.6, T4.1
func TestOnlyOneWritingPathDrawsFreshness(t *testing.T) {
	for _, pkg := range append(append([]string{}, allPkgs...),
		"cmd/rokh", "cmd/rokh-shell", "cmd/rokh-courier", "cmd/rokh-forms",
		"cmd/rokh-chest") {
		if pkg == "event" {
			continue
		}
		body := codeOnly(t, filepath.Join("..", pkg))
		if strings.Contains(body, "event.Sign(") {
			t.Errorf("%s calls event.Sign directly; the one writing path is "+
				"event.SignFresh, which draws the freshness the ruling requires", pkg)
		}
	}
}
