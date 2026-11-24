//go:build windows

package launcher

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
	processQueryLimited   = 0x1000
	stillActiveExitCode   = 259
)

var (
	modKernel32Proc      = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess      = modKernel32Proc.NewProc("OpenProcess")
	procGetExitCodeProc  = modKernel32Proc.NewProc("GetExitCodeProcess")
	procCloseHandleProc  = modKernel32Proc.NewProc("CloseHandle")
)

// configureGameCmd detaches the child Java process group on Windows so closing ModHub does NOT
// abruptly kill a running Minecraft session, and hides any empty console window for java.exe.
func configureGameCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | createNoWindow,
		HideWindow:    true,
	}
}

// requestGracefulStop asks Windows to close the Minecraft window cleanly (WM_CLOSE via taskkill without /F).
func requestGracefulStop(proc *os.Process) error {
	if proc == nil || proc.Pid <= 0 {
		return nil
	}
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(proc.Pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

// forceKillProcess forcefully terminates the game process tree if graceful stop did not succeed.
func forceKillProcess(proc *os.Process) error {
	if proc == nil || proc.Pid <= 0 {
		return nil
	}
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(proc.Pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		return proc.Kill()
	}
	return nil
}

// isProcessAlive checks whether a previously launched game PID is still active on Windows.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, _, _ := procOpenProcess.Call(uintptr(processQueryLimited), 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer procCloseHandleProc.Call(h)
	var code uint32
	r, _, _ := procGetExitCodeProc.Call(h, uintptr(unsafe.Pointer(&code)))
	if r == 0 {
		return false
	}
	return code == stillActiveExitCode
}
