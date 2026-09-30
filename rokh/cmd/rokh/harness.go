package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"

	"rokh/harness"
)

// Binding a harness, from the terminal.
//
// The core weighs bytes, signature and authority. It does not know what a verb
// means and does not guess, so a harness that wants Rokh to carry its meaning
// says four things first: its own space for addresses and verbs, its version,
// everything it can do, and what it does with a verb it has never heard. Four
// more follow for the effects it lands somewhere Rokh does not rule: what a
// repeat does there, whether it may send again on its own, whether there is an
// act that undoes it, and what it does when it never learns how the step
// ended.
//
// All eight could be said before this, by writing the socket's JSON by hand.
// That is the wrong place to keep a covenant. Seven clauses look exactly like
// eight to whoever typed them, and the one thing that must not be
// approximately said is the declaration everything afterwards is read under.
//
// So each of the eight is a flag, and not one of them has a default. A default
// here would be this program answering on the harness's behalf — and what a
// repeat does at a destination outside Rokh is not this program's to answer.
//
//	— T11.10, T10.7, T13.5

// harnessVerbs collects --can. It accumulates across repeats and splits on
// commas, so a list of verbs can be written either way and neither way is the
// one that quietly wins.
type harnessVerbs []string

func (v *harnessVerbs) String() string { return strings.Join(*v, ",") }

func (v *harnessVerbs) Set(s string) error {
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*v = append(*v, part)
		}
	}
	return nil
}

// harnessSays is each answer in the language of the person reading it. Every
// gloss is the specification's own sentence for that answer, shortened. An
// answer with no gloss here is printed bare rather than guessed at: the whole
// point of these eight is that nobody fills one in for the harness.
//
//	— T11.10, T10.7, T10.4
var harnessSays = map[string]string{
	string(harness.Refuse):       "says it does not know that verb",
	string(harness.Ignore):       "passes over it without claiming anything",
	string(harness.Idempotent):   "with the same deduplication id, however many times it goes it lands once",
	string(harness.Duplicates):   "it lands again; that is not a fault of Rokh, it is news about somewhere else",
	string(harness.MayRetry):     "it resends by itself, without asking",
	string(harness.NeverRetry):   "it does not resend by itself; a person decides",
	string(harness.Compensable):  "there is an act that undoes it",
	string(harness.Irreversible): "there is no way back; the receipt is the only trace left of it",
	string(harness.CloseUnknown): "closes it unknown and stops there",
	string(harness.AskAPerson):   "closes it unknown and puts it in front of a person",
}

func harnessGloss(answer string) string {
	if say, known := harnessSays[answer]; known {
		return " (" + say + ")"
	}
	return ""
}

// harnessDigits renders a count in Persian digits, so a number in a Persian
// sentence reads as part of it.
func harnessDigits(n int) string { return strconv.Itoa(n) }

// harnessDeclared is the eight things one harness said, on their way to the
// screen. Binding and listing print the same eight, because the person who has
// just bound and the person reading a listing are asking one question.
type harnessDeclared struct {
	namespace  string
	version    string
	can        []string
	unknown    string
	repeat     string
	retry      string
	compensate string
	ending     string
	mayResend  bool
}

func (d harnessDeclared) write(out io.Writer) {
	fmt.Fprintf(out, "harness %q, version %s\n", d.namespace, d.version)
	fmt.Fprintf(out, "  what it can do (%s): %s\n",
		harnessDigits(len(d.can)), strings.Join(d.can, ", "))
	fmt.Fprintf(out, "  an unknown verb: %s%s\n", d.unknown, harnessGloss(d.unknown))
	fmt.Fprintf(out, "  repeating: %s%s\n", d.repeat, harnessGloss(d.repeat))
	fmt.Fprintf(out, "  retrying: %s%s\n", d.retry, harnessGloss(d.retry))
	fmt.Fprintf(out, "  compensating: %s%s\n", d.compensate, harnessGloss(d.compensate))
	fmt.Fprintf(out, "  a lost ending: %s%s\n", d.ending, harnessGloss(d.ending))
	// Whether sending the same effect again is safe is not a ninth
	// declaration. It is the arithmetic of two of the eight, and it is shown
	// because a person who reads a receipt later will want to know what a
	// repeat of it would have meant.
	//   — T10.7
	if d.mayResend {
		fmt.Fprintln(out, "  resending: safe")
		return
	}
	fmt.Fprintln(out, "  resending: not safe (on these four declarations alone; it is not forbidden)")
}

// harnessFrom reads one harness out of a listing.
func harnessFrom(m map[string]any) harnessDeclared {
	text := func(key string) string {
		s, _ := m[key].(string)
		return s
	}
	d := harnessDeclared{
		namespace:  text("namespace"),
		version:    text("version"),
		unknown:    text("unknown"),
		repeat:     text("repeat"),
		retry:      text("retry"),
		compensate: text("compensate"),
		ending:     text("ending"),
	}
	if list, ok := m["can"].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok {
				d.can = append(d.can, s)
			}
		}
	}
	d.mayResend, _ = m["mayResend"].(bool)
	return d
}

// harnessConn is one open line to a running daemon: newline-delimited JSON in,
// newline-delimited JSON out, which is the whole protocol.
type harnessConn struct {
	conn net.Conn
	rw   *bufio.ReadWriter
}

func dialHarness(socket string) (*harnessConn, error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("I could not reach the daemon at %s: %w", socket, err)
	}
	return &harnessConn{
		conn: conn,
		rw:   bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)),
	}, nil
}

func (h *harnessConn) close() { h.conn.Close() }

func (h *harnessConn) ask(req map[string]any) (map[string]any, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := h.rw.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	if err := h.rw.Flush(); err != nil {
		return nil, err
	}
	line, err := h.rw.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(line, &out)
}

// refused turns a daemon's no into this program's no, with the daemon's own
// words carried through untouched.
//
// The core refuses rather than repairs, and its refusal says which clause is
// wrong and why. Restating that in easier words would cost the person the one
// sentence they can act on, so it is passed along exactly as it arrived.
//
//	— T11.10, T10.7
func refused(resp map[string]any, what string) error {
	msg, said := resp["error"].(string)
	if !said || strings.TrimSpace(msg) == "" {
		return fmt.Errorf("%s; the daemon said no and did not say why", what)
	}
	return fmt.Errorf("%s: %s", what, msg)
}

func cmdBind(args []string) error { return bindHarness(os.Stdout, args) }

func bindHarness(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("bind", flag.ContinueOnError)
	socket := fs.String("socket", "", "socket of the running daemon")
	namespace := fs.String("namespace", "", "the harness's own space for addresses and verbs")
	version := fs.String("version", "", "which version of the harness is speaking")
	var can harnessVerbs
	fs.Var(&can, "can", "a verb it can do, under the namespace; repeat or separate with commas")
	unknown := fs.String("unknown", "", "a verb inside its space it has never heard: "+
		string(harness.Refuse)+" or "+string(harness.Ignore))
	repeat := fs.String("repeat", "", "what a second identical effect does at the destination: "+
		string(harness.Idempotent)+" or "+string(harness.Duplicates))
	retry := fs.String("retry", "", "whether it may send again on its own: "+
		string(harness.MayRetry)+" or "+string(harness.NeverRetry))
	compensate := fs.String("compensate", "", "whether an act undoes the effect: "+
		string(harness.Compensable)+" or "+string(harness.Irreversible))
	ending := fs.String("ending", "", "what it does when it never learns how the step ended: "+
		string(harness.CloseUnknown)+" or "+string(harness.AskAPerson))
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *socket == "" {
		return errors.New("--socket was not given; without it I do not know where to bind the covenant")
	}

	// A missing clause is named and nothing is sent. A covenant with a hole in
	// it is not a covenant with a default, and choosing what the harness left
	// unsaid would be this program declaring in its place.
	//   — T11.10
	var holes []string
	for _, clause := range []struct {
		flag    string
		missing bool
	}{
		{"--namespace", strings.TrimSpace(*namespace) == ""},
		{"--version", strings.TrimSpace(*version) == ""},
		{"--can", len(can) == 0},
		{"--unknown", strings.TrimSpace(*unknown) == ""},
	} {
		if clause.missing {
			holes = append(holes, "«"+clause.flag+"»")
		}
	}
	if len(holes) > 0 {
		return fmt.Errorf("the covenant has four clauses and all four are required; %s missing. I sent nothing",
			strings.Join(holes, ", "))
	}

	// And the same for the four about effects, which are a separate refusal in
	// the core because they answer a separate question: the covenant says what
	// a verb means, these say what happens somewhere else when it is acted on.
	//   — T10.7
	var undeclared []string
	for _, decl := range []struct{ flag, said string }{
		{"--repeat", *repeat},
		{"--retry", *retry},
		{"--compensate", *compensate},
		{"--ending", *ending},
	} {
		if strings.TrimSpace(decl.said) == "" {
			undeclared = append(undeclared, "«"+decl.flag+"»")
		}
	}
	if len(undeclared) > 0 {
		return fmt.Errorf("an effect has four declarations and all four are required; %s missing. I sent nothing",
			strings.Join(undeclared, ", "))
	}

	// Sorted before it goes, because the register holds the list sorted. What
	// is printed here and what a later listing shows are then one list in one
	// order.
	verbs := append([]string(nil), can...)
	sort.Strings(verbs)

	h, err := dialHarness(*socket)
	if err != nil {
		return err
	}
	defer h.close()

	resp, err := h.ask(map[string]any{
		"op": "bind", "namespace": *namespace, "version": *version,
		"can": verbs, "unknown": *unknown,
		"repeat": *repeat, "retry": *retry,
		"compensate": *compensate, "ending": *ending,
	})
	if err != nil {
		return err
	}
	if bound, _ := resp["ok"].(bool); !bound {
		return refused(resp, "the harness was not bound")
	}

	// Printed from what came back, not from what was typed — including the
	// verbs, which used to be overwritten here with the typed list. That was
	// harmless in fact, because Bind refuses a list it would have to alter
	// rather than normalising one, but it claimed a check that was not
	// happening: the whole point of printing the answer is that a person sees
	// what was bound, and a field filled in locally shows them their own
	// typing back.
	//   — T11.10
	shown := harnessFrom(resp)
	fmt.Fprintln(out, "bound.")
	shown.write(out)
	return nil
}

func cmdBound(args []string) error { return listHarnesses(os.Stdout, args) }

// listHarnesses shows which meanings are answered for on this ledger, by which
// version, and what each said about the effects it lands outside Rokh.
//
//	— T11.10, T10.7
func listHarnesses(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("bound", flag.ContinueOnError)
	socket := fs.String("socket", "", "socket of the running daemon")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *socket == "" {
		return errors.New("--socket was not given; without it I do not know where to read")
	}

	h, err := dialHarness(*socket)
	if err != nil {
		return err
	}
	defer h.close()

	resp, err := h.ask(map[string]any{"op": "bound"})
	if err != nil {
		return err
	}
	if read, _ := resp["ok"].(bool); !read {
		return refused(resp, "I could not read what is bound")
	}
	list, _ := resp["harnesses"].([]any)
	if len(list) == 0 {
		fmt.Fprintln(out, "no harness is bound; Rokh is carrying the meaning of no verb.")
		return nil
	}
	fmt.Fprintf(out, "%s harnesses bound:\n", harnessDigits(len(list)))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fmt.Fprintln(out)
		harnessFrom(m).write(out)
	}
	return nil
}
