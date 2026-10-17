package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestContextHighlightingAndPlainOutput(t *testing.T) {
	raw := json.RawMessage(`[{"type":"code","title":"Python","language":"py","content":"def f():\n    return True"}]`)
	for _, color := range []bool{true, false} {
		var out bytes.Buffer
		if err := printContext(&out, raw, color); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "\x1b[") != color {
			t.Fatalf("unexpected colour output: %q", out.String())
		}
		if !color && !strings.Contains(out.String(), "def f():\n    return True") {
			t.Fatal("plain source changed")
		}
	}
}
func TestContextStripsSourceTerminalControls(t *testing.T) {
	var out bytes.Buffer
	raw := json.RawMessage(`[{"type":"code","title":"\u001b]evil","content":"\u001b[2Jhello\u0007","language":"unknown"}]`)
	if err := printContext(&out, raw, true); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.String(), "\x1b\a") {
		t.Fatal("source supplied terminal controls")
	}
}
