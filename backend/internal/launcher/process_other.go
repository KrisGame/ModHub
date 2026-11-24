//go:build !windows

package launcher

import (
	"os"
	"os/exec"
	"syscall"
)

// configureGameCmd starts the game in its own session/process group so closing ModHub does not send SIGHUP/SIGINT to Minecraft.
func configureGameCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}

func requestGracefulStop(proc *os.Process) error {
	if proc == nil || proc.Pid <= 0 {
		return nil
	}
	return proc.Signal(syscall.SIGTERM)
}

func forceKillProcess(proc *os.Process) error {
	if proc == nil || proc.Pid <= 0 {
		return nil
	}
	return proc.Kill()
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
