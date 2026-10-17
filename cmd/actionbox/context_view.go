package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/alecthomas/chroma/v2/quick"
)

// Strip terminal control sequences supplied by a Source; colours only come from
// the formatter. JSON output retains the original data for machine consumers.
func terminalText(value string) string {
	return strings.Map(func(r rune) rune {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || r == '\u202e' || r == '\u202d' {
			return -1
		}
		return r
	}, value)
}

func printContext(out io.Writer, raw json.RawMessage, color bool) error {
	var blocks []map[string]json.RawMessage
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return err
	}
	for _, block := range blocks {
		var kind, title, source, language string
		_ = json.Unmarshal(block["type"], &kind)
		_ = json.Unmarshal(block["title"], &title)
		_ = json.Unmarshal(block["content"], &source)
		_ = json.Unmarshal(block["language"], &language)
		if title == "" {
			title = kind
		}
		fmt.Fprintf(out, "\n%s\n", terminalText(title))
		if kind == "json" {
			source = string(block["value"])
			language = "json"
		}
		if kind == "diff" {
			language = "diff"
		}
		if source == "" {
			data := block["items"]
			if len(data) == 0 {
				continue
			}
			source = string(data)
			language = "json"
		}
		if language == "json" {
			var formatted bytes.Buffer
			if json.Indent(&formatted, []byte(source), "", "  ") == nil {
				source = formatted.String()
			}
		}
		source = terminalText(source)
		language = strings.ToLower(strings.TrimSpace(language))
		aliases := map[string]string{"py": "python", "js": "javascript", "ts": "typescript", "sh": "bash", "shell": "bash", "yml": "yaml", "html": "xml", "golang": "go"}
		if alias := aliases[language]; alias != "" {
			language = alias
		}
		supported := strings.Contains("|python|javascript|typescript|json|bash|sql|yaml|go|dart|xml|css|diff|", "|"+language+"|") && language != ""
		var rendered bytes.Buffer
		if color && supported && len(source) <= 20000 && quick.Highlight(&rendered, source, language, "terminal256", "catppuccin-mocha") == nil {
			if _, err := out.Write(rendered.Bytes()); err != nil {
				return err
			}
			fmt.Fprintln(out)
		} else if _, err := fmt.Fprintln(out, source); err != nil {
			return err
		}
	}
	return nil
}
