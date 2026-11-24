//go:build windows

package launcher

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

type memoryStatusEx struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

var (
	modKernel32Sys         = syscall.NewLazyDLL("kernel32.dll")
	procGlobalMemoryStatus = modKernel32Sys.NewProc("GlobalMemoryStatusEx")
	procGetDiskFreeEx      = modKernel32Sys.NewProc("GetDiskFreeSpaceExW")
)

func QuerySystemMemory() SystemMemoryInfo {
	var ms memoryStatusEx
	ms.dwLength = uint32(unsafe.Sizeof(ms))
	r, _, _ := procGlobalMemoryStatus.Call(uintptr(unsafe.Pointer(&ms)))
	if r == 0 || ms.ullTotalPhys == 0 {
		return SystemMemoryInfo{TotalRAMMB: 8192, AvailableRAMMB: 4096, RecommendedMaxMB: 4096}
	}
	totalMB := int64(ms.ullTotalPhys / (1024 * 1024))
	availMB := int64(ms.ullAvailPhys / (1024 * 1024))
	return SystemMemoryInfo{
		TotalRAMMB:       totalMB,
		AvailableRAMMB:   availMB,
		RecommendedMaxMB: computeRecommendedMaxRAM(totalMB),
	}
}

func availableDiskMB(dir string) (int64, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	ptr, err := syscall.UTF16PtrFromString(abs)
	if err != nil {
		return 0, false
	}
	var freeAvail, totalBytes, totalFree uint64
	r, _, _ := procGetDiskFreeEx.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&freeAvail)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return 0, false
	}
	return int64(freeAvail / (1024 * 1024)), true
}
