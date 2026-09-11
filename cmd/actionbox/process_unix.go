//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureCommandCancellation(command *exec.Cmd, isolateProcessGroup bool) {
	if isolateProcessGroup {
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		pid := command.Process.Pid
		if isolateProcessGroup {
			pid = -pid
		}
		err := syscall.Kill(pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 10 * time.Second
}
