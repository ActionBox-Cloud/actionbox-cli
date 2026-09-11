package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBodyBytes = 1 << 20

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Health struct {
	Status      string `json:"status"`
	Database    string `json:"database,omitempty"`
	Environment string `json:"environment,omitempty"`
}

func (e APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func New(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: 40 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, input any, output any, idempotencyKey string) error {
	return c.doWithHeaders(ctx, method, path, input, output, idempotencyKey, nil)
}

func (c *Client) doWithHeaders(ctx context.Context, method, path string, input any, output any, idempotencyKey string, extra http.Header) error {
	var encoded []byte
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		encoded = data
	}
	retryable := requestCanBeRetried(method, idempotencyKey)
	for attempt := 0; attempt < 3; attempt++ {
		var body io.Reader
		if encoded != nil {
			body = bytes.NewReader(encoded)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("X-Actionbox-Client", "cli")
		for key, values := range extra {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
		if input != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			if retryable && attempt < 2 && waitForRetry(ctx, attempt, "") == nil {
				continue
			}
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if len(data) > maxResponseBodyBytes {
			return fmt.Errorf("Actionbox response exceeds %d bytes", maxResponseBodyBytes)
		}
		if retryable && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && attempt < 2 {
			if waitErr := waitForRetry(ctx, attempt, resp.Header.Get("Retry-After")); waitErr == nil {
				continue
			} else {
				return waitErr
			}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			var envelope struct {
				Error APIError `json:"error"`
			}
			_ = json.Unmarshal(data, &envelope)
			if envelope.Error.Message == "" {
				envelope.Error.Message = strings.TrimSpace(string(data))
			}
			return envelope.Error
		}
		if output == nil || len(data) == 0 {
			return nil
		}
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(data, &envelope); err == nil && len(envelope.Data) > 0 {
			return json.Unmarshal(envelope.Data, output)
		}
		return json.Unmarshal(data, output)
	}
	return fmt.Errorf("request failed after retries")
}

func requestCanBeRetried(method, idempotencyKey string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions || idempotencyKey != ""
}

func waitForRetry(ctx context.Context, attempt int, retryAfter string) error {
	delay := retryAfterDelay(retryAfter, time.Now())
	if delay < 0 {
		base := 250 * time.Millisecond * time.Duration(1<<attempt)
		delay = base + time.Duration(rand.Int63n(int64(base/2)+1))
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryAfterDelay(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return -1
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return -1
		}
		return min(time.Duration(seconds)*time.Second, 30*time.Second)
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return -1
	}
	return min(max(when.Sub(now), 0), 30*time.Second)
}

type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Style string `json:"style"`
}

type ApprovalPolicy struct {
	SchemaVersion       int    `json:"schema_version"`
	Mode                string `json:"mode"`
	RequiredApprovals   int    `json:"required_approvals,omitempty"`
	ApprovalOptionID    string `json:"approval_option_id,omitempty"`
	RejectionOptionID   string `json:"rejection_option_id,omitempty"`
	AllowSourceOverride bool   `json:"allow_source_override"`
}

type Action struct {
	ContentFingerprint string          `json:"content_fingerprint"`
	ResolvedByType     string          `json:"resolved_by_type"`
	ID                 string          `json:"id"`
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	Priority           string          `json:"priority"`
	Status             string          `json:"status"`
	Environment        string          `json:"environment"`
	ActionVersion      int             `json:"action_version"`
	Fingerprint        string          `json:"fingerprint"`
	ResolutionOptionID string          `json:"resolution_option_id"`
	ResolutionReason   string          `json:"resolution_reason"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	Options            []Option        `json:"options"`
	Interaction        json.RawMessage `json:"interaction"`
	Response           json.RawMessage `json:"response"`
	Receipt            string          `json:"receipt"`
	Context            json.RawMessage `json:"context"`
	DecisionClass      string          `json:"decision_class"`
	DecisionContext    json.RawMessage `json:"decision_context"`
	ContextRequest     json.RawMessage `json:"context_request"`
	OnExpire           json.RawMessage `json:"on_expire"`
	ApprovalPolicy     *ApprovalPolicy `json:"approval_policy"`
	ApprovalProgress   json.RawMessage `json:"approval_progress"`
}

type UpdateAction struct {
	ChatDelivery    string          `json:"chat_delivery,omitempty"`
	Context         json.RawMessage `json:"context,omitempty"`
	DecisionClass   *string         `json:"decision_class,omitempty"`
	DecisionContext json.RawMessage `json:"decision_context,omitempty"`
}

type CreateAction struct {
	ChatDelivery    string          `json:"chat_delivery,omitempty"`
	Title           string          `json:"title"`
	Description     string          `json:"description,omitempty"`
	Priority        string          `json:"priority,omitempty"`
	OpenURL         string          `json:"open_url,omitempty"`
	DedupeKey       string          `json:"dedupe_key,omitempty"`
	CallbackURL     string          `json:"callback_url,omitempty"`
	ExpiresAt       string          `json:"expires_at,omitempty"`
	OnExpire        json.RawMessage `json:"on_expire,omitempty"`
	Context         json.RawMessage `json:"context,omitempty"`
	DecisionClass   string          `json:"decision_class,omitempty"`
	DecisionContext json.RawMessage `json:"decision_context,omitempty"`
	Options         []Option        `json:"options,omitempty"`
	Interaction     json.RawMessage `json:"interaction,omitempty"`
	Controls        json.RawMessage `json:"controls,omitempty"`
	Metadata        map[string]any  `json:"metadata,omitempty"`
	AssigneeEmail   string          `json:"assignee_email,omitempty"`
	AssigneeUserID  string          `json:"assignee_user_id,omitempty"`
	Visibility      string          `json:"visibility,omitempty"`
	ReviewerEmails  []string        `json:"reviewer_emails,omitempty"`
	ReviewerUserIDs []string        `json:"reviewer_user_ids,omitempty"`
	ApprovalPolicy  *ApprovalPolicy `json:"approval_policy,omitempty"`
}

// Watch is the safe Watch representation returned by the API.  A heartbeat
// capability URL is intentionally only present on CreateWatch's response.
type Watch struct {
	ID                 string  `json:"id"`
	WorkspaceID        string  `json:"workspace_id"`
	SourceID           string  `json:"source_id"`
	Name               string  `json:"name"`
	ScheduleType       string  `json:"schedule_type"`
	IntervalSeconds    *int    `json:"interval_seconds"`
	CronExpression     *string `json:"cron_expression"`
	Timezone           string  `json:"timezone"`
	GraceSeconds       int     `json:"grace_seconds"`
	MaxRuntimeSeconds  *int    `json:"max_runtime_seconds"`
	Priority           string  `json:"priority"`
	RunbookURL         *string `json:"runbook_url"`
	Status             string  `json:"status"`
	DerivedStatus      string  `json:"derived_status"`
	Late               bool    `json:"late"`
	Running            bool    `json:"running"`
	TokenPrefix        string  `json:"token_prefix"`
	TokenRotatedAt     string  `json:"token_rotated_at"`
	ArmedAt            *string `json:"armed_at"`
	LastHeartbeatAt    *string `json:"last_heartbeat_at"`
	LastStartedAt      *string `json:"last_started_at"`
	LastSuccessAt      *string `json:"last_success_at"`
	LastFailureAt      *string `json:"last_failure_at"`
	ActiveRunStartedAt *string `json:"active_run_started_at"`
	RunDeadlineAt      *string `json:"run_deadline_at"`
	NextExpectedAt     *string `json:"next_expected_at"`
	NextEvaluationAt   *string `json:"next_evaluation_at"`
	IncidentActionID   *string `json:"incident_action_id"`
	IncidentKind       *string `json:"incident_kind"`
	ArchivedAt         *string `json:"archived_at"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

type CreatedWatch struct {
	Watch
	HeartbeatURL string `json:"heartbeat_url"`
}

type CreateWatch struct {
	SourceID          string  `json:"source_id,omitempty"`
	Name              string  `json:"name"`
	ScheduleType      string  `json:"schedule_type"`
	IntervalSeconds   *int    `json:"interval_seconds,omitempty"`
	CronExpression    *string `json:"cron_expression,omitempty"`
	Timezone          string  `json:"timezone,omitempty"`
	GraceSeconds      int     `json:"grace_seconds,omitempty"`
	MaxRuntimeSeconds *int    `json:"max_runtime_seconds,omitempty"`
	Priority          string  `json:"priority,omitempty"`
	RunbookURL        string  `json:"runbook_url,omitempty"`
	SignalMethod      string  `json:"signal_method,omitempty"`
}

type WatchEvent struct {
	ID          string         `json:"id"`
	WatchID     string         `json:"watch_id"`
	EventType   string         `json:"event_type"`
	StateBefore string         `json:"state_before"`
	StateAfter  string         `json:"state_after"`
	Payload     map[string]any `json:"payload"`
	CreatedAt   string         `json:"created_at"`
}

func (c *Client) ListWatches(ctx context.Context) ([]Watch, error) {
	var result []Watch
	err := c.do(ctx, http.MethodGet, "/v1/source/watches", nil, &result, "")
	return result, err
}

func (c *Client) CreateWatch(ctx context.Context, payload CreateWatch) (CreatedWatch, error) {
	var result CreatedWatch
	err := c.do(ctx, http.MethodPost, "/v1/source/watches", payload, &result, "")
	return result, err
}

func (c *Client) PauseWatch(ctx context.Context, id string) (Watch, error) {
	var result Watch
	err := c.do(ctx, http.MethodPost, "/v1/source/watches/"+url.PathEscape(id)+"/pause", nil, &result, "")
	return result, err
}

func (c *Client) ResumeWatch(ctx context.Context, id string) (Watch, error) {
	var result Watch
	err := c.do(ctx, http.MethodPost, "/v1/source/watches/"+url.PathEscape(id)+"/resume", nil, &result, "")
	return result, err
}

func (c *Client) RotateWatchToken(ctx context.Context, id string) (CreatedWatch, error) {
	var result CreatedWatch
	err := c.do(ctx, http.MethodPost, "/v1/source/watches/"+url.PathEscape(id)+"/token/rotate", nil, &result, "")
	return result, err
}

func (c *Client) ArchiveWatch(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/source/watches/"+url.PathEscape(id), nil, nil, "")
}

func (c *Client) CreateAction(ctx context.Context, payload CreateAction, key string) (Action, error) {
	return c.createAction(ctx, payload, key, false)
}

func (c *Client) CreateBlockingAction(ctx context.Context, payload CreateAction, key string) (Action, error) {
	return c.createAction(ctx, payload, key, true)
}

func (c *Client) createAction(ctx context.Context, payload CreateAction, key string, blocking bool) (Action, error) {
	var result Action
	extra := http.Header{}
	if blocking {
		extra.Set("X-Actionbox-Blocking", "true")
	}
	err := c.doWithHeaders(ctx, http.MethodPost, "/v1/actions", payload, &result, key, extra)
	return result, err
}

func (c *Client) GetAction(ctx context.Context, id string, waitSeconds ...int) (Action, error) {
	var result Action
	path := "/v1/source/actions/" + url.PathEscape(id)
	if len(waitSeconds) > 0 && waitSeconds[0] > 0 {
		path += "?wait_seconds=" + strconv.Itoa(min(waitSeconds[0], 30))
	}
	err := c.do(ctx, http.MethodGet, path, nil, &result, "")
	return result, err
}

func (c *Client) UpdateAction(ctx context.Context, id string, payload UpdateAction) (Action, error) {
	var result Action
	err := c.do(ctx, http.MethodPatch, "/v1/actions/"+url.PathEscape(id), payload, &result, "")
	return result, err
}

func (c *Client) MarkContextUnavailable(ctx context.Context, id, reason, reasonCode string) (Action, error) {
	var result Action
	payload := map[string]string{"reason": reason, "reason_code": reasonCode}
	err := c.do(ctx, http.MethodPost, "/v1/source/actions/"+url.PathEscape(id)+"/context-request/unavailable", payload, &result, "")
	return result, err
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var result Health
	err := c.do(ctx, http.MethodGet, "/health", nil, &result, "")
	return result, err
}

func (c *Client) Resolve(ctx context.Context, id, optionID, reason string) (Action, error) {
	return c.resolve(ctx, id, optionID, nil, reason, 0, "", false)
}

// ResolveAtVersion binds a machine resolution to the Action snapshot that the
// caller reviewed. A zero version preserves compatibility with old callers.
func (c *Client) ResolveAtVersion(ctx context.Context, id, optionID, reason string, actionVersion int, fingerprint string) (Action, error) {
	return c.resolve(ctx, id, optionID, nil, reason, actionVersion, fingerprint, false)
}

func (c *Client) ResolveWithResponse(
	ctx context.Context,
	id string,
	optionID string,
	response json.RawMessage,
	reason string,
) (Action, error) {
	return c.resolve(ctx, id, optionID, response, reason, 0, "", false)
}

// ResolveWithResponseAtVersion binds a typed machine resolution to an Action
// snapshot. A zero version preserves compatibility with old callers.
func (c *Client) ResolveWithResponseAtVersion(
	ctx context.Context,
	id string,
	optionID string,
	response json.RawMessage,
	reason string,
	actionVersion int,
	fingerprint string,
) (Action, error) {
	return c.resolve(ctx, id, optionID, response, reason, actionVersion, fingerprint, false)
}

func (c *Client) ResolveApprovalOverrideAtVersion(
	ctx context.Context,
	id string,
	optionID string,
	response json.RawMessage,
	reason string,
	actionVersion int,
	fingerprint string,
) (Action, error) {
	return c.resolve(ctx, id, optionID, response, reason, actionVersion, fingerprint, true)
}

type resolveActionRequest struct {
	ActionVersion    int             `json:"action_version,omitempty"`
	Fingerprint      string          `json:"fingerprint,omitempty"`
	OptionID         string          `json:"option_id,omitempty"`
	Response         json.RawMessage `json:"response,omitempty"`
	Reason           string          `json:"reason,omitempty"`
	ApprovalOverride bool            `json:"approval_override,omitempty"`
}

func (c *Client) resolve(
	ctx context.Context,
	id string,
	optionID string,
	response json.RawMessage,
	reason string,
	actionVersion int,
	fingerprint string,
	approvalOverride bool,
) (Action, error) {
	var result Action
	payload := resolveActionRequest{
		ActionVersion:    actionVersion,
		Fingerprint:      fingerprint,
		OptionID:         optionID,
		Response:         response,
		Reason:           reason,
		ApprovalOverride: approvalOverride,
	}
	err := c.do(ctx, http.MethodPost, "/v1/actions/"+url.PathEscape(id)+"/resolve", payload, &result, "")
	return result, err
}

func (c *Client) Cancel(ctx context.Context, id, reason string) (Action, error) {
	var result Action
	err := c.do(ctx, http.MethodPost, "/v1/actions/"+url.PathEscape(id)+"/cancel", map[string]string{"reason": reason}, &result, "")
	return result, err
}
