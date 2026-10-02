// Package gate is a Rokh home's gate: the one trusted process that holds the
// home open, and the only way in.
//
// It serves two kinds of connection, and neither is a way around the other.
//
// The owner's socket carries a channel keyed from the owner's passphrase. The
// owner's command proves it knows the key with a challenge, the gate proves the
// same back, and every frame after that is sealed under keys drawn from both
// sides' fresh randomness. The passphrase itself never crosses the socket, so a
// process that put its own socket in the gate's place learns nothing it could
// use, and one that relays between the two can neither read nor change a frame.
//
// A program's connection is either handed to it by the gate itself — one end of
// a socket pair, inherited as a descriptor when the gate starts the program in
// its enclosure, and bound to that program before the program runs — or opened
// on the gate's socket and identified by the program's credential. On a bound
// connection a credential that belongs to another program is refused and the
// connection closed. Either way, identity is the gate's to establish: a name in
// a request, a process number or a declared namespace is not an identity.
package gate

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"

	"rokh/booth"
)

// Protocol names what the gate speaks: the contract's one protocol of both
// booths (contract section 5), rokh.booth/1, on the owner's channel and on a
// program's connection alike (T2). Every message is one JSON object with
// "v" and "id"; every answer carries the same "id", "ok", and on failure
// "code" and "error".
const Protocol = booth.Protocol

// MaxLine bounds one frame.
const MaxLine = 8 << 20

const (
	channelInfo = "rokh-home/1/owner-channel"
	proofOwner  = "rokh-home/1/owner-proof"
	proofGate   = "rokh-home/1/gate-proof"
	frameAAD    = "rokh-home/1/frame"
)

var (
	ErrProof   = errors.New("gate: the proof does not hold")
	errTooLong = errors.New("gate: a frame is over its limit")
)

func readLine(r *bufio.Reader, max int) ([]byte, error) {
	var out []byte
	for {
		chunk, err := r.ReadSlice('\n')
		out = append(out, chunk...)
		if len(out) > max {
			return nil, errTooLong
		}
		switch {
		case err == nil:
			return bytes.TrimRight(out, "\r\n"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(out) > 0:
			return nil, io.ErrUnexpectedEOF
		default:
			return nil, err
		}
	}
}

type lineConn struct {
	c net.Conn
	r *bufio.Reader
}

func newLineConn(c net.Conn) *lineConn { return &lineConn{c: c, r: bufio.NewReaderSize(c, 64<<10)} }

func (l *lineConn) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = l.c.Write(append(b, '\n'))
	return err
}

func (l *lineConn) readJSON(v any) error {
	line, err := readLine(l.r, MaxLine)
	if err != nil {
		return err
	}
	return json.Unmarshal(line, v)
}

func mac(key []byte, label string, parts ...[]byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(label))
	for _, p := range parts {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(p)))
		m.Write(n[:])
		m.Write(p)
	}
	return m.Sum(nil)
}

func aeadOf(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func channelKeys(ownerKey, nonce, challenge []byte, server bool) (cipher.AEAD, cipher.AEAD, error) {
	k, err := hkdf.Key(sha256.New, ownerKey, append(append([]byte(nil), nonce...), challenge...), channelInfo, 64)
	if err != nil {
		return nil, nil, err
	}
	c2s, err := aeadOf(k[:32])
	if err != nil {
		return nil, nil, err
	}
	s2c, err := aeadOf(k[32:])
	if err != nil {
		return nil, nil, err
	}
	if server {
		return s2c, c2s, nil
	}
	return c2s, s2c, nil
}

// sealedConn is the owner's channel after the handshake.
type sealedConn struct {
	*lineConn
	send, recv   cipher.AEAD
	sendN, recvN uint64
}

func (s *sealedConn) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[4:], s.sendN)
	s.sendN++
	frame := base64.StdEncoding.EncodeToString(s.send.Seal(nil, nonce, b, []byte(frameAAD)))
	_, err = s.c.Write([]byte(frame + "\n"))
	return err
}

func (s *sealedConn) read(v any) error {
	line, err := readLine(s.r, MaxLine*2)
	if err != nil {
		return err
	}
	ct, err := base64.StdEncoding.DecodeString(string(line))
	if err != nil {
		return ErrProof
	}
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[4:], s.recvN)
	s.recvN++
	plain, err := s.recv.Open(nil, nonce, ct, []byte(frameAAD))
	if err != nil {
		return ErrProof
	}
	return json.Unmarshal(plain, v)
}

func random(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// ownerAccept is the gate's side of the owner's handshake.
// The owner's channel is bound as every rokh.booth/1 session is, by hello and
// prove before anything else is answered (B3), with the owner's own kind of
// proof: hello offers auth "owner" and a 32-byte nonce, and is answered a
// challenge; prove gives the owner's proof over both, and is answered the
// gate's proof back. Then every frame is sealed, and each holds one
// rokh.booth/1 message.
type handshake struct {
	V     string `json:"v"`
	ID    any    `json:"id"`
	Op    string `json:"op"`
	Auth  string `json:"auth"`
	Nonce string `json:"nonce"`
	Proof string `json:"proof"`
}

// answerTo is an answer of the owner's handshake, in the protocol's shape.
func answerTo(req handshake, kv map[string]any) map[string]any {
	kv["v"] = Protocol
	if req.ID != nil {
		kv["id"] = req.ID
	}
	return kv
}

func ownerAccept(l *lineConn, ownerKey []byte) (*sealedConn, error) {
	var hello handshake
	if err := l.readJSON(&hello); err != nil {
		return nil, err
	}
	nonce, err := hex.DecodeString(hello.Nonce)
	if hello.V != Protocol || hello.Op != "hello" || hello.Auth != "owner" || err != nil || len(nonce) != 32 {
		l.writeJSON(answerTo(hello, map[string]any{"ok": false, "code": booth.CodeBadRequest,
			"error": "the owner's channel speaks " + Protocol + ": it starts with hello, auth \"owner\" and a 32-byte nonce"}))
		return nil, ErrProof
	}
	challenge := random(32)
	if err := l.writeJSON(answerTo(hello, map[string]any{"ok": true, "protocol": Protocol, "challenge": hex.EncodeToString(challenge)})); err != nil {
		return nil, err
	}
	var prove handshake
	if err := l.readJSON(&prove); err != nil {
		return nil, err
	}
	proof, err := hex.DecodeString(prove.Proof)
	if prove.V != Protocol || prove.Op != "prove" || err != nil || !hmac.Equal(proof, mac(ownerKey, proofOwner, nonce, challenge)) {
		l.writeJSON(answerTo(prove, map[string]any{"ok": false, "code": booth.CodeProof, "reason": "owner_proof", "error": "the proof does not hold"}))
		return nil, ErrProof
	}
	if err := l.writeJSON(answerTo(prove, map[string]any{"ok": true, "server_proof": hex.EncodeToString(mac(ownerKey, proofGate, challenge, nonce))})); err != nil {
		return nil, err
	}
	send, recv, err := channelKeys(ownerKey, nonce, challenge, true)
	if err != nil {
		return nil, err
	}
	return &sealedConn{lineConn: l, send: send, recv: recv}, nil
}

// OwnerClient is the owner's side of the channel.
type OwnerClient struct {
	sc  *sealedConn
	seq int
}

// DialOwner opens the owner's channel to the gate at path.
func DialOwner(path string, ownerKey []byte) (*OwnerClient, error) {
	c, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	l := newLineConn(c)
	nonce := random(32)
	if err := l.writeJSON(map[string]any{"v": Protocol, "id": "hello", "op": "hello", "auth": "owner", "nonce": hex.EncodeToString(nonce)}); err != nil {
		c.Close()
		return nil, err
	}
	var hello struct {
		OK        bool   `json:"ok"`
		Challenge string `json:"challenge"`
		Error     string `json:"error"`
	}
	if err := l.readJSON(&hello); err != nil {
		c.Close()
		return nil, err
	}
	challenge, err := hex.DecodeString(hello.Challenge)
	if !hello.OK || err != nil || len(challenge) != 32 {
		c.Close()
		return nil, fmt.Errorf("gate: %s", hello.Error)
	}
	if err := l.writeJSON(map[string]any{"v": Protocol, "id": "prove", "op": "prove", "proof": hex.EncodeToString(mac(ownerKey, proofOwner, nonce, challenge))}); err != nil {
		c.Close()
		return nil, err
	}
	var proved struct {
		OK          bool   `json:"ok"`
		ServerProof string `json:"server_proof"`
		Error       string `json:"error"`
	}
	if err := l.readJSON(&proved); err != nil {
		c.Close()
		return nil, err
	}
	sp, err := hex.DecodeString(proved.ServerProof)
	if !proved.OK || err != nil || !hmac.Equal(sp, mac(ownerKey, proofGate, challenge, nonce)) {
		c.Close()
		if !proved.OK {
			return nil, fmt.Errorf("%w: %s", ErrProof, proved.Error)
		}
		return nil, fmt.Errorf("%w: the other end is not this home's gate", ErrProof)
	}
	send, recv, err := channelKeys(ownerKey, nonce, challenge, false)
	if err != nil {
		c.Close()
		return nil, err
	}
	return &OwnerClient{sc: &sealedConn{lineConn: l, send: send, recv: recv}}, nil
}

// Call sends one request, as a rokh.booth/1 message of its own id, and reads
// its answer.
func (o *OwnerClient) Call(req map[string]any) (map[string]any, error) {
	o.seq++
	msg := map[string]any{}
	for k, v := range req {
		msg[k] = v
	}
	// The message's own id is the message's (B2): the id of the thing a
	// request is about travels as "ref".
	if ref, ok := req["id"]; ok {
		msg["ref"] = ref
	}
	msg["v"], msg["id"] = Protocol, fmt.Sprint(o.seq)
	if err := o.sc.write(msg); err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := o.sc.read(&resp); err != nil {
		return nil, err
	}
	if resp["id"] != msg["id"] {
		return nil, fmt.Errorf("gate: the answer names request %v, not %v", resp["id"], msg["id"])
	}
	return resp, nil
}

// Close closes the channel.
func (o *OwnerClient) Close() error { return o.sc.c.Close() }

// Client is a program's side of a gate connection, in rokh.booth/1: it binds
// with hello and prove before anything else, by the credential the gate
// issued, or, on a connection the gate handed the program, by the empty
// credential that names it.
type Client struct {
	c net.Conn
	b *booth.Client
}

// NewClient speaks over a connection the program already has: the descriptor
// the gate handed it, or a socket it dialled.
func NewClient(c net.Conn) *Client { return &Client{c: c, b: booth.NewClient(c)} }

// Dial opens the gate's socket.
func Dial(path string) (*Client, error) {
	c, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	return NewClient(c), nil
}

// Bind binds the session: hello, then prove with the credential; the empty
// credential on a connection the gate bound to one program. It answers the
// gate's whoami.
func (c *Client) Bind(credential string) (map[string]any, error) {
	return c.b.ProveCredential(credential)
}

// Call sends one request and reads its answer; the request's "op" names the
// operation.
func (c *Client) Call(req map[string]any) (map[string]any, error) {
	op, _ := req["op"].(string)
	fields := map[string]any{}
	for k, v := range req {
		fields[k] = v
	}
	// The message's own id is the message's (B2): the id of the thing a
	// request is about travels as "ref".
	if ref, ok := req["id"]; ok {
		fields["ref"] = ref
	}
	return c.b.Call(op, fields)
}

// Close closes the connection.
func (c *Client) Close() error { return c.c.Close() }
