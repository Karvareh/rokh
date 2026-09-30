package gate

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"

	"rokh-home/authority"
	"rokh-home/home"
	"rokh/booth"
	"rokh/frame"
)

// The gate is the booth of a home (contract section 5): it identifies each
// program, holds it to its view and starts it. A program speaks rokh.booth/1:
// hello, prove with its credential (on a stream the gate bound to it before it
// ran, the empty credential names it), and then the home's operations, each
// decided against that program's grants before anything is read or done.

// gateAuthority answers who a credential belongs to, for one stream.
type gateAuthority struct {
	s     *Server
	bound string // the program this stream was bound to, if any
}

func (a gateAuthority) Anchor() frame.ID {
	st := a.s.home.OwnerLedger(map[string]any{"op": "status"})
	id, _ := frame.ParseID(fmtString(st["anchor"]))
	return id
}

func (a gateAuthority) KeyByID([32]byte) (booth.Identity, bool) {
	// A program is known by the credential the gate issued it; the owner's
	// channel is the sealed one, proven with the owner's key.
	return booth.Identity{}, false
}

func (a gateAuthority) Credential(token string) (booth.Identity, bool) {
	var c authority.Consumer
	switch {
	case token == "" && a.bound != "":
		cons, ok := a.s.home.Consumer(a.bound)
		if !ok || cons.Revoked {
			return booth.Identity{}, false
		}
		c = cons
	case token != "":
		cred, err := hex.DecodeString(token)
		if err != nil {
			return booth.Identity{}, false
		}
		cons, ok := a.s.home.Authenticate(cred)
		if !ok || (a.bound != "" && cons.ID != a.bound) {
			return booth.Identity{}, false
		}
		c = cons
	default:
		return booth.Identity{}, false
	}
	id := booth.Identity{Key: sha256.Sum256([]byte("rokh-home/program/" + c.ID)), Gen: uint32(c.Version),
		Name: c.Name, Credential: c.ID, Program: c.ID}
	for _, g := range c.Grants {
		if g.Action == authority.Read || g.Action == authority.Bytes {
			id.Reads = append(id.Reads, g.Scope)
		}
	}
	return id, true
}

func fmtString(v any) string { s, _ := v.(string); return s }

// serveBooth runs one rokh.booth/1 session for a program.
func (s *Server) serveBooth(rw io.ReadWriter, bound string) {
	auth := gateAuthority{s: s, bound: bound}
	_ = booth.Serve(rw, auth, booth.HandlerFunc(func(sess *booth.Session, req map[string]any) map[string]any {
		c, ok := s.home.Consumer(sess.Who.Credential)
		if !ok || c.Revoked {
			// Served nothing more, and unbound: it may say hello again only
			// while it stands (B7).
			sess.Bound = false
			return booth.Refuse(booth.CodeKeyRevoked, errString("this program's standing was withdrawn"))
		}
		actor := home.Actor{Consumer: c.ID, Session: uint64(sess.Who.Gen)}
		inner := map[string]any{}
		for k, v := range req {
			if k != "v" && k != "id" {
				inner[k] = v
			}
		}
		// B2: a message's own "id" is the message's. The object an op is
		// about travels as "ref" (a draft, an import) or "event".
		for _, alias := range []string{"ref", "event"} {
			if v, ok := req[alias]; ok {
				inner["id"] = v
				delete(inner, alias)
			}
		}
		return s.dispatch(actor, inner, false)
	}), nil)
}

type errString string

func (e errString) Error() string { return string(e) }

// firstLine reads a stream's first message and gives back a reader that
// replays it, so one listener can tell rokh.booth/1 from the older line form.
func firstLine(c net.Conn) ([]byte, io.Reader, error) {
	r := bufio.NewReaderSize(c, 64<<10)
	line, err := r.ReadSlice('\n')
	if err != nil && len(line) == 0 {
		return nil, nil, err
	}
	head := append([]byte(nil), line...)
	return head, io.MultiReader(bytes.NewReader(head), r), nil
}

// speaksBooth says whether a first message is rokh.booth/1.
func speaksBooth(line []byte) bool {
	var m map[string]any
	if json.Unmarshal(bytes.TrimSpace(line), &m) != nil {
		return false
	}
	return m["v"] == booth.Protocol
}

// ReadOpen: the gate serves programs by credential; a guest is the
// carrier's booth's to serve (B5), not the home's.
func (a gateAuthority) ReadOpen() []string { return nil }
