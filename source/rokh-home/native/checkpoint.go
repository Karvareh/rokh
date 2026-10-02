package native

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Files a runtime keeps for this process alone, which are not part of the
// conductor's state and are not carried.
var notState = map[string]bool{lockFile: true, "conductor.log": true, "bridge.log": true}

// Checkpoint seals the conductor's own state into the home.
//
// It is taken from a stopped conductor and from nothing else: an open SQLite
// file is not a backup, so a live process refuses here rather than producing
// something that would restore into a corrupt chain. The sealed object is
// written and read back before the profile moves to the new generation, so a
// failure anywhere leaves the old state exactly as it was.
func (m *Manager) Checkpoint(profileID, attempt string) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.seal.Health(); err != nil {
		return nil, err
	}
	if attempt == "" {
		return nil, errors.New("native: a checkpoint is taken under a named attempt")
	}
	p, err := m.loadProfile(profileID)
	if err != nil {
		return nil, err
	}
	if s := m.live[profileID]; s != nil {
		return map[string]any{"live": p.Live}, ErrNotQuiesced
	}
	if p.Live != nil && processAlive(p.Live.PID) {
		return map[string]any{"live": p.Live}, ErrNotQuiesced
	}
	if p.RuntimeDir == "" {
		return nil, errors.New("native: this profile has no runtime to checkpoint")
	}
	if !p.Quiesced {
		return map[string]any{"last_stop": p.LastStop}, fmt.Errorf(
			"%w: the last ending of this conductor was not a consistent one", ErrNotQuiesced)
	}

	// The same attempt twice is the checkpoint already taken, not a second one.
	if p.Snapshot != nil && p.Snapshot.Attempt == attempt && p.Snapshot.Complete {
		return map[string]any{"snapshot": p.Snapshot, "already": true, "generation": p.Generation}, nil
	}

	next := p.Generation + 1
	// The object is named after the attempt, not the generation.
	//
	// Objects are write-once: the same name with different bytes is a mismatch.
	// If the profile could not be saved after an object was sealed, the identity
	// is still at the old generation, and a generation-named object would make
	// every later attempt collide with the one already there — permanently, as
	// soon as anything in the runtime folder differed. Naming by attempt keeps
	// the property that matters (one attempt is one version, and repeating it
	// can never produce a different one) and lets a fresh attempt proceed.
	object := fmt.Sprintf("%s/%s", p.ID, attemptKey(p.ID, attempt))

	// The same lock Start takes. Without it, another broker could bring this
	// folder up between the liveness check above and the read below, and the
	// tar would be read out of an open database — the one thing a checkpoint
	// must never be.
	lock, err := holdRuntime(p.RuntimeDir)
	if err != nil {
		return map[string]any{"runtime_dir": p.RuntimeDir}, fmt.Errorf(
			"%w: the runtime folder is held by another process: %v", ErrNotQuiesced, err)
	}
	defer lock.Close()

	files, skipped, err := listRuntime(p.RuntimeDir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("native: the runtime folder holds no state")
	}

	// Seal the tar as one stream. store.Put hashes what it wrote and syncs the
	// publication; an error here means nothing was activated.
	pr, pw := io.Pipe()
	entriesCh := make(chan []FileEntry, 1)
	errCh := make(chan error, 1)
	go func() {
		entries, err := writeTar(pw, p.RuntimeDir, files)
		entriesCh <- entries
		errCh <- err
		pw.CloseWithError(err)
	}()
	info, putErr := m.seal.Put(kindSnap, object, pr, nil)
	// Put may stop reading early — a mismatch against an object already there
	// does exactly that. The writer is still filling the pipe, and waiting for
	// it before the read end is closed would wait forever.
	pr.CloseWithError(putErr)
	entries := <-entriesCh
	tarErr := <-errCh
	// The seal is asked about first. When it refuses early, closing the read end
	// is what stops the writer, so the writer's own error is that closure and
	// not a cause: reporting it would name the wrong thing.
	if putErr != nil {
		return nil, fmt.Errorf("native: the checkpoint was not sealed: %w", putErr)
	}
	if tarErr != nil {
		return nil, fmt.Errorf("native: the checkpoint could not be read off disk: %w", tarErr)
	}

	// Read the sealed stream back before anything is declared. A name on disk
	// is not evidence that a whole authenticated stream is there.
	verified, err := m.verifyObject(object)
	if err != nil {
		return map[string]any{"object": object}, fmt.Errorf("native: the sealed checkpoint did not read back: %w", err)
	}
	if verified.Size != info.Size || verified.SHA256 != hex.EncodeToString(info.SHA256[:]) {
		return map[string]any{"object": object, "written": hex.EncodeToString(info.SHA256[:]), "read_back": verified.SHA256},
			errors.New("native: the sealed checkpoint does not read back as it was written")
	}

	if !p.HeadRead {
		// The head is what a restored chain is checked against. Saying it was
		// read when it was not would make a later comparison meaningless.
		return map[string]any{"object": object, "sealed": true, "activated": false},
			fmt.Errorf("%w: the last stop could not read this chain's head, so a checkpoint would name a head nobody saw",
				ErrUnknownEnding)
	}
	snap := &Snapshot{Format: SnapshotFormat, Attempt: attempt, Generation: next, Object: object,
		Bytes: info.Size, SHA256: hex.EncodeToString(info.SHA256[:]), Files: entries,
		Head: p.LastHead, AgentKey: p.AgentKey, DNAHash: p.DNAHash, AppID: p.AppID,
		Engine: p.Engine, TakenUTC: nowUTC(), Complete: true}

	// Only now does the identity move on. Until this pointer is written the old
	// generation stands, and the runtime folder it belongs to is still its own.
	previous := p.Generation
	p.Snapshot, p.Generation = snap, next
	p.Retired = append(p.Retired, p.RuntimeDir)
	if err := m.saveProfile(p); err != nil {
		return map[string]any{"object": object, "sealed": true, "activated": false},
			fmt.Errorf("native: the checkpoint is sealed but the profile did not move to it: %w", err)
	}
	return map[string]any{"snapshot": snap, "generation": next, "previous_generation": previous,
		"retired_runtime": p.RuntimeDir, "already": false, "not_state": skipped}, nil
}

// attemptKey names the sealed object of one attempt on one profile. The attempt
// text itself is not a path component: it is the caller's, and a path is not a
// place to put somebody else's string.
func attemptKey(profile, attempt string) string {
	h := sha256.New()
	h.Write([]byte("rokh.native-checkpoint/1\x00"))
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write([]byte(attempt))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

type objectInfo struct {
	Size   int64
	SHA256 string
}

func (m *Manager) verifyObject(id string) (objectInfo, error) {
	var out objectInfo
	r, err := m.seal.Get(kindSnap, id)
	if err != nil {
		return out, err
	}
	defer r.Close()
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return out, err
	}
	out.Size, out.SHA256 = n, hex.EncodeToString(h.Sum(nil))
	return out, nil
}

// listRuntime names the regular files that are this conductor's state, and
// separately what was left behind: the keystore's own socket and this
// process's log and lock are made again on the next start and are not state.
func listRuntime(root string) ([]string, []string, error) {
	var out, skipped []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if notState[rel] || strings.HasPrefix(rel, ".tmp-") {
			skipped = append(skipped, rel)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			skipped = append(skipped, rel+" ("+d.Type().String()+")")
			return nil
		}
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	sort.Strings(skipped)
	return out, skipped, err
}

func writeTar(w io.WriteCloser, root string, files []string) ([]FileEntry, error) {
	tw := tar.NewWriter(w)
	entries := make([]FileEntry, 0, len(files))
	for _, rel := range files {
		path := filepath.Join(root, rel)
		fi, err := os.Lstat(path)
		if err != nil {
			return entries, err
		}
		hdr := &tar.Header{Name: rel, Mode: int64(fi.Mode().Perm()), Size: fi.Size(), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			return entries, err
		}
		f, err := os.Open(path)
		if err != nil {
			return entries, err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(tw, h), f)
		f.Close()
		if err != nil {
			return entries, err
		}
		if n != fi.Size() {
			return entries, fmt.Errorf("native: %s changed size while it was being sealed", rel)
		}
		entries = append(entries, FileEntry{Path: rel, Bytes: n, Mode: uint32(fi.Mode().Perm()),
			SHA256: hex.EncodeToString(h.Sum(nil))})
	}
	if err := tw.Close(); err != nil {
		return entries, err
	}
	return entries, nil
}

// Restore opens this identity's current checkpoint onto a fresh runtime path.
//
// An older checkpoint is refused by generation, not by hope: a snapshot from
// before the last one would put the identity back behind work its peers have
// already seen. The restored folder is stamped with the generation it is, so
// the start guard sees the same thing.
func (m *Manager) Restore(profileID, runtimeDir string) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.seal.Health(); err != nil {
		return nil, err
	}
	p, err := m.loadProfile(profileID)
	if err != nil {
		return nil, err
	}
	if s := m.live[profileID]; s != nil {
		return map[string]any{"live": p.Live}, ErrLive
	}
	if p.Live != nil && processAlive(p.Live.PID) {
		return map[string]any{"live": p.Live}, ErrLive
	}
	if p.Snapshot == nil || !p.Snapshot.Complete {
		return nil, ErrNoSnapshot
	}
	if p.Snapshot.Generation != p.Generation {
		return map[string]any{"snapshot_generation": p.Snapshot.Generation, "generation": p.Generation}, ErrStale
	}
	dir, err := filepath.Abs(runtimeDir)
	if err != nil {
		return nil, err
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return map[string]any{"runtime_dir": dir}, errors.New("native: restore onto a fresh folder, not one that already holds something")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	r, err := m.seal.Get(kindSnap, p.Snapshot.Object)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	h := sha256.New()
	restored, err := readTar(io.TeeReader(r, h), dir)
	if err != nil {
		return map[string]any{"runtime_dir": dir}, err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != p.Snapshot.SHA256 {
		return map[string]any{"want": p.Snapshot.SHA256, "got": got},
			errors.New("native: the checkpoint's bytes are not the ones it was sealed with")
	}
	// Every file is checked against the manifest, not just the stream as a whole.
	want := map[string]FileEntry{}
	for _, e := range p.Snapshot.Files {
		want[e.Path] = e
	}
	if len(restored) != len(want) {
		return map[string]any{"restored": len(restored), "expected": len(want)},
			errors.New("native: the checkpoint restored a different set of files than its manifest names")
	}
	for _, e := range restored {
		w, ok := want[e.Path]
		if !ok || w.SHA256 != e.SHA256 || w.Bytes != e.Bytes {
			return map[string]any{"path": e.Path}, errors.New("native: a restored file does not match the manifest")
		}
	}
	// The conductor's config names absolute paths of the folder it was in.
	// Rewriting it here is what lets the old folder be genuinely absent.
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	origin := "rokh-native-" + p.ID
	if err := writeConductorConfig(dir, p, port, origin); err != nil {
		return nil, err
	}
	rewritten, stillNamed, ambiguous, err := rewritePaths(dir, p.Retired, m.configFiles())
	if err != nil {
		return nil, err
	}
	if err := writeStamp(dir, stamp{Format: RuntimeFormat, Profile: p.ID, Generation: p.Generation,
		AppID: p.AppID, WrittenUTC: nowUTC()}); err != nil {
		return nil, err
	}
	p.RuntimeDir = dir
	p.Quiesced = false
	// The conductor this profile last had is gone; its record is not part of
	// what was restored, and leaving it would put a dead pid in every answer.
	p.Live = nil
	// The head the restored chain must match is the checkpoint's own, and the
	// identity is the one that was sealed with it.
	p.LastHead, p.HeadRead = p.Snapshot.Head, true
	p.AgentKey, p.DNAHash, p.AppID = p.Snapshot.AgentKey, p.Snapshot.DNAHash, p.Snapshot.AppID
	if err := m.saveProfile(p); err != nil {
		return nil, err
	}
	out := map[string]any{"runtime_dir": dir, "generation": p.Generation, "files": len(restored),
		"snapshot": p.Snapshot.Object, "sha256": p.Snapshot.SHA256,
		"head": p.Snapshot.Head, "agent_key": p.Snapshot.AgentKey, "dna_hash": p.Snapshot.DNAHash,
		"live_cleared":    true,
		"paths_rewritten": withGenerated(rewritten)}
	out["paths_still_named"] = stillNamed
	if len(stillNamed) > 0 {
		out["paths_still_named_note"] = "these are not this release's configuration and were left exactly as they " +
			"were sealed; look at them before starting the conductor on this folder"
	}
	if len(ambiguous) > 0 {
		out["paths_not_reported_on"] = ambiguous
		out["paths_not_reported_on_note"] = "this folder's path extends these retired ones, so a mention of either " +
			"cannot be told from a mention of this one; they were rewritten exactly, and not reported on"
	}
	return out, nil
}

func readTar(r io.Reader, dir string) ([]FileEntry, error) {
	tr := tar.NewReader(r)
	out := []FileEntry{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out, err
		}
		if hdr.Typeflag != tar.TypeReg {
			return out, fmt.Errorf("native: %s is not a regular file", hdr.Name)
		}
		clean := filepath.Clean(hdr.Name)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return out, fmt.Errorf("native: %q escapes the folder it is restored into", hdr.Name)
		}
		path := filepath.Join(dir, clean)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return out, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(hdr.Mode).Perm()&0o700)
		if err != nil {
			return out, err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(f, h), tr)
		if syncErr := f.Sync(); err == nil {
			err = syncErr
		}
		closeErr := f.Close()
		if err != nil {
			return out, err
		}
		if closeErr != nil {
			return out, closeErr
		}
		out = append(out, FileEntry{Path: clean, Bytes: n, Mode: uint32(os.FileMode(hdr.Mode).Perm()),
			SHA256: hex.EncodeToString(h.Sum(nil))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// releaseConfigFiles are the configuration files this release's own runtime
// writes, named exactly, relative to the runtime folder.
//
// Only these are rewritten. A suffix is not a licence: the keystore's store and
// the conductor's databases lie in the same tree, some of them under names that
// end in .json or .yaml, and a path replaced inside one of those would be
// replaced by one of a different length. A store of the wrong length is a
// keystore that fails at the next start with nothing to point at the cause.
// Anything outside this list that still names a retired folder is reported and
// left exactly as it was sealed.
// configFiles are the files the engine manifest names as its runtime's own
// configuration.
func (m *Manager) configFiles() map[string]bool {
	out := map[string]bool{}
	for _, f := range m.bin.Manifest.ReleaseConfigFiles {
		out[f] = true
	}
	return out
}

// generatedOnRestore are the two of those a restore writes fresh for the folder
// it is restoring into, before rewritePaths runs: the conductor's configuration
// and the runtime's own stamp. They never appear in the rewritten list, and
// would otherwise go unreported.
var generatedOnRestore = []string{"conductor.yaml", "runtime.json"}

// rewritePaths replaces an earlier runtime path inside this release's own
// configuration files, and reports every other file that still names one.
//
// That second list is not an error here — a vestigial name inside a store may
// never be read — but it is the one thing an operator has to see before the
// conductor is started on the restored folder, rather than after it fails.
func rewritePaths(dir string, retired []string, releaseConfigFiles map[string]bool) (rewritten, stillNamed, ambiguous []string, err error) {
	rewritten, stillNamed, ambiguous = []string{}, []string{}, []string{}
	all := make([]string, 0, len(retired))
	names := make([]string, 0, len(retired))
	for _, old := range retired {
		if old == "" || old == dir {
			continue
		}
		all = append(all, old)
		// A retired folder whose path the new one extends — /run-a and
		// /run-a-b — cannot be told apart from the new one by looking at bytes:
		// every mention of the new folder contains the old one. Rewriting is
		// still exact, because a replacement is not scanned again; but reporting
		// on it would name files that only carry the new path. It is said as a
		// limit instead of guessed at.
		if strings.HasPrefix(dir, old) {
			ambiguous = append(ambiguous, old)
			continue
		}
		names = append(names, old)
	}
	sort.Strings(ambiguous)
	if len(all) == 0 {
		return rewritten, stillNamed, ambiguous, nil
	}
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if !releaseConfigFiles[rel] {
			if len(names) == 0 {
				return nil
			}
			// Every other file is only looked at, and only to say whether it
			// still names a folder that is gone. The look is a streamed one:
			// these are the conductor's databases, and reading one into memory
			// to search it would be a cost taken for nothing.
			found, scanErr := fileContainsAny(path, names)
			if scanErr != nil {
				return scanErr
			}
			if found {
				stillNamed = append(stillNamed, rel)
			}
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(b)
		replaced := false
		for _, old := range all {
			if strings.Contains(text, old) {
				text = strings.ReplaceAll(text, old, dir)
				replaced = true
			}
		}
		if !replaced {
			return nil
		}
		if err := atomicWrite(path, []byte(text)); err != nil {
			return err
		}
		rewritten = append(rewritten, rel)
		return nil
	})
	sort.Strings(rewritten)
	sort.Strings(stillNamed)
	return rewritten, stillNamed, ambiguous, err
}

// fileContainsAny streams a file looking for any of these strings, keeping
// enough of each chunk to catch one that straddles a boundary.
func fileContainsAny(path string, needles []string) (bool, error) {
	longest := 0
	for _, n := range needles {
		if len(n) > longest {
			longest = len(n)
		}
	}
	if longest == 0 {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
	const chunk = 1 << 20
	buf := make([]byte, chunk+longest)
	carry := 0
	for {
		n, readErr := io.ReadFull(f, buf[carry:carry+chunk])
		if n > 0 {
			window := buf[:carry+n]
			for _, needle := range needles {
				if bytes.Contains(window, []byte(needle)) {
					return true, nil
				}
			}
			if carry+n > longest {
				copy(buf, window[carry+n-longest:])
				carry = longest
			} else {
				carry = carry + n
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
				return false, nil
			}
			return false, readErr
		}
	}
}

// withGenerated names the files a restore wrote itself among the ones it
// rewrote, so the answer accounts for every file in the folder that is not
// byte for byte what was sealed.
func withGenerated(rewritten []string) []string {
	out := append([]string(nil), generatedOnRestore...)
	for _, name := range rewritten {
		already := false
		for _, seen := range out {
			if seen == name {
				already = true
				break
			}
		}
		if !already {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
