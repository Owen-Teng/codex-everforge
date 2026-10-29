//go:build !windows

package quota

import (
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
func guardProcess(cmd *exec.Cmd) (func(), error) {
	// CommandContext.Cancel terminates the owned group before Wait reaps its leader.
	return func() {}, nil
}
