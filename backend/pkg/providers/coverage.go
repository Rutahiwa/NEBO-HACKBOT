package providers

import (
	"fmt"
	"strings"
	"sync"
)

type HarnessHistory struct {
	mu             sync.Mutex
	toolCallNames  []string
	iterationCount int
	lastNewFinding int
	lastNewEndpoint int
}

func NewHarnessHistory() *HarnessHistory {
	return &HarnessHistory{
		toolCallNames: make([]string, 0),
	}
}

func (h *HarnessHistory) RecordToolCall(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.toolCallNames = append(h.toolCallNames, name)
}

func (h *HarnessHistory) RecordIteration() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.iterationCount++
}

func (h *HarnessHistory) RecordNewFinding() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastNewFinding = h.iterationCount
}

func (h *HarnessHistory) RecordNewEndpoint() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastNewEndpoint = h.iterationCount
}

func (h *HarnessHistory) IterationCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.iterationCount
}

func (h *HarnessHistory) ToolCallCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.toolCallNames)
}

func (h *HarnessHistory) DetectStall(threshold int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.iterationCount == 0 {
		return false
	}
	itersSinceNewFinding := h.iterationCount - h.lastNewFinding
	itersSinceNewEndpoint := h.iterationCount - h.lastNewEndpoint
	return itersSinceNewFinding > threshold && itersSinceNewEndpoint > threshold
}

func (h *HarnessHistory) DetectToolRepetition(threshold int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.toolCallNames) < threshold {
		return false
	}
	recent := h.toolCallNames[len(h.toolCallNames)-threshold:]
	first := recent[0]
	for _, name := range recent[1:] {
		if name != first {
			return false
		}
	}
	return true
}

func (h *HarnessHistory) GenerateNudge(stallType string) string {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch stallType {
	case "stall":
		return "You have not made new discoveries in several iterations. " +
			"Try a completely different approach — different tool, different endpoint, different technique. " +
			"Check get_state with section 'coverage' to see what areas remain untested."
	case "repetition":
		if len(h.toolCallNames) > 0 {
			lastTool := h.toolCallNames[len(h.toolCallNames)-1]
			return fmt.Sprintf("You have called '%s' repeatedly. Try a different tool or approach. "+
				"Check get_state to see untested areas.", lastTool)
		}
		return "You are repeating the same approach. Try something different."
	case "text_only":
		return "Make your next tool call now. Do not explain what you plan to do — execute it."
	case "progress":
		return fmt.Sprintf("Progress check: %d iterations, %d tool calls. "+
			"Keep working — check get_state with section 'coverage' to find untested areas.",
			h.iterationCount, len(h.toolCallNames))
	default:
		return "Continue testing. Use get_state to check coverage."
	}
}

type PhaseResult struct {
	PhaseName string
	Summary   string
	Error     error
}

func buildCoverageNudge(coveragePct float64, findingCount int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Coverage is at %.0f%%", coveragePct))
	if findingCount > 0 {
		sb.WriteString(fmt.Sprintf(" with %d finding(s) recorded", findingCount))
	}
	sb.WriteString(". ")
	if coveragePct < 30 {
		sb.WriteString("Many areas remain untested. Keep exploring and testing.")
	} else if coveragePct < 70 {
		sb.WriteString("Good progress. Continue testing untested endpoint-vulnerability combinations.")
	} else {
		sb.WriteString("High coverage achieved. Consider validating confirmed findings and wrapping up.")
	}
	return sb.String()
}
