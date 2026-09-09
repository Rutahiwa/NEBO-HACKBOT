package providers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"pentagi/pkg/cast"

	"github.com/vxcontrol/langchaingo/llms"
)

// textToolCallJSON is the intermediate JSON structure that models sometimes
// emit as plain text instead of using the structured function-calling API.
type textToolCallJSON struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// reToolCallTag matches <tool_call>...</tool_call> blocks, including optional
// trailing tokens such as <|im_start|> that some models (e.g. Qwen) append.
var reToolCallTag = regexp.MustCompile(`(?s)<tool_call>\s*(.*?)\s*(?:<\|im_start\|>|</tool_call>)`)

// parseTextModeToolCalls attempts to extract tool calls that the model emitted
// as plain text content instead of using the structured tool-calling API.
//
// It recognises the following formats:
//  1. <tool_call>{"name":"X","arguments":{...}}</tool_call>
//  2. <tool_call>\n{"name":"X","arguments":{...}}\n</tool_call>
//  3. <tool_call>\n{"name":"X","arguments":{...}}\n<|im_start|>  (Qwen variant)
//  4. Bare JSON: {"name":"X","arguments":{...}} when it is the only content
//
// Returns nil when no valid tool calls are found.
func parseTextModeToolCalls(content string) []llms.ToolCall {
	var calls []llms.ToolCall

	// Strategy 1: extract from <tool_call> tags.
	matches := reToolCallTag.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		if tc := tryParseToolCallJSON(m[1]); tc != nil {
			calls = append(calls, *tc)
		}
	}

	if len(calls) > 0 {
		assignTextParseIDs(calls)
		return calls
	}

	// Strategy 2: bare JSON — the entire (trimmed) content is a single
	// tool-call object. Only attempt this when the content starts with '{'.
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") {
		if tc := tryParseToolCallJSON(trimmed); tc != nil {
			calls = append(calls, *tc)
			assignTextParseIDs(calls)
			return calls
		}
	}

	return nil
}

// tryParseToolCallJSON attempts to parse a single tool-call JSON blob.
// It returns nil when the blob is not a valid tool call.
func tryParseToolCallJSON(raw string) *llms.ToolCall {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var parsed textToolCallJSON
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}

	if parsed.Name == "" {
		return nil
	}

	args := string(parsed.Arguments)
	if args == "" || args == "null" {
		args = "{}"
	}

	// Sanitize control characters in arguments (same as structured path).
	args = cast.SanitizeJSONControlChars(args)
	if !json.Valid([]byte(args)) {
		args = "{}"
	}

	return &llms.ToolCall{
		Type: "function",
		FunctionCall: &llms.FunctionCall{
			Name:      parsed.Name,
			Arguments: args,
		},
	}
}

// assignTextParseIDs assigns deterministic IDs to parsed tool calls so that
// tool-call-response matching works correctly downstream.
func assignTextParseIDs(calls []llms.ToolCall) {
	for i := range calls {
		calls[i].ID = fmt.Sprintf("textparse-%d", i)
	}
}
