package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ActionBox-Cloud/actionbox-cli/internal/api"
	"golang.org/x/term"
)

// runWithWatch wraps one process with start/success/fail signals. Heartbeat
// delivery is best-effort so a temporary monitor outage never changes the
// wrapped command's exit status.
func runWithWatch(args []string) error {
	separator := -1
	for index, arg := range args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator == len(args)-1 {
		return errors.New("usage: actionbox run [--watch-url URL] -- COMMAND [ARGS...]")
	}
	fs := flagSet("run")
	watchURL := fs.String("watch-url", strings.TrimSpace(os.Getenv("ACTIONBOX_WATCH_URL")), "Heartbeat URL returned when a Watch is created")
	if err := fs.Parse(reorderFlags(args[:separator], fs)); err != nil {
		return err
	}
	if fs.NArg() != 0 || strings.TrimSpace(*watchURL) == "" {
		return errors.New("--watch-url or ACTIONBOX_WATCH_URL and a command after -- are required")
	}
	commandArgs := args[separator+1:]
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := api.SendHeartbeat(ctx, *watchURL, "start"); err != nil {
		fmt.Fprintln(os.Stderr, "actionbox: start heartbeat unavailable; continuing command")
	}
	command := exec.CommandContext(ctx, commandArgs[0], commandArgs[1:]...)
	// A child in a separate process group cannot read from this controlling
	// terminal unless foreground ownership is also transferred. Keep interactive
	// commands in the foreground group; isolate non-interactive jobs so context
	// cancellation can terminate their entire process tree.
	configureCommandCancellation(command, !term.IsTerminal(int(os.Stdin.Fd())))
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	runErr := command.Run()
	signalName := "success"
	if runErr != nil || ctx.Err() != nil {
		signalName = "fail"
	}
	finalContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := api.SendHeartbeat(finalContext, *watchURL, signalName); err != nil {
		fmt.Fprintf(os.Stderr, "actionbox: %s heartbeat unavailable\n", signalName)
	}
	if runErr == nil && ctx.Err() == nil {
		return nil
	}
	if exitError, ok := runErr.(*exec.ExitError); ok {
		code := exitError.ExitCode()
		if code < 0 {
			code = 1
		}
		return &cliExitError{code: code}
	}
	if ctx.Err() != nil {
		return &cliExitError{code: 130}
	}
	return fmt.Errorf("command failed: %w", runErr)
}
