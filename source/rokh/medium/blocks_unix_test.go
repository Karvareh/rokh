//go:build darwin || linux || android

package medium

import (
	"os"
	"syscall"
)

func blocks(info os.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(st.Blocks)
	}
	return -1
}
