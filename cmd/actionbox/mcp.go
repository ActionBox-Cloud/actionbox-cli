package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ActionBox-Cloud/actionbox-cli/internal/config"
)

const maxMCPResponseBytes = 4 << 20

type mcpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpErrorMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   mcpError        `json:"error"`
}

func mcpProxy(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: actionbox mcp")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return runMCPProxy(context.Background(), cfg.BaseURL, cfg.Token, os.Stdin, os.Stdout, os.Stderr)
}

// runMCPProxy exposes the hosted stateless MCP endpoint as a local stdio server.
// The MCP client only sees this process; the Source token stays in Actionbox's
// owner-only CLI config and is never placed in the MCP JSON-RPC stream.
func runMCPProxy(ctx context.Context, baseURL, token string, input io.Reader, output, log io.Writer) error {
	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), maxMCPResponseBytes)
	sessionID := ""
	protocolVersion := ""

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var message mcpMessage
		if err := json.Unmarshal(line, &message); err != nil {
			if writeErr := writeMCPError(output, json.RawMessage("null"), -32700, "parse error"); writeErr != nil {
				return writeErr
			}
			continue
		}

		if currentProtocolVersion := mcpProtocolVersion(message.Params); currentProtocolVersion != "" {
			protocolVersion = currentProtocolVersion
		}
		body, contentType, responseSessionID, statusCode, err := postMCPMessage(ctx, client, baseURL, token, sessionID, protocolVersion, line, message)
		if responseSessionID != "" {
			sessionID = responseSessionID
		}
		if err != nil {
			if writeErr := writeMCPError(output, message.ID, -32001, err.Error()); writeErr != nil {
				return writeErr
			}
			if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
				return err
			}
			continue
		}
		if len(body) == 0 {
			continue
		}
		if err := writeMCPBody(output, contentType, body); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read MCP input: %w", err)
	}
	return nil
}

func postMCPMessage(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	token string,
	sessionID string,
	protocolVersion string,
	body []byte,
	message mcpMessage,
) ([]byte, string, string, int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/mcp", bytes.NewReader(body))
	if err != nil {
		return nil, "", "", 0, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("X-Actionbox-Client", "cli-mcp")
	if sessionID != "" {
		if !validMCPHeaderValue(sessionID) {
			return nil, "", "", 0, errors.New("invalid MCP session identifier")
		}
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	if message.Method != "" {
		if !validMCPHeaderValue(message.Method) {
			return nil, "", "", 0, errors.New("invalid MCP method")
		}
		request.Header.Set("Mcp-Method", message.Method)
	}
	if protocolVersion != "" {
		if !validMCPHeaderValue(protocolVersion) {
			return nil, "", "", 0, errors.New("invalid MCP protocol version")
		}
		request.Header.Set("MCP-Protocol-Version", protocolVersion)
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, "", "", 0, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxMCPResponseBytes+1))
	if err != nil {
		return nil, "", "", response.StatusCode, err
	}
	if len(responseBody) > maxMCPResponseBytes {
		return nil, "", "", response.StatusCode, fmt.Errorf("MCP response exceeds %d bytes", maxMCPResponseBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, "", "", response.StatusCode, fmt.Errorf("Actionbox MCP request failed with HTTP %d", response.StatusCode)
	}
	responseSessionID := response.Header.Get("Mcp-Session-Id")
	if responseSessionID != "" && !validMCPHeaderValue(responseSessionID) {
		return nil, "", "", response.StatusCode, errors.New("Actionbox MCP response contained an invalid session identifier")
	}
	return responseBody, response.Header.Get("Content-Type"), responseSessionID, response.StatusCode, nil
}

func validMCPHeaderValue(value string) bool {
	if len(value) > 1024 {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func mcpProtocolVersion(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var payload struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.ProtocolVersion)
}

func writeMCPBody(output io.Writer, contentType string, body []byte) error {
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		normalizedBody := strings.ReplaceAll(string(body), "\r\n", "\n")
		normalizedBody = strings.ReplaceAll(normalizedBody, "\r", "\n")
		dataLines := make([]string, 0, 1)
		flushEvent := func() error {
			if len(dataLines) == 0 {
				return nil
			}
			data := strings.Join(dataLines, "\n")
			dataLines = dataLines[:0]
			if strings.TrimSpace(data) == "" || strings.TrimSpace(data) == "[DONE]" {
				return nil
			}
			return writeMCPJSON(output, []byte(data))
		}
		for _, line := range strings.Split(normalizedBody, "\n") {
			if line == "" {
				if err := flushEvent(); err != nil {
					return err
				}
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(data, " ") {
				data = data[1:]
			}
			dataLines = append(dataLines, data)
		}
		return flushEvent()
	}
	return writeMCPJSON(output, body)
}

func writeMCPJSON(output io.Writer, body []byte) error {
	var compact bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		return fmt.Errorf("invalid MCP response: %w", err)
	}
	_, err := fmt.Fprintln(output, compact.String())
	return err
}

func writeMCPError(output io.Writer, id json.RawMessage, code int, message string) error {
	if len(id) == 0 {
		return nil
	}
	if !json.Valid(id) {
		id = json.RawMessage("null")
	}
	response := mcpErrorMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error:   mcpError{Code: code, Message: message},
	}
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(body))
	return err
}
