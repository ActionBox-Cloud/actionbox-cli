//go:build windows

package main

import (
	"os"
	"os/exec"
	"time"
)

func configureCommandCancellation(command *exec.Cmd, _ bool) {
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return command.Process.Kill()
	}
	command.WaitDelay = 10 * time.Second
}
