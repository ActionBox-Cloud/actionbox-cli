package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ActionBox-Cloud/actionbox-cli/internal/netutil"
)

// SendHeartbeat delivers a capability URL without ever including the URL in
// an error.  Callers can therefore safely surface errors in CI logs.
func SendHeartbeat(ctx context.Context, heartbeatURL, signal string) error {
	if signal == "" {
		signal = "ping"
	}
	if signal != "ping" && signal != "start" && signal != "success" && signal != "fail" {
		return errors.New("heartbeat signal must be ping, start, success, or fail")
	}
	parsed, err := url.Parse(strings.TrimSpace(heartbeatURL))
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("heartbeat URL is invalid")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && netutil.IsLoopbackHost(parsed.Hostname())) {
		return errors.New("heartbeat URL must use HTTPS unless it targets localhost")
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) != 2 || segments[0] != "hb" || segments[1] == "" || !strings.HasPrefix(segments[1], "hb_") {
		return errors.New("heartbeat URL must be an Actionbox heartbeat URL")
	}
	if signal != "ping" {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + signal
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), nil)
	if err != nil {
		return errors.New("heartbeat URL is invalid")
	}
	request.Header.Set("User-Agent", "actionbox-cli/heartbeat")
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		// net/http wraps transport failures with the full request URL.  Never
		// propagate that error because the URL contains the bearer capability.
		return errors.New("heartbeat delivery failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("heartbeat delivery failed with HTTP %d", response.StatusCode)
	}
	return nil
}
