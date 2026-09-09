package providers

import "github.com/vxcontrol/langchaingo/llms"

// applyContextWindow returns a view of the chain that keeps the pinned
// prefix (system + first human message) and only the last windowSize
// messages from the conversation history. This prevents the context
// from growing unboundedly while preserving the system prompt for
// KV-cache reuse.
//
// The window boundary is adjusted to avoid cutting inside a tool-call
// exchange: if the cut point falls on a Tool response without its
// preceding AI message (which contains the ToolCall), we back up to
// include the AI message so the LLM sees a coherent pair.
//
// The full chain is still persisted to DB by the caller — the window
// only affects what the LLM sees on each call.
func applyContextWindow(chain []llms.MessageContent, windowSize int) []llms.MessageContent {
	if windowSize <= 0 || len(chain) <= windowSize+2 {
		return chain
	}

	// Find the prefix: system messages + first human message
	prefixEnd := 0
	for i, msg := range chain {
		prefixEnd = i + 1
		if msg.Role == llms.ChatMessageTypeHuman {
			break
		}
	}

	remaining := chain[prefixEnd:]
	if len(remaining) <= windowSize {
		return chain
	}

	cutIdx := len(remaining) - windowSize

	// Adjust cut point: don't start with a Tool response (orphaned without
	// its AI message). Back up until we hit a non-Tool message.
	for cutIdx > 0 && cutIdx < len(remaining) && remaining[cutIdx].Role == llms.ChatMessageTypeTool {
		cutIdx--
	}

	windowed := make([]llms.MessageContent, 0, prefixEnd+(len(remaining)-cutIdx))
	windowed = append(windowed, chain[:prefixEnd]...)
	windowed = append(windowed, remaining[cutIdx:]...)

	return windowed
}
