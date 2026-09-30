//go:build !(darwin || linux || android)

package medium

import "os"

// Allocated blocks are not measured on this platform: reported as -1.
func blocks(info os.FileInfo) int64 { return -1 }
