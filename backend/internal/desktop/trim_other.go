//go:build !windows

package desktop

import "runtime"

// TrimProcessWorkingSet triggers GC on non-Windows platforms.
func TrimProcessWorkingSet() {
	runtime.GC()
}
