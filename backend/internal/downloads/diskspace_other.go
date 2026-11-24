//go:build !windows

package downloads

import (
	"os"
	"path/filepath"
	"syscall"
)

func availableDiskBytes(dir string) (uint64, bool) {
	cur := dir
	for cur != "" && cur != "/" && cur != "." {
		if _, err := os.Stat(cur); err == nil {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(cur, &st); err != nil {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}
