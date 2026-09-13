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
		sb.WriteString("You are stuck in a loop. STOP what you are doing and switch to TESTING.\n")
		sb.WriteString("Pick one UNTESTED endpoint from the coverage state below and test it for a specific vulnerability.\n")
		sb.WriteString("Run a targeted command like: curl with SQLi payload, or sqlmap --batch against a specific endpoint.\n")
		sb.WriteString("After testing, call state_update to mark the endpoint as tested.")
	case "repetition":
		if len(h.toolCallNames) > 0 {
			lastTool := h.toolCallNames[len(h.toolCallNames)-1]
			sb.WriteString(fmt.Sprintf("You have called '%s' multiple times in a row. This is wasting time.\n", lastTool))
		}
		sb.WriteString("Switch to a DIFFERENT action: if you were discovering, start testing. If testing one vuln class, try another.")
	case "text_only":
		sb.WriteString("Execute a tool call now. Do not narrate — act.")
	case "progress":
		sb.WriteString(fmt.Sprintf("Progress: %d iterations, %d tool calls.",
			h.iterationCount, len(h.toolCallNames)))
	default:
		sb.WriteString("Continue.")
	}

	if coverageSummary != "" {
		sb.WriteString("\n\n")
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
