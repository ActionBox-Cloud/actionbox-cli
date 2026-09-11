package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ActionBox-Cloud/actionbox-cli/internal/api"
	"github.com/ActionBox-Cloud/actionbox-cli/internal/config"
)

func watch(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		return errors.New("usage: actionbox watch <create|list|pause|resume|rotate-token|archive> [options]")
	}
	switch args[0] {
	case "create":
		return createWatch(args[1:])
	case "list":
		return listWatches(args[1:])
	case "pause":
		return mutateWatch(args[1:], "pause")
	case "resume":
		return mutateWatch(args[1:], "resume")
	case "rotate-token":
		return mutateWatch(args[1:], "rotate-token")
	case "archive":
		return mutateWatch(args[1:], "archive")
	default:
		return fmt.Errorf("unknown watch command %q; use create, list, pause, resume, rotate-token, or archive", args[0])
	}
}

func createWatch(args []string) error {
	fs := flagSet("watch create")
	sourceID := fs.String("source-id", "", "Optional Source ID compatibility check")
	schedule := fs.String("schedule", "interval", "interval or cron")
	interval := fs.Int("interval-seconds", 3600, "Interval in seconds (minimum 60)")
	cronExpression := fs.String("cron", "", "Five-field cron expression")
	timezone := fs.String("timezone", "UTC", "IANA timezone")
	grace := fs.Int("grace-seconds", 0, "Allowed lateness in seconds")
	maxRuntime := fs.Int("max-runtime-seconds", 0, "Maximum runtime after start (0 disables)")
	graceDuration := fs.String("grace", "", "Allowed lateness as a duration (for example 15m or 1h)")
	maxRuntimeDuration := fs.String("max-runtime", "", "Maximum runtime as a duration (for example 1h)")
	priority := fs.String("priority", "normal", "low, normal, high, or urgent")
	runbook := fs.String("runbook-url", "", "HTTPS runbook URL")
	signalMethod := fs.String("signal-method", "post", "post or any (default: post)")
	jsonOutput := fs.Bool("json", false, "Print machine-readable JSON")
	if err := fs.Parse(reorderFlags(args, fs)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: actionbox watch create NAME [options]")
	}
	if *schedule != "interval" && *schedule != "cron" {
		return errors.New("--schedule must be interval or cron")
	}
	if *priority != "low" && *priority != "normal" && *priority != "high" && *priority != "urgent" {
		return errors.New("--priority must be low, normal, high, or urgent")
	}
	if *signalMethod != "post" && *signalMethod != "any" {
		return errors.New("--signal-method must be post or any")
	}
	if *schedule == "interval" && *interval < 60 {
		return errors.New("--interval-seconds must be at least 60")
	}
	if *schedule == "cron" && strings.TrimSpace(*cronExpression) == "" {
		return errors.New("--cron is required for cron schedules")
	}
	parsedGrace, err := watchDuration(*graceDuration, *grace, "grace")
	if err != nil {
		return err
	}
	parsedMaxRuntime, err := watchDuration(*maxRuntimeDuration, *maxRuntime, "max runtime")
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	payload := api.CreateWatch{
		SourceID:     strings.TrimSpace(*sourceID),
		Name:         strings.TrimSpace(fs.Arg(0)),
		ScheduleType: *schedule,
		Timezone:     strings.TrimSpace(*timezone),
		GraceSeconds: parsedGrace,
		Priority:     *priority,
		RunbookURL:   strings.TrimSpace(*runbook),
		SignalMethod: *signalMethod,
	}
	if *schedule == "interval" {
		payload.IntervalSeconds = interval
	} else {
		payload.CronExpression = cronExpression
	}
	if parsedMaxRuntime > 0 {
		payload.MaxRuntimeSeconds = &parsedMaxRuntime
	}
	created, err := api.New(cfg.BaseURL, cfg.Token).CreateWatch(context.Background(), payload)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(created)
	}
	fmt.Printf("%s  %s [%s]\n", created.ID, created.Name, created.Status)
	fmt.Printf("heartbeat URL (copy now): %s\n", created.HeartbeatURL)
	return nil
}

func mutateWatch(args []string, operation string) error {
	fs := flagSet("watch " + operation)
	jsonOutput := fs.Bool("json", false, "Print machine-readable JSON")
	if err := fs.Parse(reorderFlags(args, fs)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: actionbox watch %s WATCH_ID [--json]", operation)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client := api.New(cfg.BaseURL, cfg.Token)
	id := fs.Arg(0)
	if operation == "archive" {
		if err := client.ArchiveWatch(context.Background(), id); err != nil {
			return err
		}
		if *jsonOutput {
			return printJSON(map[string]any{"id": id, "archived": true})
		}
		fmt.Printf("%s archived\n", id)
		return nil
	}
	var watch api.Watch
	var created api.CreatedWatch
	switch operation {
	case "pause":
		watch, err = client.PauseWatch(context.Background(), id)
	case "resume":
		watch, err = client.ResumeWatch(context.Background(), id)
	case "rotate-token":
		created, err = client.RotateWatchToken(context.Background(), id)
		watch = created.Watch
	default:
		return fmt.Errorf("unsupported Watch operation %q", operation)
	}
	if err != nil {
		return err
	}
	if *jsonOutput {
		if operation == "rotate-token" {
			return printJSON(created)
		}
		return printJSON(watch)
	}
	fmt.Printf("%s  %s [%s]\n", watch.ID, watch.Name, watch.DerivedStatus)
	if operation == "rotate-token" {
		fmt.Printf("heartbeat URL (copy now): %s\n", created.HeartbeatURL)
	}
	return nil
}

func watchDuration(value string, seconds int, label string) (int, error) {
	if strings.TrimSpace(value) == "" {
		if seconds < 0 {
			return 0, fmt.Errorf("--%s cannot be negative", label)
		}
		return seconds, nil
	}
	if seconds != 0 {
		return 0, fmt.Errorf("use either --%s or --%s-seconds", label, strings.ReplaceAll(label, " ", "-"))
	}
	text := strings.TrimSpace(value)
	if raw, err := strconv.Atoi(text); err == nil {
		if raw < 0 {
			return 0, fmt.Errorf("--%s cannot be negative", label)
		}
		return raw, nil
	}
	duration, err := time.ParseDuration(text)
	if err != nil || duration < 0 {
		return 0, fmt.Errorf("--%s must be a non-negative duration such as 15m or 1h", label)
	}
	wholeSeconds := int(duration / time.Second)
	if duration%time.Second != 0 {
		return 0, fmt.Errorf("--%s must resolve to whole seconds", label)
	}
	return wholeSeconds, nil
}

func listWatches(args []string) error {
	fs := flagSet("watch list")
	jsonOutput := fs.Bool("json", false, "Print machine-readable JSON")
	if err := fs.Parse(reorderFlags(args, fs)); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: actionbox watch list [--json]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	watches, err := api.New(cfg.BaseURL, cfg.Token).ListWatches(context.Background())
	if err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(watches)
	}
	for _, watch := range watches {
		fmt.Printf("%s  %s [%s]", watch.ID, watch.Name, watch.DerivedStatus)
		if watch.ScheduleType == "interval" && watch.IntervalSeconds != nil {
			fmt.Printf(" every %ds", *watch.IntervalSeconds)
		} else if watch.CronExpression != nil {
			fmt.Printf(" %s (%s)", *watch.CronExpression, watch.Timezone)
		}
		fmt.Println()
	}
	return nil
}

// flagSet keeps command-local parsing quiet and consistent with the existing
// CLI.  The helper is intentionally tiny so watch output never leaks a secret
// through the standard flag package's usage text.
func flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}
