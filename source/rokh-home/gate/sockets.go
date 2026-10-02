package gate

import (
	"fmt"
	"os"
	"path/filepath"
)

// Socket grants name individual existing endpoints, never a directory-wide
// network rule. Resolve symlinks once, then hand the exact paths to the OS.
func socketPaths(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("gate: a Unix socket grant needs an absolute path")
		}
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			return nil, err
		}
		st, err := os.Lstat(real)
		if err != nil {
			return nil, err
		}
		if st.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("gate: the named endpoint is not a Unix socket")
		}
		out = append(out, real)
	}
	return out, nil
}
