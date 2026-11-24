//go:build !windows

package launcher

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func QuerySystemMemory() SystemMemoryInfo {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return SystemMemoryInfo{TotalRAMMB: 8192, AvailableRAMMB: 4096, RecommendedMaxMB: 4096}
	}
	defer f.Close()
	var totalKB, availKB int64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				totalKB, _ = strconv.ParseInt(fields[1], 10, 64)
			}
		} else if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				availKB, _ = strconv.ParseInt(fields[1], 10, 64)
			}
		}
	}
	if totalKB <= 0 {
		return SystemMemoryInfo{TotalRAMMB: 8192, AvailableRAMMB: 4096, RecommendedMaxMB: 4096}
	}
	totalMB := totalKB / 1024
	availMB := availKB / 1024
	if availMB <= 0 {
		availMB = totalMB / 2
	}
	return SystemMemoryInfo{
		TotalRAMMB:       totalMB,
		AvailableRAMMB:   availMB,
		RecommendedMaxMB: computeRecommendedMaxRAM(totalMB),
	}
}

func availableDiskMB(dir string) (int64, bool) {
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
	return int64((st.Bavail * uint64(st.Bsize)) / (1024 * 1024)), true
}
