package native

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ManifestFormat names the engine manifest's format.
const ManifestFormat = "rokh.engine-manifest/1"

// Manifest is an engine's own description, given by the host at start
// (contract B6). Rokh holds no engine's name, version, application id, file
// name or function name in code: they are read from here, and replacing an
// engine is replacing the manifest.
type Manifest struct {
	Format string `json:"format"`
	// Name is the engine's display name; Namespace the ledger namespace its
	// receipts are written under.
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	// VersionPrefix begins the line of `--version` output that names the
	// release; Conductor.Version and CLI.Version are that whole line.
	VersionPrefix string `json:"version_prefix"`
	Conductor     Part   `json:"conductor"`
	CLI           Part   `json:"cli"`
	Bridge        Part   `json:"bridge"`
	App           App    `json:"app"`
	Functions     Funcs  `json:"functions"`
	// ReleaseConfigFiles are the configuration files the engine's runtime
	// writes, relative to its runtime folder, rewritten on restore.
	ReleaseConfigFiles []string `json:"release_config_files"`
}

// Part is one file of the engine, and the release it must report.
type Part struct {
	File    string `json:"file"`
	Version string `json:"version,omitempty"`
}

// App is the application the engine runs for this home.
type App struct {
	File string `json:"file"`
	ID   string `json:"id"`
	Zome string `json:"zome"`
}

// Funcs are the application's functions by role.
type Funcs struct {
	Publish     string   `json:"publish"`
	ChainHead   string   `json:"chain_head"`
	FindAttempt string   `json:"find_attempt"`
	Read        []string `json:"read"`
	Capability  []string `json:"capability"`
	Peer        []string `json:"peer"`
	Admin       []string `json:"admin"`
}

func listed(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (m Manifest) check() error {
	if m.Format != ManifestFormat {
		return fmt.Errorf("native: manifest format %q, want %q", m.Format, ManifestFormat)
	}
	for what, v := range map[string]string{"name": m.Name, "namespace": m.Namespace,
		"version_prefix": m.VersionPrefix, "conductor.file": m.Conductor.File,
		"conductor.version": m.Conductor.Version, "cli.file": m.CLI.File, "cli.version": m.CLI.Version,
		"bridge.file": m.Bridge.File, "app.file": m.App.File, "app.id": m.App.ID, "app.zome": m.App.Zome,
		"functions.publish": m.Functions.Publish, "functions.chain_head": m.Functions.ChainHead,
		"functions.find_attempt": m.Functions.FindAttempt} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("native: the engine manifest does not name %s", what)
		}
	}
	for _, f := range []string{m.Conductor.File, m.CLI.File, m.Bridge.File, m.App.File} {
		if filepath.Base(f) != f {
			return errors.New("native: an engine file is named inside the engine's folder, not by a path")
		}
	}
	return nil
}

// ParseManifest reads a manifest and checks it names everything.
func ParseManifest(b []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("native: the engine manifest does not read: %w", err)
	}
	return m, m.check()
}

// LoadEngine reads the manifest at path and resolves its files inside dir.
func LoadEngine(dir, path string) (Binaries, error) {
	if path == "" {
		path = filepath.Join(dir, "engine.json")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Binaries{}, err
	}
	m, err := ParseManifest(b)
	if err != nil {
		return Binaries{}, err
	}
	return Binaries{
		Conductor: filepath.Join(dir, m.Conductor.File),
		CLI:       filepath.Join(dir, m.CLI.File),
		Bridge:    filepath.Join(dir, m.Bridge.File),
		HApp:      filepath.Join(dir, m.App.File),
		Manifest:  m,
	}, nil
}

// Manifest is the engine manifest this manager was started with.
func (m *Manager) Manifest() Manifest { return m.bin.Manifest }
