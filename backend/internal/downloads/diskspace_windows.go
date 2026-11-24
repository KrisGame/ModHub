//go:build windows

package downloads

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	modKernel32Disk       = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceW = modKernel32Disk.NewProc("GetDiskFreeSpaceExW")
)

// availableDiskBytes returns free bytes available to the current user at dir (0, false if unavailable).
func availableDiskBytes(dir string) (uint64, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	pathPtr, err := syscall.UTF16PtrFromString(abs)
	if err != nil {
		return 0, false
	}
	var freeBytesAvailable uint64
	var totalBytes uint64
	var totalFreeBytes uint64
	r1, _, _ := procGetDiskFreeSpaceW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if r1 == 0 {
		return 0, false
	}
	return freeBytesAvailable, true
}
