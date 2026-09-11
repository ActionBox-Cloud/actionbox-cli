package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetActionRetriesTransientResponses(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Actionbox-Client") != "cli" {
			t.Fatalf("client header = %q", request.Header.Get("X-Actionbox-Client"))
		}
		if request.URL.Path != "/v1/source/actions/act_1" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if calls.Add(1) < 3 {
			writer.WriteHeader(http.StatusBadGateway)
			_, _ = writer.Write([]byte(`{"error":{"code":"TEMPORARY","message":"try again"}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"data":{"id":"act_1","title":"Test","status":"open","created_at":"2026-08-11T12:00:00Z","updated_at":"2026-08-11T12:00:00Z"}}`)
	}))
	defer server.Close()

	action, err := New(server.URL, "token").GetAction(context.Background(), "act_1")
	if err != nil {
		t.Fatalf("GetAction returned error: %v", err)
	}
	if action.ID != "act_1" || calls.Load() != 3 {
		t.Fatalf("action=%+v calls=%d", action, calls.Load())
	}
}

func TestReleasedClientShapeReadsFutureActionAndLegacyDecisionSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"data":{"id":"act_future","title":"Deploy?","description":"","priority":"high","status":"open","environment":"live","action_version":3,"fingerprint":"sha256:bound","resolution_option_id":"","resolution_reason":"","created_at":"2026-08-27T00:00:00Z","updated_at":"2026-08-27T00:00:00Z","options":[],"interaction":null,"response":null,"receipt":"","context":[{"type":"logs","title":"Decision summary","content":"Proposed change: Deploy 2.18.0.","_actionbox_projection":"decision_context_v1"}],"on_expire":{"type":"return_expired"},"decision_context":{"schema_version":1,"reason":"Staging passed.","proposed_change":"Deploy 2.18.0.","risk_level":"high","reversibility":"reversible"},"future_server_field":{"safe":true}}}`)
	}))
	defer server.Close()

	client := New(server.URL, "axb_test")
	action, err := client.GetAction(context.Background(), "act_future")
	if err != nil {
		t.Fatal(err)
	}
	if action.ActionVersion != 3 || action.Fingerprint != "sha256:bound" {
		t.Fatalf("binding = (%d, %q)", action.ActionVersion, action.Fingerprint)
	}
	var blocks []map[string]any
	if err := json.Unmarshal(action.Context, &blocks); err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0]["content"] != "Proposed change: Deploy 2.18.0." {
		t.Fatalf("legacy context = %#v", blocks)
	}
}

func TestUpdateActionAnswersReviewerContextRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPatch || request.URL.Path != "/v1/actions/act_1" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		var payload UpdateAction
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if string(payload.Context) != `[{"type":"logs","content":"canary passed"}]` {
			t.Fatalf("context = %s", payload.Context)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"data":{"id":"act_1","title":"Deploy?","status":"open","context_request":null}}`)
	}))
	defer server.Close()

	action, err := New(server.URL, "token").UpdateAction(context.Background(), "act_1", UpdateAction{
		Context: json.RawMessage(`[{"type":"logs","content":"canary passed"}]`),
	})
	if err != nil || action.ID != "act_1" {
		t.Fatalf("action=%+v err=%v", action, err)
	}
}

func TestMutationWithoutIdempotencyKeyIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(`{"error":{"code":"TEMPORARY","message":"try again"}}`))
	}))
	defer server.Close()

	_, err := New(server.URL, "token").Resolve(context.Background(), "act_1", "approve", "")
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestResponseBodyLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(strings.Repeat("x", maxResponseBodyBytes+1)))
	}))
	defer server.Close()

	_, err := New(server.URL, "token").GetAction(context.Background(), "act_1")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("GetAction error = %v", err)
	}
}

func TestRetryAfterDelay(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	if got := retryAfterDelay("3", now); got != 3*time.Second {
		t.Fatalf("numeric retry delay = %s", got)
	}
	if got := retryAfterDelay(now.Add(2*time.Second).Format(http.TimeFormat), now); got != 2*time.Second {
		t.Fatalf("date retry delay = %s", got)
	}
}

func TestMinimalHealthResponseDoesNotInventBlankMetadata(t *testing.T) {
	encoded, err := json.Marshal(Health{Status: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"status":"ok"}` {
		t.Fatalf("health JSON = %s", encoded)
	}
}

func TestCreateBlockingActionMarksBlockingHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Actionbox-Blocking") != "true" {
			t.Fatalf("missing blocking header")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"data":{"id":"act_1","title":"Test","status":"open"}}`)
	}))
	defer server.Close()

	_, err := New(server.URL, "token").CreateBlockingAction(context.Background(), CreateAction{Title: "Test"}, "key")
	if err != nil {
		t.Fatalf("CreateBlockingAction returned error: %v", err)
	}
}

func TestCreateActionSendsTypedInteractionAndExposesIt(t *testing.T) {
	interaction := json.RawMessage(`{"type":"boolean","label":"Deploy?"}`)
	contextJSON := json.RawMessage(`[{"type":"key_value","items":{"environment":"production"}}]`)
	onExpire := json.RawMessage(`{"type":"return_expired"}`)
	controls := json.RawMessage(`[{"key":"retry","label":"Retry","kind":"callback"}]`)
	decisionContext := json.RawMessage(`{"schema_version":1,"reason":"Production risk","proposed_change":"Deploy","risk_level":"high","reversibility":"reversible"}`)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Options         []Option        `json:"options"`
			Interaction     json.RawMessage `json:"interaction"`
			Context         json.RawMessage `json:"context"`
			OnExpire        json.RawMessage `json:"on_expire"`
			Controls        json.RawMessage `json:"controls"`
			DecisionClass   string          `json:"decision_class"`
			DecisionContext json.RawMessage `json:"decision_context"`
			AssigneeEmail   string          `json:"assignee_email"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode create request: %v", err)
		}
		if len(payload.Options) != 0 {
			t.Fatalf("options = %+v", payload.Options)
		}
		if string(payload.Interaction) != string(interaction) {
			t.Fatalf("interaction = %s", payload.Interaction)
		}
		if string(payload.Context) != string(contextJSON) || string(payload.OnExpire) != string(onExpire) {
			t.Fatalf("context=%s on_expire=%s", payload.Context, payload.OnExpire)
		}
		if string(payload.Controls) != string(controls) {
			t.Fatalf("controls=%s", payload.Controls)
		}
		if payload.DecisionClass != "production_deployment" || string(payload.DecisionContext) != string(decisionContext) {
			t.Fatalf("decision_class=%q decision_context=%s", payload.DecisionClass, payload.DecisionContext)
		}
		if payload.AssigneeEmail != "reviewer@example.com" {
			t.Fatalf("assignee_email=%q", payload.AssigneeEmail)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"data":{"id":"act_1","title":"Deploy?","status":"open","interaction":{"type":"boolean","label":"Deploy?"},"context":[{"type":"key_value","items":{"environment":"production"}}],"on_expire":{"type":"return_expired"},"response":null}}`)
	}))
	defer server.Close()

	action, err := New(server.URL, "token").CreateAction(context.Background(), CreateAction{
		Title:           "Deploy?",
		Interaction:     interaction,
		Context:         contextJSON,
		OnExpire:        onExpire,
		Controls:        controls,
		DecisionClass:   "production_deployment",
		DecisionContext: decisionContext,
		AssigneeEmail:   "reviewer@example.com",
	}, "key")
	if err != nil {
		t.Fatalf("CreateAction returned error: %v", err)
	}
	if string(action.Interaction) != string(interaction) || string(action.Context) != string(contextJSON) || string(action.OnExpire) != string(onExpire) || string(action.Response) != "null" {
		t.Fatalf("action interaction=%s context=%s on_expire=%s response=%s", action.Interaction, action.Context, action.OnExpire, action.Response)
	}
}

func TestCreateActionSendsAssigneeUserID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			AssigneeUserID string `json:"assignee_user_id"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode create request: %v", err)
		}
		if payload.AssigneeUserID != "usr_reviewer" {
			t.Fatalf("assignee_user_id=%q", payload.AssigneeUserID)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"data":{"id":"act_1","title":"Deploy?","status":"open"}}`)
	}))
	defer server.Close()

	_, err := New(server.URL, "token").CreateAction(context.Background(), CreateAction{
		Title:          "Deploy?",
		AssigneeUserID: "usr_reviewer",
	}, "key")
	if err != nil {
		t.Fatalf("CreateAction returned error: %v", err)
	}
}

func TestResolveWithResponseSendsTypedResponse(t *testing.T) {
	response := json.RawMessage(`{"type":"boolean","value":true}`)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			ActionVersion int             `json:"action_version"`
			Fingerprint   string          `json:"fingerprint"`
			OptionID      string          `json:"option_id"`
			Response      json.RawMessage `json:"response"`
			Reason        string          `json:"reason"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode resolve request: %v", err)
		}
		if payload.ActionVersion != 4 || payload.Fingerprint != "sha256:"+strings.Repeat("a", 64) || payload.OptionID != "" || string(payload.Response) != string(response) || payload.Reason != "automated check" {
			t.Fatalf("payload=%+v response=%s", payload, payload.Response)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"data":{"id":"act_1","status":"resolved","interaction":{"type":"boolean","label":"Deploy?"},"response":{"type":"boolean","value":true}}}`)
	}))
	defer server.Close()

	action, err := New(server.URL, "token").ResolveWithResponseAtVersion(context.Background(), "act_1", "", response, "automated check", 4, "sha256:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatalf("ResolveWithResponse returned error: %v", err)
	}
	if string(action.Response) != string(response) {
		t.Fatalf("response = %s", action.Response)
	}
}

func TestWatchSourceContractAndHeartbeat(t *testing.T) {
	var createdPayload CreateWatch
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/source/watches" && request.Method == http.MethodGet {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(writer, `{"data":[{"id":"wat_1","name":"Backup","status":"healthy","derived_status":"healthy","schedule_type":"interval","interval_seconds":3600,"token_prefix":"hb_test"}]}`)
			return
		}
		if request.URL.Path == "/v1/source/watches" && request.Method == http.MethodPost {
			if err := json.NewDecoder(request.Body).Decode(&createdPayload); err != nil {
				t.Fatalf("decode watch request: %v", err)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(writer, `{"data":{"id":"wat_1","name":"Backup","status":"new","heartbeat_url":"http://example.invalid/hb/hb_secret"}}`)
			return
		}
		if request.URL.Path == "/hb/hb_test/success" {
			if request.Method != http.MethodPost {
				t.Fatalf("heartbeat method = %s, want POST", request.Method)
			}
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()
	client := New(server.URL, "source-token")
	watches, err := client.ListWatches(context.Background())
	if err != nil || len(watches) != 1 || watches[0].ID != "wat_1" {
		t.Fatalf("watches=%+v err=%v", watches, err)
	}
	created, err := client.CreateWatch(context.Background(), CreateWatch{Name: "Backup", ScheduleType: "interval", SignalMethod: "post"})
	if err != nil || created.HeartbeatURL == "" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	if createdPayload.SignalMethod != "post" {
		t.Fatalf("signal_method = %q, want post", createdPayload.SignalMethod)
	}
	if createdPayload.SourceID != "" {
		t.Fatalf("source_id = %q, want omitted", createdPayload.SourceID)
	}
	if err := SendHeartbeat(context.Background(), server.URL+"/hb/hb_test", "success"); err != nil {
		t.Fatalf("SendHeartbeat returned error: %v", err)
	}
}
