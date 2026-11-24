//go:build windows

package desktop

import (
	"runtime"
	"syscall"
)

var (
	modPsapi                 = syscall.NewLazyDLL("psapi.dll")
	procEmptyWorkingSet      = modPsapi.NewProc("EmptyWorkingSet")
	procGetCurrentProcess    = syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentProcess")
)

// TrimProcessWorkingSet triggers Go GC and flushes unreferenced memory pages from RAM to the standby cache.
// On Windows, this drops process working set memory down to ~12-18 MB while Minecraft is running.
func TrimProcessWorkingSet() {
	runtime.GC()
	h, _, _ := procGetCurrentProcess.Call()
	if h != 0 {
		_, _, _ = procEmptyWorkingSet.Call(h)
	}
}
