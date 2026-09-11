package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"

	"github.com/ActionBox-Cloud/actionbox-cli/internal/api"
	"github.com/ActionBox-Cloud/actionbox-cli/internal/config"
	"golang.org/x/term"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

const maxJSONInputBytes = 256 << 10

var optionSlugUnsafe = regexp.MustCompile(`[^a-z0-9_-]+`)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("option cannot be empty")
	}
	*s = append(*s, value)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	if len(os.Args) > 2 && hasHelp(os.Args[2:]) {
		commandUsage(os.Stdout, os.Args[1])
		return
	}
	var err error
	switch os.Args[1] {
	case "configure":
		err = configure(os.Args[2:])
	case "doctor":
		err = doctor(os.Args[2:])
	case "mcp":
		err = mcpProxy(os.Args[2:])
	case "send":
		err = send(os.Args[2:], false)
	case "ask":
		err = send(os.Args[2:], true)
	case "get":
		err = get(os.Args[2:])
	case "update":
		err = update(os.Args[2:])
	case "resolve":
		err = resolve(os.Args[2:])
	case "cancel":
		err = cancel(os.Args[2:])
	case "watch":
		err = watch(os.Args[2:])
	case "run":
		err = runWithWatch(os.Args[2:])
	case "version", "--version":
		printVersion(os.Stdout)
		return
	case "help", "--help", "-h":
		if len(os.Args) > 2 {
			commandUsage(os.Stdout, os.Args[2])
			return
		}
		usage(os.Stdout)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		var exitErr *cliExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.code)
		}
		fmt.Fprintln(os.Stderr, "actionbox:", err)
		os.Exit(1)
	}
}

type cliExitError struct {
	code int
}

func (e *cliExitError) Error() string { return "" }

func usage(out io.Writer) {
	fmt.Fprintln(out, "Actionbox — let your scripts ask a human a question")
	fmt.Fprintf(out, "Version: %s\n", version)
	fmt.Fprintln(out, "\nUsage:\n  actionbox <command> [options]")
	fmt.Fprintln(out, "\nCommands:")
	fmt.Fprintln(out, "  configure  Connect this machine to an Actionbox Source")
	fmt.Fprintln(out, "  doctor     Check configuration and API reachability")
	fmt.Fprintln(out, "  mcp        Run the local secure MCP bridge")
	fmt.Fprintln(out, "  send       Create an Action and return immediately")
	fmt.Fprintln(out, "  ask        Create an Action; add --wait to block for a decision")
	fmt.Fprintln(out, "  get        Read an Action by ID")
	fmt.Fprintln(out, "  update     Add context to an open Action")
	fmt.Fprintln(out, "  resolve    Machine-resolve an Action")
	fmt.Fprintln(out, "  cancel     Cancel an Action")
	fmt.Fprintln(out, "  watch      Create or list heartbeat Watches")
	fmt.Fprintln(out, "  run        Run a command with start/success/fail heartbeats")
	fmt.Fprintln(out, "  version    Print the installed CLI version")
	fmt.Fprintln(out, "\nExamples:")
	fmt.Fprintln(out, "  actionbox configure")
	fmt.Fprintln(out, "  actionbox ask \"Deploy to production?\" --option approve --option reject --wait")
	fmt.Fprintln(out, "  actionbox send \"Backup finished\" --priority normal")
	fmt.Fprintln(out, "\nRun 'actionbox <command> --help' for command options.")
}

func printVersion(out io.Writer) {
	fmt.Fprintf(out, "actionbox %s (%s, %s)\n", version, commit, buildDate)
}

func configure(args []string) error {
	fs := flag.NewFlagSet("configure", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	baseURL := fs.String("base-url", config.DefaultBaseURL, "Hosted API base URL (maintainer/test override)")
	token := fs.String("token", "", "Source API token")
	tokenStdin := fs.Bool("token-stdin", false, "Read the Source token from stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *token != "" && *tokenStdin {
		return errors.New("use either --token or --token-stdin, not both")
	}
	if *token != "" {
		fmt.Fprintln(os.Stderr, "actionbox: warning: --token may expose the Source token in shell history and process listings; prefer --token-stdin or interactive entry")
	}
	if *tokenStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("could not read token from stdin: %w", err)
		}
		*token = strings.TrimSpace(string(data))
	}
	if *token == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			return errors.New("a Source token is required; use --token-stdin for scripts or run 'actionbox configure' interactively")
		}
		fmt.Printf("Using API base URL %s\n", *baseURL)
		fmt.Print("Source token (hidden): ")
		data, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("could not read Source token: %w", err)
		}
		*token = strings.TrimSpace(string(data))
	}
	cfg := config.Config{BaseURL: *baseURL, Token: *token}
	if err := config.Validate(cfg); err != nil {
		return err
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	path, _ := config.Path()
	fmt.Printf("Configured Actionbox at %s\n", strings.TrimRight(cfg.BaseURL, "/"))
	fmt.Printf("Source token stored securely in %s\n", path)
	fmt.Fprintln(os.Stdout, "Next step: actionbox doctor")
	return nil
}

func doctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "Print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: actionbox doctor [--json]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	path, _ := config.Path()
	health, healthErr := api.New(cfg.BaseURL, cfg.Token).Health(context.Background())
	if *jsonOutput {
		result := map[string]any{
			"version":          version,
			"config_file":      path,
			"api_base_url":     cfg.BaseURL,
			"token_configured": true,
			"api_reachable":    healthErr == nil,
		}
		if healthErr == nil {
			result["api"] = health
		} else {
			result["error"] = healthErr.Error()
		}
		if err := printJSON(result); err != nil {
			return err
		}
		if healthErr != nil {
			return &cliExitError{code: 1}
		}
		return nil
	}
	fmt.Printf("Actionbox CLI %s\n", version)
	fmt.Printf("API: %s\n", cfg.BaseURL)
	fmt.Printf("Config: %s\n", path)
	fmt.Println("Token: configured (value hidden)")
	if healthErr != nil {
		return fmt.Errorf("API is not reachable: %w", healthErr)
	}
	if strings.TrimSpace(health.Environment) == "" {
		fmt.Println("API: connected")
	} else {
		fmt.Printf("API: connected (%s)\n", health.Environment)
	}
	return nil
}

type sendFlags struct {
	noChat              bool
	description         string
	priority            string
	openURL             string
	dedupeKey           string
	callbackURL         string
	expires             string
	wait                bool
	timeout             time.Duration
	jsonOutput          bool
	quiet               bool
	noOrigin            bool
	options             stringList
	interaction         json.RawMessage
	context             json.RawMessage
	decisionClass       string
	decisionContext     json.RawMessage
	onExpire            json.RawMessage
	controls            json.RawMessage
	assigneeEmail       string
	assigneeUserID      string
	private             bool
	reviewerEmails      stringList
	reviewerUserIDs     stringList
	approvalMode        string
	requiredApprovals   int
	approvalOptionID    string
	rejectionOptionID   string
	allowSourceOverride bool
}

func parseSend(args []string, command string) (sendFlags, string, error) {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	flags := sendFlags{timeout: time.Hour}
	fs.BoolVar(&flags.noChat, "no-chat", false, "Keep this Action out of chat integrations")
	fs.StringVar(&flags.description, "description", "", "Action description")
	fs.StringVar(&flags.priority, "priority", "normal", "low, normal, high, or urgent")
	fs.StringVar(&flags.openURL, "open-url", "", "URL to open for context")
	fs.StringVar(&flags.dedupeKey, "dedupe-key", "", "Merge repeated open occurrences")
	fs.StringVar(&flags.callbackURL, "callback-url", "", "HTTPS callback destination")
	fs.StringVar(&flags.expires, "expires", "", "RFC3339 expiration timestamp")
	fs.StringVar(&flags.assigneeEmail, "assignee-email", "", "Assign to an active workspace reviewer")
	fs.StringVar(&flags.assigneeUserID, "assignee-user-id", "", "Assign by stable ActionBox user ID")
	fs.BoolVar(&flags.private, "private", false, "Restrict visibility to reviewers and workspace managers (Team)")
	fs.Var(&flags.reviewerEmails, "reviewer-email", "Allowed reviewer email; repeat for multiple reviewers")
	fs.Var(&flags.reviewerUserIDs, "reviewer-user-id", "Allowed reviewer user ID; repeat for multiple reviewers")
	fs.StringVar(&flags.approvalMode, "approval-mode", "", "Approval policy: any, all, or quorum (Team)")
	fs.IntVar(&flags.requiredApprovals, "required-approvals", 0, "Approval threshold for quorum mode")
	fs.StringVar(&flags.approvalOptionID, "approval-option-id", "", "Single-choice option ID that approves")
	fs.StringVar(&flags.rejectionOptionID, "rejection-option-id", "", "Single-choice option ID that rejects")
	fs.BoolVar(&flags.allowSourceOverride, "allow-source-override", false, "Allow explicit Source override of this policy")
	fs.BoolVar(&flags.wait, "wait", false, "Wait for a human decision")
	fs.DurationVar(&flags.timeout, "timeout", time.Hour, "Maximum wait time")
	fs.BoolVar(&flags.jsonOutput, "json", false, "Print JSON")
	fs.BoolVar(&flags.quiet, "quiet", false, "Print only the decision when waiting")
	fs.BoolVar(&flags.noOrigin, "no-origin", false, "Do not attach detected CI origin metadata")
	fs.Var(&flags.options, "option", "Decision option, optionally id=Label")
	var interactionJSON string
	fs.StringVar(&interactionJSON, "interaction-json", "", "Typed interaction JSON, or @FILE")
	var contextJSON string
	fs.StringVar(&contextJSON, "context-json", "", "Context block array JSON, or @FILE")
	fs.StringVar(&flags.decisionClass, "decision-class", "", "Stable decision category identifier")
	var decisionContextJSON string
	fs.StringVar(&decisionContextJSON, "decision-context-json", "", "Structured Decision Brief JSON, or @FILE")
	var onExpireJSON string
	fs.StringVar(&onExpireJSON, "on-expire-json", "", "Expiration outcome JSON, or @FILE")
	var controlsJSON string
	fs.StringVar(&controlsJSON, "controls-json", "", "Action Controls array JSON, or @FILE")
	if err := fs.Parse(reorderFlags(args, fs)); err != nil {
		return flags, "", err
	}
	if fs.NArg() < 1 {
		return flags, "", errors.New("a title is required")
	}
	if flags.priority != "low" && flags.priority != "normal" && flags.priority != "high" && flags.priority != "urgent" {
		return flags, "", errors.New("priority must be one of: low, normal, high, urgent")
	}
	if len(flags.options) > 3 {
		return flags, "", errors.New("at most 3 options are supported")
	}
	if strings.TrimSpace(flags.assigneeEmail) != "" && strings.TrimSpace(flags.assigneeUserID) != "" {
		return flags, "", errors.New("use either --assignee-email or --assignee-user-id, not both")
	}
	if len(flags.reviewerEmails) > 0 && len(flags.reviewerUserIDs) > 0 {
		return flags, "", errors.New("use reviewer emails or reviewer user IDs, not both")
	}
	if !flags.private && (len(flags.reviewerEmails) > 0 || len(flags.reviewerUserIDs) > 0) {
		return flags, "", errors.New("reviewer lists require --private")
	}
	if flags.private && len(flags.reviewerEmails) == 0 && len(flags.reviewerUserIDs) == 0 && strings.TrimSpace(flags.assigneeEmail) == "" && strings.TrimSpace(flags.assigneeUserID) == "" {
		return flags, "", errors.New("--private requires at least one reviewer")
	}
	if flags.approvalMode != "" {
		if flags.approvalMode != "any" && flags.approvalMode != "all" && flags.approvalMode != "quorum" {
			return flags, "", errors.New("--approval-mode must be one of: any, all, quorum")
		}
		if !flags.private || (len(flags.reviewerEmails) == 0 && len(flags.reviewerUserIDs) == 0) {
			return flags, "", errors.New("approval policies require --private and at least one explicit --reviewer-email or --reviewer-user-id")
		}
		if flags.approvalMode == "quorum" && flags.requiredApprovals <= 0 {
			return flags, "", errors.New("quorum approval requires --required-approvals")
		}
		reviewerCount := len(flags.reviewerEmails) + len(flags.reviewerUserIDs)
		if flags.approvalMode == "quorum" && flags.requiredApprovals > reviewerCount {
			return flags, "", fmt.Errorf("--required-approvals (%d) cannot exceed reviewer count (%d)", flags.requiredApprovals, reviewerCount)
		}
		if flags.approvalMode != "quorum" && flags.requiredApprovals != 0 {
			return flags, "", errors.New("--required-approvals is only valid with --approval-mode quorum")
		}
	} else if flags.requiredApprovals != 0 || flags.approvalOptionID != "" || flags.rejectionOptionID != "" || flags.allowSourceOverride {
		return flags, "", errors.New("approval policy flags require --approval-mode")
	}
	if (flags.approvalOptionID == "") != (flags.rejectionOptionID == "") {
		return flags, "", errors.New("--approval-option-id and --rejection-option-id must be provided together")
	}
	if interactionJSON != "" {
		var err error
		flags.interaction, err = readJSONObject(interactionJSON, "interaction")
		if err != nil {
			return flags, "", err
		}
	}
	if contextJSON != "" {
		var err error
		flags.context, err = readJSONArray(contextJSON, "context")
		if err != nil {
			return flags, "", err
		}
	}
	if decisionContextJSON != "" {
		var err error
		flags.decisionContext, err = readJSONObject(decisionContextJSON, "decision-context")
		if err != nil {
			return flags, "", err
		}
	}
	if onExpireJSON != "" {
		var err error
		flags.onExpire, err = readJSONObject(onExpireJSON, "on-expire")
		if err != nil {
			return flags, "", err
		}
	}
	if controlsJSON != "" {
		var err error
		flags.controls, err = readJSONArray(controlsJSON, "controls")
		if err != nil {
			return flags, "", err
		}
	}
	if len(flags.options) > 0 && len(flags.interaction) > 0 {
		return flags, "", errors.New("use either --option or --interaction-json, not both")
	}
	if flags.approvalMode != "" {
		interactionType := ""
		optionIDs := make([]string, 0, len(flags.options))
		if len(flags.options) > 0 {
			interactionType = "single_choice"
			for _, raw := range flags.options {
				id, _ := optionValues(raw)
				optionIDs = append(optionIDs, id)
			}
		} else if len(flags.interaction) > 0 {
			var interaction struct {
				Type    string `json:"type"`
				Options []struct {
					ID string `json:"id"`
				} `json:"options"`
			}
			if err := json.Unmarshal(flags.interaction, &interaction); err != nil {
				return flags, "", fmt.Errorf("invalid interaction JSON: %w", err)
			}
			interactionType = interaction.Type
			for _, option := range interaction.Options {
				optionIDs = append(optionIDs, option.ID)
			}
		}
		switch interactionType {
		case "boolean":
			if flags.approvalOptionID != "" || flags.rejectionOptionID != "" {
				return flags, "", errors.New("boolean approval policies cannot use --approval-option-id or --rejection-option-id")
			}
		case "single_choice":
			if len(optionIDs) != 2 {
				return flags, "", errors.New("approval policies require exactly two single-choice options")
			}
			if flags.approvalOptionID == "" || flags.rejectionOptionID == "" {
				return flags, "", errors.New("single-choice approval policies require --approval-option-id and --rejection-option-id")
			}
			if flags.approvalOptionID == flags.rejectionOptionID {
				return flags, "", errors.New("approval and rejection option IDs must be different")
			}
			mapped := map[string]bool{
				flags.approvalOptionID:  true,
				flags.rejectionOptionID: true,
			}
			if !mapped[optionIDs[0]] || !mapped[optionIDs[1]] {
				return flags, "", errors.New("approval and rejection option IDs must match the two interaction options")
			}
		default:
			return flags, "", errors.New("approval policies require a boolean or two-option single-choice interaction")
		}
	}
	if flags.timeout <= 0 {
		return flags, "", errors.New("timeout must be greater than zero")
	}
	return flags, strings.Join(fs.Args(), " "), nil
}

// The standard flag package stops parsing at the first positional argument.
// Actionbox examples intentionally put the title first, so normalize known
// flags before handing the arguments to flag.FlagSet.
func reorderFlags(args []string, fs *flag.FlagSet) []string {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		declared := fs.Lookup(name)
		isBoolean := false
		if declared != nil {
			if booleanValue, ok := declared.Value.(interface{ IsBoolFlag() bool }); ok {
				isBoolean = booleanValue.IsBoolFlag()
			}
		}
		if !strings.Contains(arg, "=") && declared != nil && !isBoolean && index+1 < len(args) {
			index++
			flags = append(flags, args[index])
		}
	}
	return append(flags, positionals...)
}

func send(args []string, ask bool) error {
	flags, title, err := parseSend(args, map[bool]string{true: "ask", false: "send"}[ask])
	if err != nil {
		return err
	}
	if ask && len(flags.options) == 0 && len(flags.interaction) == 0 {
		return errors.New("ask requires --option or --interaction-json")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	options := make([]api.Option, 0, len(flags.options))
	optionIDs := map[string]bool{}
	for index, raw := range flags.options {
		id, label := optionValues(raw)
		if id == "" || label == "" {
			return errors.New("each --option must have a non-empty label or id=label value")
		}
		if optionIDs[id] {
			return fmt.Errorf("duplicate option ID %q", id)
		}
		optionIDs[id] = true
		style := "default"
		if index == 0 {
			style = "primary"
		} else if index == 1 {
			style = "destructive"
		}
		options = append(options, api.Option{ID: id, Label: label, Style: style})
	}
	payload := api.CreateAction{
		Title:           title,
		Description:     flags.description,
		Priority:        flags.priority,
		OpenURL:         flags.openURL,
		DedupeKey:       flags.dedupeKey,
		CallbackURL:     flags.callbackURL,
		ExpiresAt:       flags.expires,
		Options:         options,
		Interaction:     flags.interaction,
		Context:         flags.context,
		DecisionClass:   flags.decisionClass,
		DecisionContext: flags.decisionContext,
		OnExpire:        flags.onExpire,
		Controls:        flags.controls,
		AssigneeEmail:   flags.assigneeEmail,
		AssigneeUserID:  flags.assigneeUserID,
		ReviewerEmails:  flags.reviewerEmails,
		ReviewerUserIDs: flags.reviewerUserIDs,
	}
	if flags.noChat {
		payload.ChatDelivery = "disabled"
	}
	if flags.approvalMode != "" {
		payload.ApprovalPolicy = &api.ApprovalPolicy{
			SchemaVersion: 1, Mode: flags.approvalMode,
			RequiredApprovals:   flags.requiredApprovals,
			ApprovalOptionID:    flags.approvalOptionID,
			RejectionOptionID:   flags.rejectionOptionID,
			AllowSourceOverride: flags.allowSourceOverride,
		}
	}
	if flags.private {
		payload.Visibility = "restricted"
	}
	if !flags.noOrigin {
		if origin := detectOrigin(nil); origin != nil {
			payload.Metadata = map[string]any{"origin": origin}
		}
	}
	client := api.New(cfg.BaseURL, cfg.Token)
	key, err := randomKey()
	if err != nil {
		return err
	}
	var action api.Action
	if ask && flags.wait {
		action, err = client.CreateBlockingAction(context.Background(), payload, key)
	} else {
		action, err = client.CreateAction(context.Background(), payload, key)
	}
	if err != nil {
		return err
	}
	if !ask || !flags.wait {
		return printAction(action, flags.jsonOutput, flags.quiet)
	}
	return waitForDecision(client, action, flags)
}

func optionValues(raw string) (string, string) {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	label := strings.TrimSpace(raw)
	slug := optionSlugUnsafe.ReplaceAllString(strings.ToLower(label), "-")
	return strings.Trim(slug, "-"), label
}

func waitForDecision(client *api.Client, action api.Action, flags sendFlags) error {
	original := action
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	deadline := time.Now().Add(flags.timeout)
	for {
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "Action %s is still open; timed out locally\n", action.ID)
			if flags.jsonOutput {
				_ = printJSON(waitResult(action, "open"))
			} else {
				fmt.Println(action.ID)
			}
			return &cliExitError{code: 124}
		}
		if action.Status != "open" {
			if action.Status == "resolved" {
				sameBinding := original.Fingerprint != "" && original.Fingerprint == action.Fingerprint && original.ActionVersion == action.ActionVersion
				sameContent := original.ContentFingerprint != "" && original.ContentFingerprint == action.ContentFingerprint
				policy := original.ApprovalPolicy != nil && action.ApprovalPolicy != nil
				provenance := action.ResolvedByType == "user" || (policy && original.ApprovalPolicy.AllowSourceOverride && action.ApprovalPolicy.AllowSourceOverride && action.ResolvedByType == "source")
				if !provenance || !(sameBinding || (policy && sameContent)) {
					return fmt.Errorf("APPROVAL_BINDING_MISMATCH: Action was not approved against the originally created request")
				}
			}
			if flags.jsonOutput {
				return printJSON(waitResult(action, action.Status))
			}
			result := action.ResolutionOptionID
			if result == "" {
				result = action.Status
			}
			if flags.quiet {
				fmt.Println(result)
			} else {
				fmt.Printf("Action %s finished with %s\n", action.ID, result)
			}
			return nil
		}
		remaining := time.Until(deadline)
		waitSeconds := int((remaining + time.Second - 1) / time.Second)
		if waitSeconds < 1 {
			waitSeconds = 1
		}
		if waitSeconds > 30 {
			waitSeconds = 30
		}
		requestCtx, cancel := context.WithDeadline(ctx, deadline)
		updated, err := client.GetAction(requestCtx, action.ID, waitSeconds)
		cancel()
		if err != nil {
			if requestCtx.Err() == context.DeadlineExceeded {
				continue
			}
			fmt.Fprintln(os.Stderr, "waiting:", err)
			if ctx.Err() != nil {
				fmt.Fprintf(os.Stderr, "stopped waiting; Action %s remains open\n", action.ID)
				return &cliExitError{code: 130}
			}
			if waitErr := waitForRetry(ctx, 500*time.Millisecond); waitErr != nil {
				return waitErr
			}
			continue
		}
		action = updated
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func waitResult(action api.Action, status string) map[string]any {
	return map[string]any{
		"action_id":   action.ID,
		"status":      status,
		"decision":    action.ResolutionOptionID,
		"interaction": action.Interaction,
		"response":    action.Response,
		"receipt":     action.Receipt,
	}
}

func get(args []string) error {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "Print JSON")
	if err := fs.Parse(reorderFlags(args, fs)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: actionbox get <action-id> [--json]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	action, err := api.New(cfg.BaseURL, cfg.Token).GetAction(context.Background(), fs.Arg(0))
	if err != nil {
		return err
	}
	return printAction(action, *jsonOutput, false)
}

func update(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	contextJSON := fs.String("context-json", "", "Typed context JSON array, or @FILE")
	decisionClass := fs.String("decision-class", "", "Stable decision category")
	decisionContextJSON := fs.String("decision-context-json", "", "Decision context JSON object, or @FILE")
	chatDelivery := fs.String("chat-delivery", "", "Chat delivery: inherit or disabled")
	unavailableReason := fs.String("context-unavailable", "", "Explain why requested context cannot be provided")
	unavailableReasonCode := fs.String("context-unavailable-code", "not_available", "not_available, cannot_access, not_applicable, sensitive, or unknown")
	jsonOutput := fs.Bool("json", false, "Print JSON")
	if err := fs.Parse(reorderFlags(args, fs)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: actionbox update <action-id> [--context-json JSON|@FILE] [--decision-context-json JSON|@FILE]")
	}
	if *chatDelivery != "" && *chatDelivery != "inherit" && *chatDelivery != "disabled" {
		return errors.New("--chat-delivery must be inherit or disabled")
	}
	payload := api.UpdateAction{ChatDelivery: *chatDelivery}
	var err error
	if *contextJSON != "" {
		payload.Context, err = readJSONArray(*contextJSON, "context")
		if err != nil {
			return err
		}
	}
	if *decisionContextJSON != "" {
		payload.DecisionContext, err = readJSONObject(*decisionContextJSON, "decision-context")
		if err != nil {
			return err
		}
	}
	if *decisionClass != "" {
		payload.DecisionClass = decisionClass
	}
	if *unavailableReason != "" && (len(payload.Context) > 0 || len(payload.DecisionContext) > 0 || payload.DecisionClass != nil || payload.ChatDelivery != "") {
		return errors.New("use --context-unavailable without context update flags")
	}
	validUnavailableCode := map[string]bool{"not_available": true, "cannot_access": true, "not_applicable": true, "sensitive": true, "unknown": true}
	if !validUnavailableCode[*unavailableReasonCode] {
		return errors.New("invalid --context-unavailable-code")
	}
	if len(payload.Context) == 0 && len(payload.DecisionContext) == 0 && payload.DecisionClass == nil && payload.ChatDelivery == "" {
		if *unavailableReason == "" {
			return errors.New("provide context to update or --context-unavailable")
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client := api.New(cfg.BaseURL, cfg.Token)
	var action api.Action
	if *unavailableReason != "" {
		action, err = client.MarkContextUnavailable(context.Background(), fs.Arg(0), *unavailableReason, *unavailableReasonCode)
	} else {
		action, err = client.UpdateAction(context.Background(), fs.Arg(0), payload)
	}
	if err != nil {
		return err
	}
	return printAction(action, *jsonOutput, false)
}

func resolve(args []string) error {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	option := fs.String("option", "", "Decision option ID")
	responseJSON := fs.String("response-json", "", "Typed response JSON, or @FILE")
	reason := fs.String("reason", "", "Machine resolution reason")
	approvalOverride := fs.Bool("approval-override", false, "Explicitly override an opted-in approval policy")
	jsonOutput := fs.Bool("json", false, "Print JSON")
	if err := fs.Parse(reorderFlags(args, fs)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: actionbox resolve <action-id> [--option id | --response-json JSON|@FILE]")
	}
	if *option != "" && *responseJSON != "" {
		return errors.New("use either --option or --response-json, not both")
	}
	if *approvalOverride && strings.TrimSpace(*reason) == "" {
		return errors.New("--approval-override requires a non-empty --reason")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var response json.RawMessage
	if *responseJSON != "" {
		response, err = readJSONObject(*responseJSON, "response")
		if err != nil {
			return err
		}
	}
	client := api.New(cfg.BaseURL, cfg.Token)
	// Read the exact snapshot before resolving so the machine decision cannot
	// silently close an Action that changed while it was being reviewed.
	observed, err := client.GetAction(context.Background(), fs.Arg(0))
	if err != nil {
		return err
	}
	var action api.Action
	if *approvalOverride {
		action, err = client.ResolveApprovalOverrideAtVersion(
			context.Background(), fs.Arg(0), *option, response, *reason,
			observed.ActionVersion, observed.Fingerprint,
		)
	} else if len(response) > 0 {
		action, err = client.ResolveWithResponseAtVersion(
			context.Background(), fs.Arg(0), *option, response, *reason,
			observed.ActionVersion, observed.Fingerprint,
		)
	} else {
		action, err = client.ResolveAtVersion(
			context.Background(), fs.Arg(0), *option, *reason,
			observed.ActionVersion, observed.Fingerprint,
		)
	}
	if err != nil {
		return err
	}
	return printAction(action, *jsonOutput, false)
}

func cancel(args []string) error {
	fs := flag.NewFlagSet("cancel", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	reason := fs.String("reason", "", "Cancellation reason")
	jsonOutput := fs.Bool("json", false, "Print JSON")
	if err := fs.Parse(reorderFlags(args, fs)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: actionbox cancel <action-id>")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	action, err := api.New(cfg.BaseURL, cfg.Token).Cancel(context.Background(), fs.Arg(0), *reason)
	if err != nil {
		return err
	}
	return printAction(action, *jsonOutput, false)
}

func randomKey() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "cli-" + hex.EncodeToString(buffer), nil
}

func printAction(action api.Action, jsonOutput, quiet bool) error {
	if jsonOutput {
		return printJSON(action)
	}
	if quiet {
		fmt.Println(action.ID)
		return nil
	}
	fmt.Printf("%s  %s [%s]\n", action.ID, action.Title, action.Status)
	if action.ResolutionOptionID != "" {
		fmt.Printf("decision: %s\n", action.ResolutionOptionID)
	}
	return nil
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func readJSONObject(value, fieldName string) (json.RawMessage, error) {
	data, err := readJSON(value, fieldName)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, fmt.Errorf("--%s-json must contain a JSON object", fieldName)
	}
	return data, nil
}

func readJSONArray(value, fieldName string) (json.RawMessage, error) {
	data, err := readJSON(value, fieldName)
	if err != nil {
		return nil, err
	}
	var array []any
	if err := json.Unmarshal(data, &array); err != nil || array == nil {
		return nil, fmt.Errorf("--%s-json must contain a JSON array", fieldName)
	}
	return data, nil
}

func readJSON(value, fieldName string) (json.RawMessage, error) {
	input := strings.TrimSpace(value)
	if input == "" {
		return nil, fmt.Errorf("--%s-json cannot be empty", fieldName)
	}

	data := []byte(input)
	path := input
	if strings.HasPrefix(input, "@") {
		path = strings.TrimSpace(strings.TrimPrefix(input, "@"))
		if path == "" {
			return nil, fmt.Errorf("--%s-json file path cannot be empty", fieldName)
		}
	}
	if strings.HasPrefix(input, "@") {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("could not read %s JSON file %q: %w", fieldName, path, err)
		}
		fileData, readErr := io.ReadAll(io.LimitReader(file, maxJSONInputBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("could not read %s JSON file %q: %w", fieldName, path, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("could not close %s JSON file %q: %w", fieldName, path, closeErr)
		}
		if len(fileData) > maxJSONInputBytes {
			return nil, fmt.Errorf("%s JSON file %q exceeds %d bytes", fieldName, path, maxJSONInputBytes)
		}
		data = fileData
	}

	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil, fmt.Errorf("--%s-json must contain a JSON value", fieldName)
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("invalid %s JSON: %w", fieldName, err)
	}
	if decoded == nil {
		return nil, fmt.Errorf("--%s-json must not be null", fieldName)
	}
	return json.RawMessage(data), nil
}
