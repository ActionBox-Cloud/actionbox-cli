package main

import "io"

func hasHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func commandUsage(out io.Writer, command string) {
	switch command {
	case "configure":
		printlnTo(out, "Usage: actionbox configure [options]", "", "Connect this machine to a Source.", "", "Options:", "  --base-url URL      Hosted API URL (maintainer/test override; default: https://api.actionbox.cloud)", "  --token TOKEN       Source token (prefer interactive entry or --token-stdin)", "  --token-stdin       Read the Source token from stdin", "", "The token is stored with owner-only permissions.")
	case "doctor":
		printlnTo(out, "Usage: actionbox doctor [--json]", "", "Validate local configuration and check API reachability.")
	case "mcp":
		printlnTo(out, "Usage: actionbox mcp", "", "Run the hosted MCP server through local stdio.", "", "The Source token is read from the owner-only configuration created by 'actionbox configure'. It is never sent through the MCP JSON-RPC stream.")
	case "send":
		printlnTo(out, "Usage: actionbox send TITLE [options]", "", "Create an Action and return immediately.", "", sendOptionHelp())
	case "ask":
		printlnTo(out, "Usage: actionbox ask TITLE (--option OPTION | --interaction-json JSON|@FILE) [options]", "", "Create an Action. Add --wait to block until a human decides.", "", sendOptionHelp())
	case "get":
		printlnTo(out, "Usage: actionbox get ACTION_ID [--json]", "", "Read an Action by ID.")
	case "update":
		printlnTo(out, "Usage: actionbox update ACTION_ID [options]", "", "Add context to an open Action, including information requested by a reviewer.", "", "Options:", "  --context-json JSON|@FILE          Typed context block array", "  --decision-class VALUE            Stable decision category", "  --decision-context-json JSON|@FILE Structured Decision Brief", "  --context-unavailable TEXT        Explain why the request cannot be answered", "  --context-unavailable-code CODE   not_available, cannot_access, not_applicable, sensitive, or unknown", "  --json                             Print machine-readable JSON")
	case "resolve":
		printlnTo(out, "Usage: actionbox resolve ACTION_ID [options]", "", "Machine-resolve an Action.", "", "Options:", "  --option ID              Decision option ID", "  --response-json JSON|@FILE  Typed response JSON", "  --reason TEXT            Machine resolution reason", "  --approval-override      Explicitly override an opted-in approval policy", "  --json                   Print machine-readable JSON")
	case "cancel":
		printlnTo(out, "Usage: actionbox cancel ACTION_ID [options]", "", "Cancel an Action from the Source.", "", "Options:", "  --reason TEXT       Cancellation reason", "  --json              Print machine-readable JSON")
	case "watch":
		printlnTo(out, "Usage: actionbox watch <create|list|pause|resume|rotate-token|archive> [options]", "", "Manage heartbeat Watches using the configured Source.", "", "Create:", "  actionbox watch create NAME [options]", "  --source-id ID           Optional compatibility check for the configured Source", "  --schedule TYPE          interval or cron (default: interval)", "  --interval-seconds N     Interval schedule, minimum 60", "  --cron EXPRESSION        Five-field cron schedule", "  --timezone NAME          IANA timezone (default: UTC)", "  --grace 15m              Allowed lateness as a duration", "  --max-runtime 1h         Runtime limit after start", "  --priority LEVEL         low, normal, high, or urgent", "  --signal-method METHOD   post or any (default: post)", "  --runbook-url URL        HTTPS runbook", "  --json                   Print machine-readable JSON", "", "List or mutate:", "  actionbox watch list [--json]", "  actionbox watch pause WATCH_ID", "  actionbox watch resume WATCH_ID", "  actionbox watch rotate-token WATCH_ID", "  actionbox watch archive WATCH_ID", "", "The heartbeat URL is displayed only on create or rotate and must be copied into the scheduler.")
	case "run":
		printlnTo(out, "Usage: actionbox run [--watch-url URL] -- COMMAND [ARGS...]", "", "Run one command and send start, success, or fail heartbeats.", "", "Set ACTIONBOX_WATCH_URL to keep the capability URL out of shell history and process arguments.", "Heartbeat delivery is best-effort and never replaces the wrapped command's exit status.")
	default:
		usage(out)
	}
}

func sendOptionHelp() string {
	return "Options:\n  --description TEXT       Action description\n  --priority LEVEL         low, normal, high, or urgent\n  --option VALUE           Decision option; use id=Label for a stable ID\n  --interaction-json JSON|@FILE\n                           Typed interaction JSON (use instead of --option)\n  --context-json JSON|@FILE\n                           JSON array of typed context blocks\n  --on-expire-json JSON|@FILE\n                           Expiration outcome JSON (return_expired or resolve)\n  --controls-json JSON|@FILE\n                           Paid-plan secondary controls array\n  --assignee-email EMAIL   Assign to an active workspace reviewer\n  --assignee-user-id ID    Assign by stable ActionBox user ID\n  --private                Limit visibility to reviewers and managers (Team)\n  --reviewer-email EMAIL   Allowed reviewer; repeat for multiple reviewers\n  --reviewer-user-id ID    Allowed reviewer ID; repeat for multiple reviewers\n  --approval-mode MODE     any, all, or quorum (Team; requires --private)\n  --required-approvals N   Threshold for quorum mode\n  --approval-option-id ID  Approving option in a two-option policy\n  --rejection-option-id ID Rejecting option in a two-option policy\n  --allow-source-override  Permit an explicit, reasoned Source override\n  --callback-url URL       HTTPS callback destination\n  --open-url URL           URL to open for context\n  --dedupe-key KEY         Merge repeated occurrences\n  --expires RFC3339        Expire the Action at this time\n  --no-origin              Do not attach detected CI origin metadata\n  --wait                   Wait for a human decision\n  --timeout DURATION       Maximum wait time (default: 1h)\n  --json                   Print machine-readable JSON\n  --quiet                  Print only the result"
}

func printlnTo(out io.Writer, lines ...string) {
	for _, line := range lines {
		_, _ = io.WriteString(out, line+"\n")
	}
}
