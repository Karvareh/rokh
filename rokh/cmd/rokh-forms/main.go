// Command rokh-forms is a thin local adapter, not part of Rokh.
//
// It exists to prove one thing: the generic CLI plus a small adapter is enough
// for a real application. Rokh is not specialized for this template, and this
// program uses no Rokh internals that any other program could not use.
//
// What it does, and only this:
//
//   - commit: read a bundle exported by the HTML page, store each form's text
//     as a plain file in a declared content root *outside* the carrier, build
//     a content descriptor, and hand it to `rokh write --payload-file -`.
//   - render: read `rokh log --json`, find the latest committed text for each
//     form, and rewrite the HTML page as a projection of the ledger.
//
// Boundaries it keeps:
//
//   - The content root belongs here, never to Rokh. It is refused if it sits
//     inside the carrier, so the carrier inventory of docs/05 cannot change.
//   - No listener, no port, no socket. The page hands over a file because a
//     file:// page cannot reach a Unix socket, and adding one to Rokh was
//     never an option.
//   - Nothing is recorded except by an explicit run of `commit`.
//   - The rendered HTML is a mutable projection. Deleting it loses nothing.
package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"rokh/content"
	"rokh/frame"
)

//go:embed template.html
var templateHTML string

const (
	addressPrefix = "forms"
	stateMarker   = `<script id="committed-state" type="application/json">`
)

type bundle struct {
	Version int `json:"rokh_forms"`
	Forms   []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Type  string `json:"type"`
		Text  string `json:"text"`
	} `json:"forms"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "commit":
		err = commit(os.Args[2:])
	case "render":
		err = render(os.Args[2:])
	case "template":
		_, err = os.Stdout.WriteString(templateHTML)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `rokh-forms - a local adapter over the generic rokh CLI

  rokh-forms commit   --carrier DIR --content-root DIR --bundle FILE [--rokh PATH]
  rokh-forms render   --carrier DIR --content-root DIR --out FILE    [--rokh PATH]
  rokh-forms template                                  > forms.html

The passphrase is inherited from the environment (ROKH_PASSPHRASE), because
this program never handles it: it only runs the rokh CLI.
`)
}

// contentRoot resolves the declared content root and refuses to let it sit
// inside the carrier. Rokh is not a content store, and the carrier inventory
// must not change because an application had somewhere to put a file.
func contentRoot(root, carrier string) (string, error) {
	if root == "" {
		return "", errors.New("--content-root is required and must be declared explicitly")
	}
	ar, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	ac, err := filepath.Abs(carrier)
	if err != nil {
		return "", err
	}
	if ar == ac || strings.HasPrefix(ar, ac+string(filepath.Separator)) {
		return "", fmt.Errorf("the content root must be outside the carrier; %s is inside %s", ar, ac)
	}
	if err := os.MkdirAll(ar, 0o700); err != nil {
		return "", err
	}
	return ar, nil
}

// blobPath is content-addressed: the descriptor alone is enough to find the
// bytes again, so no filename needs to be recorded anywhere.
func blobPath(root string, d content.Descriptor) string {
	h := d.Hash.String()
	return filepath.Join(root, h[:2], h[2:]+d.Extension())
}

func writeBlob(root string, d content.Descriptor, body []byte) (string, error) {
	p := blobPath(root, d)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	if _, err := os.Stat(p); err == nil {
		return p, nil // content-addressed: identical bytes, nothing to do
	}
	if err := os.WriteFile(p, body, 0o600); err != nil {
		return "", err
	}
	return p, nil
}

// runRokh calls the generic CLI. This is the whole point of the exercise: the
// adapter has no privileged access, only the same commands anyone has.
func runRokh(rokh string, stdin []byte, args ...string) (string, error) {
	cmd := exec.Command(rokh, args...)
	if stdin != nil {
		cmd.Stdin = strings.NewReader(string(stdin))
	}
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("rokh %s: %s", strings.Join(args, " "), msg)
	}
	return string(out), nil
}

func commit(args []string) error {
	fs := flag.NewFlagSet("commit", flag.ContinueOnError)
	carrier := fs.String("carrier", "", "carrier directory")
	root := fs.String("content-root", "", "where committed text is stored, outside the carrier")
	bundlePath := fs.String("bundle", "", "the JSON file exported by the page")
	rokh := fs.String("rokh", "rokh", "path to the rokh binary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *carrier == "" || *bundlePath == "" {
		return errors.New("--carrier and --bundle are required")
	}
	cr, err := contentRoot(*root, *carrier)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(*bundlePath)
	if err != nil {
		return err
	}
	var b bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return fmt.Errorf("%s: %w", *bundlePath, err)
	}
	if b.Version != 1 {
		return fmt.Errorf("%s: unknown bundle version %d", *bundlePath, b.Version)
	}
	if len(b.Forms) == 0 {
		return errors.New("bundle contains no forms; nothing to record")
	}

	for _, f := range b.Forms {
		if f.ID == "" || f.Type == "" {
			return errors.New("a form is missing its id or type")
		}
		// The text goes to disk exactly as it arrived. No normalization, no
		// repair: Persian text and ZWNJ must survive byte for byte.
		body := []byte(f.Text)
		d, err := content.New(body, f.Type)
		if err != nil {
			return err
		}
		path, err := writeBlob(cr, d, body)
		if err != nil {
			return err
		}
		payload, err := d.Encode()
		if err != nil {
			return err
		}
		// The event carries the descriptor, never the text. That is what keeps
		// an event small enough to travel, however large the form gets.
		out, err := runRokh(*rokh, payload,
			"write", *carrier,
			"--address", addressPrefix+"/"+f.ID,
			"--verb", content.VerbPut,
			"--payload-file", "-")
		if err != nil {
			return err
		}
		id := strings.Fields(strings.TrimSpace(out))
		short := ""
		if len(id) > 0 {
			short = id[0]
		}
		fmt.Printf("%s  %s  %d bytes  %s\n", short, f.ID, d.Size, d.Type)
		fmt.Printf("           content %s\n", path)
	}
	return nil
}

type logRow struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Verb    string `json:"verb"`
	Payload string `json:"payload"`
}

func render(args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	carrier := fs.String("carrier", "", "carrier directory")
	root := fs.String("content-root", "", "where committed text is stored")
	out := fs.String("out", "forms.html", "HTML file to write")
	rokh := fs.String("rokh", "rokh", "path to the rokh binary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *carrier == "" {
		return errors.New("--carrier is required")
	}
	cr, err := contentRoot(*root, *carrier)
	if err != nil {
		return err
	}
	logJSON, err := runRokh(*rokh, nil, "log", *carrier, "--json")
	if err != nil {
		return err
	}

	// Later events win: the ledger's order is deterministic, so the last
	// content.put for an address is the current committed state.
	latest := map[string]content.Descriptor{}
	eventOf := map[string]string{}
	for _, line := range strings.Split(logJSON, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row logRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if !strings.HasPrefix(row.Address, addressPrefix+"/") {
			continue
		}
		formID := strings.TrimPrefix(row.Address, addressPrefix+"/")
		switch row.Verb {
		case content.VerbPut:
			payload, err := base64.StdEncoding.DecodeString(row.Payload)
			if err != nil {
				continue
			}
			d, err := content.Decode(payload)
			if err != nil {
				continue
			}
			latest[formID] = d
			eventOf[formID] = row.ID
		case content.VerbDelete:
			// Deletion is itself an event; the projection simply stops showing
			// the content. History still holds both events.
			delete(latest, formID)
			delete(eventOf, formID)
		}
	}

	state := map[string]any{}
	for formID, d := range latest {
		body, err := os.ReadFile(blobPath(cr, d))
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: content for %s is not in the content root: %v\n", formID, err)
			continue
		}
		// Verify before showing: a descriptor names bytes, and these had
		// better be those bytes.
		if err := d.Verify(body); err != nil {
			fmt.Fprintf(os.Stderr, "warning: content for %s does not match its descriptor: %v\n", formID, err)
			continue
		}
		state[formID] = map[string]any{
			"text":  string(body),
			"hash":  d.Hash.Short(),
			"size":  d.Size,
			"type":  d.Type,
			"event": shortOf(eventOf[formID]),
		}
	}

	blob, err := json.Marshal(state)
	if err != nil {
		return err
	}
	i := strings.Index(templateHTML, stateMarker)
	if i < 0 {
		return errors.New("template is missing its committed-state marker")
	}
	j := strings.Index(templateHTML[i:], "</script>")
	if j < 0 {
		return errors.New("template has an unterminated committed-state block")
	}
	page := templateHTML[:i+len(stateMarker)] + string(blob) + templateHTML[i+j:]

	if err := os.WriteFile(*out, []byte(page), 0o600); err != nil {
		return err
	}
	fmt.Printf("rendered %d form(s) to %s\n", len(state), *out)
	fmt.Println("this page is a projection of the ledger, not history")
	return nil
}

func shortOf(id string) string {
	if pid, err := frame.ParseID(id); err == nil {
		return pid.Short()
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
