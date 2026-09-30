package content

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"rokh/canon"
	"rokh/frame"
)

// MediaTypeTree marks a tree manifest: the canonical listing of a folder.
//
// A folder import produces exactly one manifest blob plus one blob per file,
// and one descriptor event referencing the manifest. Identity is content only:
// sorted relative paths, sizes and hashes. Symlinks and special files are
// refused, and OS permissions and times are deliberately not part of identity,
// because two machines must agree on what a folder *is* without agreeing on
// how their filesystems dress it.
const MediaTypeTree = "application/vnd.rokh.tree+json"

// MaxTreeEntries bounds a manifest. A hundred thousand files is already a
// filesystem, not a folder.
const MaxTreeEntries = 100000

// TreeEntry is one file in a tree manifest.
type TreeEntry struct {
	Path string   `json:"path"`
	Size uint64   `json:"size"`
	Hash frame.ID `json:"-"`

	// HashHex is what actually serializes; Hash is the typed view.
	HashHex string `json:"hash"`
}

// Tree is a canonical folder listing.
type Tree struct {
	Version int         `json:"rokh_tree"`
	Entries []TreeEntry `json:"entries"`
}

// ValidTreePath checks one manifest path: relative, "/"-separated, no dot
// segments, no backslash, valid UTF-8.
func ValidTreePath(p string) error {
	if p == "" || len(p) > 4096 {
		return fmt.Errorf("%w: tree path length", ErrShape)
	}
	if !utf8.ValidString(p) {
		return fmt.Errorf("%w: tree path is not valid UTF-8", ErrShape)
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return fmt.Errorf("%w: tree path must be relative with \"/\" (%q)", ErrShape, p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%w: tree path segment %q", ErrShape, seg)
		}
	}
	return nil
}

// EncodeTree writes the manifest in canonical form: entries sorted by path,
// one encoding per tree. The result is content - a blob whose descriptor goes
// into an event - so determinism is what makes "the same folder" mean the
// same hash.
func EncodeTree(entries []TreeEntry) ([]byte, error) {
	if len(entries) > MaxTreeEntries {
		return nil, fmt.Errorf("%w: %d entries, limit %d", ErrShape, len(entries), MaxTreeEntries)
	}
	cp := append([]TreeEntry(nil), entries...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Path < cp[j].Path })
	for i := range cp {
		if err := ValidTreePath(cp[i].Path); err != nil {
			return nil, err
		}
		if i > 0 && cp[i-1].Path == cp[i].Path {
			return nil, fmt.Errorf("%w: duplicate tree path %q", ErrShape, cp[i].Path)
		}
		cp[i].HashHex = cp[i].Hash.String()
	}
	return canon.Marshal(Tree{Version: 1, Entries: cp})
}

// DecodeTree reads a manifest and enforces the canonical form: version 1,
// paths valid, sorted and unique, hashes full.
func DecodeTree(b []byte) (Tree, error) {
	var t Tree
	if err := json.Unmarshal(b, &t); err != nil {
		return t, fmt.Errorf("%w: %v", ErrShape, err)
	}
	if t.Version != 1 {
		return t, fmt.Errorf("%w: unknown tree version %d", ErrShape, t.Version)
	}
	if len(t.Entries) > MaxTreeEntries {
		return t, fmt.Errorf("%w: too many entries", ErrShape)
	}
	for i := range t.Entries {
		if err := ValidTreePath(t.Entries[i].Path); err != nil {
			return t, err
		}
		if i > 0 && t.Entries[i-1].Path >= t.Entries[i].Path {
			return t, fmt.Errorf("%w: tree entries not sorted and unique", ErrShape)
		}
		id, err := frame.ParseID(t.Entries[i].HashHex)
		if err != nil {
			return t, fmt.Errorf("%w: entry hash: %v", ErrShape, err)
		}
		t.Entries[i].Hash = id
	}
	return t, nil
}

// BlobPath is the conventional on-disk place for a blob in a content root:
// content-addressed, so the descriptor alone finds the bytes again and no
// filename is ever recorded.
func BlobPath(root string, d Descriptor) string {
	h := d.Hash.String()
	return filepath.Join(root, h[:2], h[2:]+d.Extension())
}
