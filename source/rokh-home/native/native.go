// Package native holds a Holochain conductor for one home, beside the home's
// own network-free core rather than inside it. Nothing in home/ or store/
// imports this package: a home opens, reads, writes, searches, exports and
// closes with no Holochain present at all, and says so when asked.
//
// What lives here is the broker's trusted runtime manager:
//
//   - the engine offer: which conductor is on this host, at which version, and
//     whether it is the one this release was built against;
//   - a synthetic profile whose conductor passphrase and zome-call signing
//     credentials are generated here and kept only in the home's sealed store;
//   - start, status and a consistent stop of that conductor, with every secret
//     travelling on a pipe and never in an argument or the environment;
//   - a sealed checkpoint of the conductor's own state, and a restore of it
//     onto a fresh runtime path;
//   - the guards that keep one identity from being written by two conductors,
//     and a retired runtime or a stale snapshot from being resumed.
//
// The App API is reached through one bridge process built on Holochain's
// official Rust client, spoken to over pipes. `hc client zome-call` takes its
// payload as a process argument, which would put a private body in `ps`; the
// bridge exists so that nothing sensitive is ever an argument.
package native

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"rokh-home/store"
)

// Formats and the engine this release was built against.
const (
	EngineFormat   = "rokh.native-engine/1"
	ProfileFormat  = "rokh.native-profile/1"
	SnapshotFormat = "rokh.native-snapshot/1"
	RuntimeFormat  = "rokh.native-runtime/1"

	ptrProfile  = "native/profile/"
	ptrIndex    = "native/profiles"
	kindSnap    = "native-snapshot"
	runtimeFile = "runtime.json"
	lockFile    = "runtime.lock"
)

// Errors the gate turns into typed answers rather than bare failures.
var (
	ErrNoEngine      = errors.New("native: no Holochain conductor is available on this host")
	ErrIncompatible  = errors.New("native: the conductor on this host is not the release this home was built against")
	ErrLive          = errors.New("native: this identity already has a live conductor")
	ErrNotLive       = errors.New("native: no conductor is running for this profile")
	ErrStale         = errors.New("native: that runtime was retired by a later checkpoint")
	ErrNoSnapshot    = errors.New("native: this profile has no complete checkpoint")
	ErrNotQuiesced   = errors.New("native: a checkpoint is taken from a stopped conductor, never an open database")
	ErrUnknownEnding = errors.New("native: the conductor's ending was not learned")
	// ErrClosing is a start or a call refused because this manager is shutting
	// down. It exists so that work racing a close cannot leave a conductor
	// behind after the sessions have been collected, and so that a call that
	// arrived after the drain is refused rather than slipping in behind it.
	ErrClosing = errors.New("native: this home is closing and takes no new native work")
	// ErrStopping is work refused because this profile's conductor is being
	// stopped. The drain a stop does is only worth anything if what arrives
	// during it is turned away.
	ErrStopping = errors.New("native: this conductor is stopping and takes no new work")
	// ErrNotSent is a request that never left this process. It is the one
	// failure that says the chain is certainly untouched.
	ErrNotSent = errors.New("native: the request was not sent")
)

// Sealer is the part of a home's sealed store this manager uses. Secrets,
// manifests and checkpoints go through it and nowhere else.
type Sealer interface {
	Pointer(name string) ([]byte, bool, error)
	SetPointer(name string, value []byte) error
	Put(kind, id string, r io.Reader, wantSHA256 []byte) (store.Info, error)
	Get(kind, id string) (io.ReadCloser, error)
	Has(kind, id string) (bool, error)
	Health() error
}

// Binaries are the host's executables. Paths only; nothing is copied.
type Binaries struct {
	Conductor string `json:"conductor"`
	CLI       string `json:"cli"`
	Bridge    string `json:"bridge"`
	HApp      string `json:"happ"`
	// Manifest is the engine's own description: its releases, application,
	// functions and files. Nothing of it is in this package's code.
	Manifest Manifest `json:"-"`
}

// Engine is what this host offers of the native engine, and on what terms.
// It confers no authority over home content: that is the owner's registry.
type Engine struct {
	Format     string `json:"format"`
	Present    bool   `json:"present"`
	Compatible bool   `json:"compatible"`
	Reason     string `json:"reason,omitempty"`

	Conductor        string `json:"conductor,omitempty"`
	ConductorVersion string `json:"conductor_version,omitempty"`
	ConductorSHA256  string `json:"conductor_sha256,omitempty"`
	CLI              string `json:"cli,omitempty"`
	CLIVersion       string `json:"cli_version,omitempty"`
	CLISHA256        string `json:"cli_sha256,omitempty"`
	Bridge           string `json:"bridge,omitempty"`
	BridgeSHA256     string `json:"bridge_sha256,omitempty"`
	HApp             string `json:"happ,omitempty"`
	HAppSHA256       string `json:"happ_sha256,omitempty"`

	Want     string `json:"want"`
	WantCLI  string `json:"want_cli"`
	Platform string `json:"platform"`

	// The host's own capacity, reported so a caller need not guess it, and the
	// ceiling this manager will actually use.
	CPUs        int `json:"cpus"`
	Concurrency int `json:"concurrency"`
}

// ChainHead is the conductor's own word about where a source chain ends.
type ChainHead struct {
	Seq    uint32 `json:"seq"`
	Action string `json:"action"`
}

// Snapshot is one sealed checkpoint of a conductor's state.
type Snapshot struct {
	Format     string      `json:"format"`
	Attempt    string      `json:"attempt"`
	Generation uint64      `json:"generation"`
	Object     string      `json:"object"`
	Bytes      int64       `json:"bytes"`
	SHA256     string      `json:"sha256"`
	Files      []FileEntry `json:"files"`
	Head       ChainHead   `json:"head"`
	AgentKey   string      `json:"agent_key"`
	DNAHash    string      `json:"dna_hash"`
	AppID      string      `json:"app_id"`
	Engine     string      `json:"engine"`
	TakenUTC   string      `json:"taken_utc"`
	Complete   bool        `json:"complete"`
}

// FileEntry is one file inside a checkpoint, with the hash it was sealed at.
type FileEntry struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256"`
}

// Live is the conductor this profile has running, as the broker recorded it.
type Live struct {
	PID        int    `json:"pid"`
	RuntimeDir string `json:"runtime_dir"`
	Generation uint64 `json:"generation"`
	AdminPort  int    `json:"admin_port"`
	StartedUTC string `json:"started_utc"`
	Origin     string `json:"origin"`
}

// Profile is one synthetic native identity of this home. It is kept as a
// sealed pointer: the passphrase and the signing credentials in it never touch
// an unsealed file, an argument or the environment.
type Profile struct {
	Format     string `json:"format"`
	ID         string `json:"id"`
	Label      string `json:"label"`
	AppID      string `json:"app_id"`
	CreatedUTC string `json:"created_utc"`
	Generation uint64 `json:"generation"`

	ConductorPassphrase []byte          `json:"conductor_passphrase"`
	Credentials         json.RawMessage `json:"credentials,omitempty"`

	AgentKey   string `json:"agent_key,omitempty"`
	DNAHash    string `json:"dna_hash,omitempty"`
	HAppSHA256 string `json:"happ_sha256,omitempty"`
	Engine     string `json:"engine,omitempty"`

	Snapshot *Snapshot `json:"snapshot,omitempty"`
	Live     *Live     `json:"live,omitempty"`

	// Where this identity's conductor state is now, which folders a checkpoint
	// has retired, whether the last ending was a consistent one, and the chain
	// head read off the conductor just before it stopped.
	RuntimeDir string   `json:"runtime_dir,omitempty"`
	Retired    []string `json:"retired,omitempty"`
	Quiesced   bool     `json:"quiesced"`
	// HeadRead says whether the head recorded below was actually read off the
	// conductor at the last stop, rather than left over from an earlier one.
	HeadRead bool           `json:"head_read"`
	LastStop map[string]any `json:"last_stop,omitempty"`
	LastHead ChainHead      `json:"last_head"`

	// Network is the test network this profile joins. It is explicit, never a
	// default that would reach a public bootstrap.
	Bootstrap string `json:"bootstrap"`
	Relay     string `json:"relay"`
}

// View is a profile with every secret removed, for answers and evidence.
func (p *Profile) View() map[string]any {
	v := map[string]any{
		"format": p.Format, "id": p.ID, "label": p.Label, "app_id": p.AppID,
		"created_utc": p.CreatedUTC, "generation": p.Generation,
		"agent_key": p.AgentKey, "dna_hash": p.DNAHash, "happ_sha256": p.HAppSHA256,
		"engine": p.Engine, "bootstrap": p.Bootstrap, "relay": p.Relay,
		"runtime_dir": p.RuntimeDir, "retired": p.Retired, "quiesced": p.Quiesced,
		"head_read": p.HeadRead,
		"last_head": p.LastHead, "last_stop": p.LastStop,
		"secrets": map[string]any{
			"conductor_passphrase":  secretNote(len(p.ConductorPassphrase) > 0),
			"zome_call_credentials": secretNote(len(p.Credentials) > 0),
			"kept_in":               "the home's sealed store, never a file, an argument or the environment",
		},
	}
	if p.Snapshot != nil {
		v["snapshot"] = p.Snapshot
	}
	if p.Live != nil {
		v["live"] = p.Live
	}
	return v
}

// pendingSeal says this identity has conductor state on disk that the sealed
// snapshot does not stand for.
//
// It is what makes a second put-away real. A stop takes the session out of this
// process before the checkpoint runs, so after a checkpoint failed there is
// nothing live left to find and a put-away that looked only at live sessions
// would report a clean close over an unsealed chain. The sealed record outlives
// the process, so this is also what a reopened broker sees: an identity stopped
// but never sealed is picked up and sealed on the next attempt.
func pendingSeal(p *Profile) bool {
	switch {
	case p == nil || p.RuntimeDir == "":
		return false
	case p.Snapshot == nil || !p.Snapshot.Complete:
		return true
	case p.Snapshot.Generation != p.Generation:
		return true
	case p.Snapshot.Head != p.LastHead:
		return true
	case !p.HeadRead:
		// The head on record was not read off this conductor, so nothing can be
		// said about whether the snapshot stands for what is on disk.
		return true
	}
	return false
}

func secretNote(present bool) string {
	if present {
		return "held sealed; not recorded"
	}
	return "none yet"
}

// Manager is one home's runtime manager. It is part of the broker, not a
// program the gate serves.
type Manager struct {
	mu   sync.Mutex
	cond *sync.Cond
	seal Sealer
	bin  Binaries
	live map[string]*session // by profile id, for this process
	// closing is set before the live sessions are collected. Start and new work
	// refuse once it is set, so anything that began after the collection cannot
	// leave a conductor running behind the shutdown.
	closing bool
	// quiescing is the put-away that is under way, if any. A second caller joins
	// it and is given the same report rather than a fresh, empty one: two closes
	// racing must not let the second declare a home closed over a failure the
	// first one found, nor return before the first has finished.
	quiescing *putAway
	// ceiling is the host's limit on conductors, set by the gate from the same
	// offer that bounds programs. A manager with none refuses nothing on that
	// account; a manager with one asks before every start and gives the slot
	// back when the conductor is gone.
	ceiling Ceiling
}

// putAway is one run of Quiesce, and the place the callers that joined it wait.
type putAway struct {
	done   chan struct{}
	report map[string]any
}

// Ceiling is what the host allows this manager. It is supplied by the gate, so
// a conductor is counted against exactly the same offer a program is, and it is
// asked before a start does anything at all.
type Ceiling struct {
	// Take reserves one slot for this profile's conductor. It answers an error
	// when the host offers no network, when the ceiling is full, or when the
	// offer has been withdrawn or has expired. commit keeps the slot for as long
	// as the conductor runs; release gives it back when the start failed.
	Take func(profile string) (commit func(), release func(), err error)
	// Give returns the slot a running conductor held.
	Give func(profile string)
}

// New makes a manager over a home's sealed store.
func New(seal Sealer, bin Binaries) *Manager {
	m := &Manager{seal: seal, bin: bin, live: map[string]*session{}}
	m.cond = sync.NewCond(&m.mu)
	return m
}

// UnderCeiling gives this manager the host's limit on conductors. The gate sets
// it when it enables the engine; nothing else may.
func (m *Manager) UnderCeiling(c Ceiling) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ceiling = c
}

// Closing says whether this manager has stopped taking new native work.
func (m *Manager) Closing() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closing
}

// Concurrency is the ceiling this manager uses for its own parallel work. It
// is taken from the host rather than fixed, and never less than one.
func Concurrency() int {
	n := runtime.NumCPU()
	switch {
	case n <= 1:
		return 1
	case n > 8:
		return 8
	default:
		return n
	}
}

// Offer probes the host: which binaries are there, at which versions, and
// whether they are the release this home was built against. A missing or
// mismatched engine is a named answer, not a failure.
func (m *Manager) Offer() Engine {
	man := m.bin.Manifest
	e := Engine{Format: EngineFormat, Want: man.Conductor.Version, WantCLI: man.CLI.Version,
		Platform: runtime.GOOS + "/" + runtime.GOARCH, CPUs: runtime.NumCPU(), Concurrency: Concurrency()}
	e.Conductor, e.CLI, e.Bridge, e.HApp = m.bin.Conductor, m.bin.CLI, m.bin.Bridge, m.bin.HApp
	missing := []string{}
	for name, path := range map[string]string{"conductor": m.bin.Conductor, "cli": m.bin.CLI,
		"bridge": m.bin.Bridge, "happ": m.bin.HApp} {
		if path == "" {
			missing = append(missing, name)
			continue
		}
		if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		e.Reason = "not on this host: " + strings.Join(sorted(missing), ", ")
		return e
	}
	e.ConductorSHA256, _ = fileSHA(m.bin.Conductor)
	e.CLISHA256, _ = fileSHA(m.bin.CLI)
	e.BridgeSHA256, _ = fileSHA(m.bin.Bridge)
	e.HAppSHA256, _ = fileSHA(m.bin.HApp)
	e.Present = true
	e.ConductorVersion = firstLineWith(runVersion(m.bin.Conductor), man.VersionPrefix)
	e.CLIVersion = firstLineWith(runVersion(m.bin.CLI), man.VersionPrefix)
	switch {
	case man.Conductor.Version == "":
		e.Reason = "no engine manifest names the release this host must have"
	case e.ConductorVersion != man.Conductor.Version:
		e.Reason = fmt.Sprintf("the engine manifest names %q; the host has %q", man.Conductor.Version, e.ConductorVersion)
	case e.CLIVersion != man.CLI.Version:
		e.Reason = fmt.Sprintf("the engine manifest names %q; the host has %q", man.CLI.Version, e.CLIVersion)
	default:
		e.Compatible = true
	}
	return e
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func runVersion(bin string) string {
	cmd := exec.Command(bin, "--version")
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// The conductor prints a logging banner before its version.
func firstLineWith(out, prefix string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return strings.TrimSpace(out)
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// ---------- sealed profile records ----------

func (m *Manager) loadProfile(id string) (*Profile, error) {
	b, ok, err := m.seal.Pointer(ptrProfile + id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("native: no profile %q", id)
	}
	var p Profile
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if p.Format != ProfileFormat {
		return nil, fmt.Errorf("native: unsupported profile format %q", p.Format)
	}
	return &p, nil
}

func (m *Manager) saveProfile(p *Profile) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return m.seal.SetPointer(ptrProfile+p.ID, b)
}

func (m *Manager) index() ([]string, error) {
	b, ok, err := m.seal.Pointer(ptrIndex)
	if err != nil || !ok {
		return nil, err
	}
	var ids []string
	return ids, json.Unmarshal(b, &ids)
}

func (m *Manager) addToIndex(id string) error {
	ids, err := m.index()
	if err != nil {
		return err
	}
	for _, existing := range ids {
		if existing == id {
			return nil
		}
	}
	ids = append(ids, id)
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return m.seal.SetPointer(ptrIndex, b)
}

// List names every profile this home holds, without their secrets.
func (m *Manager) List() ([]map[string]any, error) {
	ids, err := m.index()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, id := range ids {
		p, err := m.loadProfile(id)
		if err != nil {
			out = append(out, map[string]any{"id": id, "error": err.Error()})
			continue
		}
		out = append(out, p.View())
	}
	return out, nil
}

// ---------- runtime stamp ----------

// stamp is the note a runtime folder carries about which profile and which
// generation it belongs to. It holds no secret, and it is what makes a retired
// runtime refuse to be resumed.
type stamp struct {
	Format     string `json:"format"`
	Profile    string `json:"profile"`
	Generation uint64 `json:"generation"`
	AppID      string `json:"app_id"`
	WrittenUTC string `json:"written_utc"`
}

func writeStamp(dir string, s stamp) error {
	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, runtimeFile), append(b, '\n'))
}

func readStamp(dir string) (stamp, bool, error) {
	var s stamp
	b, err := os.ReadFile(filepath.Join(dir, runtimeFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, false, nil
		}
		return s, false, err
	}
	return s, true, json.Unmarshal(b, &s)
}

func atomicWrite(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	return os.Rename(name, path)
}
