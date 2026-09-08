package providers

import "github.com/vxcontrol/langchaingo/llms"

// applyContextWindow returns a view of the chain that keeps the pinned
// prefix (system + first human message) and only the last windowSize
// messages from the conversation history. This prevents the context
// from growing unboundedly while preserving the system prompt for
// KV-cache reuse.
//
// The full chain is still persisted to DB by the caller -- the window
// only affects what the LLM sees on each call.
func applyContextWindow(chain []llms.MessageContent, windowSize int) []llms.MessageContent {
	if windowSize <= 0 || len(chain) <= windowSize+2 {
		return chain // already fits, or disabled
	}

	// Find the prefix: system messages + first human message
	prefixEnd := 0
	for i, msg := range chain {
		prefixEnd = i + 1
		if msg.Role == llms.ChatMessageTypeHuman {
			break
		}
	}

	// If the remaining messages after prefix are within window, return as-is
	remaining := chain[prefixEnd:]
	if len(remaining) <= windowSize {
		return chain
	}

	// Build windowed chain: prefix + last windowSize messages
	windowed := make([]llms.MessageContent, 0, prefixEnd+windowSize)
	windowed = append(windowed, chain[:prefixEnd]...)
	windowed = append(windowed, remaining[len(remaining)-windowSize:]...)

	return windowed
}
