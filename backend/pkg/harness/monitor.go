package harness

import (
	"fmt"
	"strings"
	"sync"
)

type HarnessConfig struct {
	MaxIterations          int
	StallThreshold         int
	RepetitionThreshold    int
	MaxConsecutiveTextOnly int
	CoverageCompleteAt     float64
}

func DefaultHarnessConfig() HarnessConfig {
	return HarnessConfig{
		MaxIterations:          500,
		StallThreshold:         5,
		RepetitionThreshold:    5,
		MaxConsecutiveTextOnly: 1,
		CoverageCompleteAt:     85.0,
	}
}

type HarnessHistory struct {
	mu              sync.Mutex
	toolCallNames   []string
	iterationCount  int
	lastNewFinding  int
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

func (h *HarnessHistory) GenerateNudge(stallType, coverageSummary string) string {
	h.mu.Lock()
	defer h.mu.Unlock()

	var sb strings.Builder

	switch stallType {
	case "stall":
		sb.WriteString("You have not made new discoveries in several iterations. ")
		sb.WriteString("Try a completely different approach — different tool, different endpoint, different technique.")
	case "repetition":
		if len(h.toolCallNames) > 0 {
			lastTool := h.toolCallNames[len(h.toolCallNames)-1]
			sb.WriteString(fmt.Sprintf("You have called '%s' repeatedly. Stop and try a different approach.", lastTool))
		} else {
			sb.WriteString("You are repeating the same approach. Try something different.")
		}
	case "text_only":
		sb.WriteString("Make your next tool call now. Do not explain what you plan to do — execute it.")
	case "progress":
		sb.WriteString(fmt.Sprintf("Progress check: %d iterations, %d tool calls. Keep working.",
			h.iterationCount, len(h.toolCallNames)))
	default:
		sb.WriteString("Continue testing.")
	}

	if coverageSummary == "" || strings.Contains(coverageSummary, "0/0 endpoint") {
		sb.WriteString("\n\nIMPORTANT: Your coverage state is EMPTY. You MUST call state_update to register endpoints you have discovered. " +
			"Example: call state_update with action 'add', section 'endpoints', and data containing the path, method, and params of each endpoint you found. " +
			"After registering endpoints, call state_update again with section 'endpoints' action 'update' to mark what you have tested. " +
			"Without registering endpoints, your testing progress is not tracked.")
	} else {
		sb.WriteString("\n\nCURRENT COVERAGE STATE:\n")
		sb.WriteString(coverageSummary)
	}

	return sb.String()
}

func BuildCoverageNudge(coveragePct float64, findingCount int) string {
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
