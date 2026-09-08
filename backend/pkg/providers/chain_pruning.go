package providers

import (
	"fmt"

	"github.com/vxcontrol/langchaingo/llms"
)

// pruneStaleToolResults replaces tool result content in the chain with short
// stubs for all but the most recent keepRecent tool-call/response pairs.
// This is a pure string operation — no LLM call — that prevents the chain
// from growing unboundedly between summarization passes.
const defaultKeepRecentToolResults = 5
const pruneStubMaxLen = 120

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
					stub := tcr.Content[:pruneStubMaxLen]
					tcr.Content = fmt.Sprintf("%s\n[... pruned %d bytes, use search_in_memory to retrieve ...]",
						stub, len(tcr.Content)-pruneStubMaxLen)
					msg.Parts[j] = tcr
				}
			}
		}
	}

	return chain
}
