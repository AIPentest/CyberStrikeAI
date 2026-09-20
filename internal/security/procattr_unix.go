//go:build !windows

package security

import (
	"os/exec"
	"syscall"
)

// prepareShellCmdSession 让 shell 子进程在独立会话中运行，便于超时/取消时整组 SIGKILL（含子进程）。
func prepareShellCmdSession(cmd *exec.Cmd) error {
	if cmd == nil {
		return nil
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	return nil
}

// terminateProcessGroup 对 rootPID 对应进程组发 SIGKILL；rootPID 为 0 时回退到 cmd.Process.Pid。
func terminateProcessGroup(rootPID int, cmd *exec.Cmd) {
	pid := rootPID
	if pid <= 0 && cmd != nil && cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}

// terminateCmdTree 尽力终止 cmd 及其进程组（Unix 下 Setsid 后 PGID == 首进程 PID）。
func terminateCmdTree(cmd *exec.Cmd) {
	terminateProcessGroup(0, cmd)
}

// stopProcessGroup gives the whole job a grace period to release resources.
func stopProcessGroup(pid int, cmd *exec.Cmd) {
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
	}
}

func processGroupExists(pid int) bool {
	return pid > 0 && syscall.Kill(-pid, 0) != syscall.ESRCH
}
