package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunMCPProxyForwardsCredentialOutsideJSONRPC(t *testing.T) {
	const token = "axb_live_test_secret"
	var authorization string
	var protocolVersion string
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		protocolVersion = request.Header.Get("MCP-Protocol-Version")
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		requestBody = string(body)
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	}))
	defer server.Close()

	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n")
	var output bytes.Buffer
	var log bytes.Buffer
	if err := runMCPProxy(context.Background(), server.URL, token, input, &output, &log); err != nil {
		t.Fatalf("runMCPProxy returned error: %v", err)
	}
	if authorization != "Bearer "+token {
		t.Fatalf("authorization = %q", authorization)
	}
	if protocolVersion != "2025-06-18" {
		t.Fatalf("protocol version = %q", protocolVersion)
	}
	if strings.Contains(requestBody, token) || strings.Contains(output.String(), token) || strings.Contains(log.String(), token) {
		t.Fatalf("source token crossed the JSON-RPC/log boundary")
	}
	if !strings.Contains(output.String(), `"tools":[]`) {
		t.Fatalf("proxy output = %q", output.String())
	}
}

func TestRunMCPProxyDoesNotForwardErrorBodyOrCredential(t *testing.T) {
	const token = "axb_live_test_secret"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte("invalid token: " + token))
	}))
	defer server.Close()

	input := strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/list"}` + "\n")
	var output bytes.Buffer
	var log bytes.Buffer
	err := runMCPProxy(context.Background(), server.URL, token, input, &output, &log)
	if err == nil {
		t.Fatal("runMCPProxy returned nil for unauthorized response")
	}
	if strings.Contains(output.String(), token) || strings.Contains(log.String(), token) {
		t.Fatalf("source token appeared in proxy error output")
	}
	if !strings.Contains(output.String(), `"code":-32001`) {
		t.Fatalf("proxy error output = %q", output.String())
	}
}

func TestRunMCPProxyReportsParseErrorAndContinues(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests++
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"ok":true}}`))
	}))
	defer server.Close()

	input := strings.NewReader("{invalid\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var output bytes.Buffer
	if err := runMCPProxy(context.Background(), server.URL, "axb_live_test", input, &output, io.Discard); err != nil {
		t.Fatalf("runMCPProxy returned error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if !strings.Contains(output.String(), `"code":-32700`) || !strings.Contains(output.String(), `"ok":true`) {
		t.Fatalf("proxy output = %q", output.String())
	}
}

func TestRunMCPProxyRejectsUnsafeHeaderValues(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests++
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	input := strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"tools/list\r\nX-Injected: true"}` + "\n")
	var output bytes.Buffer
	if err := runMCPProxy(context.Background(), server.URL, "axb_live_test", input, &output, io.Discard); err != nil {
		t.Fatalf("runMCPProxy returned error: %v", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
	if !strings.Contains(output.String(), `"code":-32001`) {
		t.Fatalf("proxy output = %q", output.String())
	}
}

func TestWriteMCPBodySupportsAllSSELineEndings(t *testing.T) {
	for name, separator := range map[string]string{"lf": "\n", "crlf": "\r\n", "cr": "\r"} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			body := []byte("event: message" + separator + `data: {"jsonrpc":"2.0","id":1,"result":{}}` + separator + separator)
			if err := writeMCPBody(&output, "text/event-stream", body); err != nil {
				t.Fatalf("writeMCPBody returned error: %v", err)
			}
			if !strings.Contains(output.String(), `"result":{}`) {
				t.Fatalf("output = %q", output.String())
			}
		})
	}
}

func TestWriteMCPBodyJoinsMultilineSSEDataWithinEachEvent(t *testing.T) {
	body := []byte("event: message\n" +
		`data: {"jsonrpc":"2.0",` + "\n" +
		`data: "id":1,` + "\n" +
		`data: "result":{"ok":true}}` + "\n\n" +
		`data: {"jsonrpc":"2.0","method":"notifications/progress"}`)
	var output bytes.Buffer

	if err := writeMCPBody(&output, "text/event-stream", body); err != nil {
		t.Fatalf("writeMCPBody returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("output lines = %d, want 2: %q", len(lines), output.String())
	}
	if !strings.Contains(lines[0], `"result":{"ok":true}`) {
		t.Fatalf("first output = %q", lines[0])
	}
	if !strings.Contains(lines[1], `"method":"notifications/progress"`) {
		t.Fatalf("second output = %q", lines[1])
	}
}
