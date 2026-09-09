package providers

import (
	"fmt"
	"strings"

	"github.com/vxcontrol/langchaingo/llms"
)

// pruneStaleToolResults replaces tool result content in the chain with short
// stubs for all but the most recent keepRecent tool-call/response pairs.
// This is a pure string operation — no LLM call — that prevents the chain
// from growing unboundedly between summarization passes.
//
// The stub preserves the first non-empty line of the result (which usually
// contains the most meaningful summary) plus a pointer to memory search.
const defaultKeepRecentToolResults = 15
const pruneStubMaxLen = 300

func pruneStaleToolResults(chain []llms.MessageContent, keepRecent int) []llms.MessageContent {
	if keepRecent <= 0 {
		keepRecent = defaultKeepRecentToolResults
	}

	toolResponseIndices := make([]int, 0)
	for i := len(chain) - 1; i >= 0; i-- {
		if chain[i].Role == llms.ChatMessageTypeTool {
			toolResponseIndices = append(toolResponseIndices, i)
		}
	}

	if len(toolResponseIndices) <= keepRecent {
		return chain
	}

	indicesToPrune := toolResponseIndices[keepRecent:]

	for _, idx := range indicesToPrune {
		msg := &chain[idx]
		for j, part := range msg.Parts {
			if tcr, ok := part.(llms.ToolCallResponse); ok {
				if len(tcr.Content) > pruneStubMaxLen {
					summary := extractFirstMeaningfulLine(tcr.Content)
					tcr.Content = fmt.Sprintf("[%s] %s\n[... pruned %d bytes, use search_in_memory to retrieve full output ...]",
						tcr.Name, summary, len(tcr.Content))
					msg.Parts[j] = tcr
				}
			}
		}
	}

	return chain
}

// extractFirstMeaningfulLine returns the first non-empty, non-whitespace line
// from content, trimmed to a reasonable length. This usually captures the most
// informative summary line (e.g., "Nmap scan report for 10.0.0.1" or
// "sqlmap identified the following injection point(s)").
func extractFirstMeaningfulLine(content string) string {
	lines := strings.SplitN(content, "\n", 20)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 10 {
			if len(trimmed) > 150 {
				return trimmed[:150] + "..."
			}
			return trimmed
		}
	}
	if len(content) > 150 {
		return content[:150] + "..."
	}
	return content
}
