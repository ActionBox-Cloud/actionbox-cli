package main

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ActionBox-Cloud/actionbox-cli/internal/api"
)

func TestParseSendValidatesAndReordersTitleFlags(t *testing.T) {
	flags, title, err := parseSend([]string{"Deploy to production?", "--option", "approve", "--priority", "high", "--no-origin"}, "ask")
	if err != nil {
		t.Fatalf("parseSend returned error: %v", err)
	}
	if title != "Deploy to production?" || flags.priority != "high" || len(flags.options) != 1 {
		t.Fatalf("flags=%+v title=%q", flags, title)
	}
	if !flags.noOrigin {
		t.Fatal("--no-origin was not parsed")
	}
}

func TestParseSendRejectsInvalidValues(t *testing.T) {
	tests := [][]string{
		{"Title", "--priority", "critical"},
		{"Title", "--timeout", "0s"},
		{"Title", "--option", "one", "--option", "two", "--option", "three", "--option", "four"},
		{"Title", "--assignee-email", "reviewer@example.com", "--assignee-user-id", "usr_reviewer"},
	}
	for _, args := range tests {
		if _, _, err := parseSend(args, "send"); err == nil {
			t.Fatalf("parseSend(%v) returned nil error", args)
		}
	}
}

func TestParseSendSupportsAssigneeUserID(t *testing.T) {
	flags, _, err := parseSend([]string{"Deploy?", "--assignee-user-id", "usr_reviewer"}, "send")
	if err != nil {
		t.Fatalf("parseSend returned error: %v", err)
	}
	if flags.assigneeUserID != "usr_reviewer" || flags.assigneeEmail != "" {
		t.Fatalf("assignment flags=%+v", flags)
	}
}

func TestParseSendSupportsRestrictedReviewerLists(t *testing.T) {
	flags, _, err := parseSend([]string{
		"Approve access?", "--private",
		"--reviewer-email", "one@example.com",
		"--reviewer-email", "two@example.com",
	}, "send")
	if err != nil {
		t.Fatalf("parseSend returned error: %v", err)
	}
	if !flags.private || len(flags.reviewerEmails) != 2 {
		t.Fatalf("restricted flags=%+v", flags)
	}
	if _, _, err := parseSend([]string{"Private", "--private"}, "send"); err == nil {
		t.Fatal("parseSend accepted --private without a reviewer")
	}
	if _, _, err := parseSend([]string{"Public", "--reviewer-email", "one@example.com"}, "send"); err == nil {
		t.Fatal("parseSend accepted a reviewer list without --private")
	}
}

func TestParseSendSupportsApprovalPolicy(t *testing.T) {
	flags, _, err := parseSend([]string{
		"Approve deploy?", "--private",
		"--reviewer-email", "release@example.com",
		"--reviewer-email", "security@example.com",
		"--option", "approve=Approve", "--option", "reject=Reject",
		"--approval-mode", "quorum", "--required-approvals", "2",
		"--approval-option-id", "approve", "--rejection-option-id", "reject",
		"--allow-source-override",
	}, "ask")
	if err != nil {
		t.Fatalf("parseSend returned error: %v", err)
	}
	if flags.approvalMode != "quorum" || flags.requiredApprovals != 2 || !flags.allowSourceOverride {
		t.Fatalf("approval flags=%+v", flags)
	}

	invalid := [][]string{
		{"Approve?", "--approval-mode", "any"},
		{"Approve?", "--private", "--reviewer-email", "a@example.com", "--approval-mode", "quorum"},
		{"Approve?", "--private", "--reviewer-email", "a@example.com", "--approval-mode", "all", "--required-approvals", "1"},
		{"Approve?", "--private", "--reviewer-email", "a@example.com", "--approval-mode", "any", "--approval-option-id", "approve"},
		{"Approve?", "--private", "--reviewer-email", "a@example.com", "--approval-mode", "quorum", "--required-approvals", "2", "--interaction-json", `{"type":"boolean","label":"Approve?"}`},
		{"Approve?", "--private", "--reviewer-email", "a@example.com", "--approval-mode", "any", "--interaction-json", `{"type":"boolean","label":"Approve?"}`, "--approval-option-id", "approve", "--rejection-option-id", "reject"},
		{"Approve?", "--private", "--reviewer-email", "a@example.com", "--approval-mode", "any", "--option", "approve=Approve", "--option", "reject=Reject"},
		{"Approve?", "--private", "--reviewer-email", "a@example.com", "--approval-mode", "any", "--option", "approve=Approve", "--option", "reject=Reject", "--approval-option-id", "yes", "--rejection-option-id", "no"},
	}
	for _, args := range invalid {
		if _, _, err := parseSend(args, "ask"); err == nil {
			t.Fatalf("parseSend(%v) accepted invalid approval policy", args)
		}
	}
}

func TestParseSendSupportsTypedInteractionAndPreservesOptionFlow(t *testing.T) {
	interaction := `{"type":"boolean","label":"Deploy?","true_label":"Yes","false_label":"No"}`
	flags, title, err := parseSend([]string{"Deploy?", "--interaction-json", interaction}, "ask")
	if err != nil {
		t.Fatalf("parseSend returned error: %v", err)
	}
	if title != "Deploy?" || len(flags.options) != 0 || string(flags.interaction) != interaction {
		t.Fatalf("flags=%+v title=%q", flags, title)
	}

	if _, _, err := parseSend([]string{"Deploy?", "--option", "yes", "--interaction-json", interaction}, "ask"); err == nil {
		t.Fatal("parseSend accepted --option and --interaction-json together")
	}
}

func TestParseSendSupportsContextAndExpirationOutcome(t *testing.T) {
	contextJSON := `[{"type":"key_value","title":"Deploy","items":{"environment":"production"}}]`
	onExpireJSON := `{"type":"resolve","response":{"type":"boolean","value":false},"reason":"safe default"}`
	flags, title, err := parseSend([]string{
		"Deploy?",
		"--interaction-json", `{"type":"boolean","label":"Proceed?"}`,
		"--context-json", contextJSON,
		"--on-expire-json", onExpireJSON,
	}, "ask")
	if err != nil {
		t.Fatalf("parseSend returned error: %v", err)
	}
	if title != "Deploy?" || string(flags.context) != contextJSON || string(flags.onExpire) != onExpireJSON {
		t.Fatalf("flags=%+v title=%q", flags, title)
	}
}

func TestParseSendSupportsDecisionBrief(t *testing.T) {
	decisionContext := `{"schema_version":1,"reason":"Production risk","proposed_change":"Deploy 2.18.0","risk_level":"high","reversibility":"reversible"}`
	flags, title, err := parseSend([]string{
		"Deploy?",
		"--decision-class", "production_deployment",
		"--decision-context-json", decisionContext,
	}, "ask")
	if err != nil {
		t.Fatalf("parseSend returned error: %v", err)
	}
	if title != "Deploy?" || flags.decisionClass != "production_deployment" || string(flags.decisionContext) != decisionContext {
		t.Fatalf("flags=%+v title=%q", flags, title)
	}

	if _, _, err := parseSend([]string{"Deploy?", "--decision-context-json", `[]`}, "ask"); err == nil {
		t.Fatal("parseSend accepted an array as decision context JSON")
	}
}

func TestParseSendSupportsPaidActionControls(t *testing.T) {
	controlsJSON := `[{"key":"retry","label":"Retry worker","kind":"callback"}]`
	flags, title, err := parseSend([]string{
		"Worker failed",
		"--callback-url", "https://example.com/actionbox",
		"--controls-json", controlsJSON,
	}, "send")
	if err != nil {
		t.Fatalf("parseSend returned error: %v", err)
	}
	if title != "Worker failed" || string(flags.controls) != controlsJSON {
		t.Fatalf("flags=%+v title=%q", flags, title)
	}

	if _, _, err := parseSend([]string{"Worker failed", "--controls-json", `{"key":"retry"}`}, "send"); err == nil {
		t.Fatal("parseSend accepted an object as controls JSON")
	}
}

func TestParseSendRejectsNonArrayContext(t *testing.T) {
	if _, _, err := parseSend([]string{"Title", "--context-json", `{"type":"key_value"}`}, "send"); err == nil {
		t.Fatal("parseSend accepted an object as context JSON")
	}
}

func TestReadJSONObjectSupportsInlineAndFileInput(t *testing.T) {
	inline := `{"type":"text","label":"Reason"}`
	parsed, err := readJSONObject(inline, "interaction")
	if err != nil {
		t.Fatalf("readJSONObject inline returned error: %v", err)
	}
	if string(parsed) != inline {
		t.Fatalf("inline JSON = %s", parsed)
	}

	path := filepath.Join(t.TempDir(), "response.json")
	fileJSON := `{"type":"boolean","value":true}`
	if err := os.WriteFile(path, []byte(fileJSON), 0o600); err != nil {
		t.Fatalf("write response fixture: %v", err)
	}
	parsed, err = readJSONObject("@"+path, "response")
	if err != nil {
		t.Fatalf("readJSONObject file returned error: %v", err)
	}
	if string(parsed) != fileJSON {
		t.Fatalf("file JSON = %s", parsed)
	}

	if _, err := readJSONObject(`[]`, "response"); err == nil {
		t.Fatal("readJSONObject accepted a JSON array")
	}
}

func TestReadJSONArrayNeverTreatsInlineJSONAsAFileName(t *testing.T) {
	inline := `[{"type":"code","content":"echo safe"}]`
	parsed, err := readJSONArray(inline, "context")
	if err != nil {
		t.Fatalf("readJSONArray inline returned error: %v", err)
	}
	if string(parsed) != inline {
		t.Fatalf("inline JSON = %s", parsed)
	}
}

func TestReadJSONRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", maxJSONInputBytes+1)), 0o600); err != nil {
		t.Fatalf("write oversized fixture: %v", err)
	}
	if _, err := readJSONArray("@"+path, "context"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("readJSONArray error = %v, want size error", err)
	}
}

func TestCommandUsageIncludesSafeConfigurationGuidance(t *testing.T) {
	var output strings.Builder
	commandUsage(&output, "configure")
	if !strings.Contains(output.String(), "--token-stdin") || !strings.Contains(output.String(), "owner-only") {
		t.Fatalf("configure help = %q", output.String())
	}
}

func TestCommandUsageIncludesMCPBridgeGuidance(t *testing.T) {
	var output strings.Builder
	commandUsage(&output, "mcp")
	if !strings.Contains(output.String(), "actionbox configure") || !strings.Contains(output.String(), "never sent through the MCP JSON-RPC stream") {
		t.Fatalf("mcp help = %q", output.String())
	}
}

func TestCommandUsageIncludesTypedJSONFlags(t *testing.T) {
	var output strings.Builder
	commandUsage(&output, "ask")
	if !strings.Contains(output.String(), "--interaction-json") {
		t.Fatalf("ask help = %q", output.String())
	}
	if !strings.Contains(output.String(), "--context-json") || !strings.Contains(output.String(), "--on-expire-json") {
		t.Fatalf("ask help missing context/expiration flags: %q", output.String())
	}
	if !strings.Contains(output.String(), "--controls-json") {
		t.Fatalf("ask help missing Action Controls flag: %q", output.String())
	}
	if !strings.Contains(output.String(), "--assignee-email") {
		t.Fatalf("ask help missing reviewer assignment flag: %q", output.String())
	}
	if !strings.Contains(output.String(), "--assignee-user-id") {
		t.Fatalf("ask help missing reviewer user ID flag: %q", output.String())
	}

	output.Reset()
	commandUsage(&output, "resolve")
	if !strings.Contains(output.String(), "--response-json") {
		t.Fatalf("resolve help = %q", output.String())
	}
}

func TestWaitResultExposesInteractionAndResponse(t *testing.T) {
	action := api.Action{
		ID:                 "act_1",
		Status:             "resolved",
		ResolutionOptionID: "approve",
		Interaction:        json.RawMessage(`{"type":"boolean","label":"Deploy?"}`),
		Response:           json.RawMessage(`{"type":"boolean","value":true}`),
		Receipt:            "receipt-token",
	}
	result := waitResult(action, action.Status)
	if string(result["interaction"].(json.RawMessage)) != string(action.Interaction) {
		t.Fatalf("interaction = %v", result["interaction"])
	}
	if string(result["response"].(json.RawMessage)) != string(action.Response) {
		t.Fatalf("response = %v", result["response"])
	}
	if result["receipt"] != action.Receipt {
		t.Fatalf("receipt = %v", result["receipt"])
	}
}

func TestRunWithWatchSendsLifecycleAndPreservesSuccess(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		paths = append(paths, request.URL.Path)
		methods = append(methods, request.Method)
		mu.Unlock()
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	err := runWithWatch([]string{
		"--watch-url", server.URL + "/hb/hb_test",
		"--", "sh", "-c", "exit 0",
	})
	if err != nil {
		t.Fatalf("runWithWatch returned error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(paths, ",") != "/hb/hb_test/start,/hb/hb_test/success" {
		t.Fatalf("heartbeat paths = %v", paths)
	}
	if strings.Join(methods, ",") != "POST,POST" {
		t.Fatalf("heartbeat methods = %v", methods)
	}
}

func TestRunWithWatchPreservesChildFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	err := runWithWatch([]string{
		"--watch-url", server.URL + "/hb/hb_test",
		"--", "sh", "-c", "exit 7",
	})
	if err == nil {
		t.Fatal("runWithWatch returned nil for a failing child")
	}
	exitErr, ok := err.(*cliExitError)
	if !ok || exitErr.code != 7 {
		t.Fatalf("runWithWatch error = %T %+v, want child exit code 7", err, err)
	}
}

func TestRunWithWatchReadsCapabilityFromEnvironment(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.Path)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	t.Setenv("ACTIONBOX_WATCH_URL", server.URL+"/hb/hb_env")

	if err := runWithWatch([]string{"--", "sh", "-c", "exit 0"}); err != nil {
		t.Fatalf("runWithWatch returned error: %v", err)
	}
	if strings.Join(paths, ",") != "/hb/hb_env/start,/hb/hb_env/success" {
		t.Fatalf("heartbeat paths = %v", paths)
	}
}

func TestReorderFlagsDoesNotConsumeValuesForUnknownFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("priority", "", "")
	got := reorderFlags([]string{"Title", "--unknown", "value", "--priority", "high"}, fs)
	want := []string{"--unknown", "--priority", "high", "Title", "value"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("reordered args = %q, want %q", got, want)
	}
}

func TestReorderFlagsDiscoversNewValueAndBooleanFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("future-value", "", "")
	fs.Bool("future-switch", false, "")
	got := reorderFlags([]string{"Title", "--future-value", "value", "--future-switch"}, fs)
	want := []string{"--future-value", "value", "--future-switch", "Title"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("reordered args = %q, want %q", got, want)
	}
}

func TestOptionValuesNormalizesWhitespaceLikePublishedSDKs(t *testing.T) {
	id, label := optionValues("  Ship   now  ")
	if id != "ship-now" || label != "Ship   now" {
		t.Fatalf("optionValues = (%q, %q)", id, label)
	}
}

func TestOptionValuesSanitizesGeneratedIDLikePublishedSDKs(t *testing.T) {
	id, label := optionValues("  Deploy (prod)!  ")
	if id != "deploy-prod" || label != "Deploy (prod)!" {
		t.Fatalf("optionValues = (%q, %q)", id, label)
	}

	id, label = optionValues("?!")
	if id != "" || label != "?!" {
		t.Fatalf("punctuation-only optionValues = (%q, %q)", id, label)
	}
}

func TestWaitChecksOriginalApprovalBinding(t *testing.T) {
	for _, tc := range []struct {
		name, actor, fingerprint, content string
		policy, override, ok              bool
	}{
		{"human", "user", "original", "content", false, false, true},
		{"machine", "source", "original", "content", false, false, false},
		{"fallback", "system", "original", "content", false, false, false},
		{"changed", "user", "changed", "changed", false, false, false},
		{"policy changed", "user", "changed", "changed", true, false, false},
		{"roster", "user", "changed", "content", true, false, true},
		{"override", "source", "original", "content", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initial := api.Action{ID: "act_test", Status: "open", ActionVersion: 1, Fingerprint: "original", ContentFingerprint: "content"}
			if tc.policy {
				initial.ApprovalPolicy = &api.ApprovalPolicy{AllowSourceOverride: tc.override}
			}
			final := initial
			final.Status = "resolved"
			final.ResolvedByType = tc.actor
			final.Fingerprint = tc.fingerprint
			final.ContentFingerprint = tc.content
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": final})
			}))
			defer server.Close()
			err := waitForDecision(api.New(server.URL, "test"), initial, sendFlags{timeout: time.Second, quiet: true})
			if (err == nil) != tc.ok {
				t.Fatalf("error = %v, want success %v", err, tc.ok)
			}
		})
	}
}

func TestParseSendSupportsChatExclusion(t *testing.T) {
	flags, _, err := parseSend([]string{"Private review", "--no-chat"}, "send")
	if err != nil {
		t.Fatal(err)
	}
	if !flags.noChat {
		t.Fatal("--no-chat was not parsed")
	}
}
