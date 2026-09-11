//go:build !windows

package main

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestConfigureCommandCancellationUsesGracefulProcessGroupShutdown(t *testing.T) {
	command := exec.CommandContext(context.Background(), "sh", "-c", "exit 0")
	configureCommandCancellation(command, true)
	if command.SysProcAttr == nil || !command.SysProcAttr.Setpgid {
		t.Fatal("child process does not have its own process group")
	}
	if command.Cancel == nil {
		t.Fatal("child process does not have a cancellation handler")
	}
	if command.WaitDelay != 10*time.Second {
		t.Fatalf("WaitDelay = %s, want 10s", command.WaitDelay)
	}
}

func TestConfigureCommandCancellationKeepsInteractiveChildInForegroundGroup(t *testing.T) {
	command := exec.CommandContext(context.Background(), "sh", "-c", "exit 0")
	configureCommandCancellation(command, false)
	if command.SysProcAttr != nil {
		t.Fatal("interactive child unexpectedly has process-group isolation")
	}
	if command.Cancel == nil {
		t.Fatal("interactive child does not have a cancellation handler")
	}
	if command.WaitDelay != 10*time.Second {
		t.Fatalf("WaitDelay = %s, want 10s", command.WaitDelay)
	}
}
