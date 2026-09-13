package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/tools"

	"github.com/sirupsen/logrus"
	"github.com/vxcontrol/langchaingo/llms"
)

type ChainPersister interface {
	UpdateMsgChain(ctx context.Context, params database.UpdateMsgChainParams) (database.Msgchain, error)
}

type MsgLogger interface {
	PutMsg(ctx context.Context, msgType database.MsglogType, taskID, subtaskID *int64, streamID int64, thinking, message string) (int64, error)
}

type LoopConfig struct {
	Caller        LLMCaller
	Tools         []BaseTool
	CoverageState *tools.CoverageState
	Harness       HarnessConfig

	ChainID   int64
	TaskID    int64
	SubtaskID int64
	FlowID    int64

	DB       ChainPersister
	MsgLog   MsgLogger
	Model    string
	Provider string
}

type LoopResult struct {
	FinalContent string
	Iterations   int
	ToolCalls    int
}

func RunLoop(ctx context.Context, cfg LoopConfig, chain []llms.MessageContent) (*LoopResult, error) {
	history := NewHarnessHistory()

	logger := logrus.WithContext(ctx).WithFields(logrus.Fields{
		"flow_id":    cfg.FlowID,
		"task_id":    cfg.TaskID,
		"subtask_id": cfg.SubtaskID,
		"chain_id":   cfg.ChainID,
		"harness":    "opencode",
	})

	logger.Info("harness: starting OpenCode-style agent loop")

	var lastContent string

	for iteration := 0; iteration < cfg.Harness.MaxIterations; iteration++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		history.RecordIteration()

		// Prune old tool results to keep context manageable
		chain = pruneStaleToolResults(chain, 15)

		// Call LLM directly
		callStart := time.Now()
		result, err := callLLM(ctx, cfg.Caller, chain, cfg.Tools)
		callDuration := time.Since(callStart).Seconds()
		if err != nil {
			logger.WithError(err).Error("harness: LLM call failed")
			return nil, err
		}

		// Persist usage
		if err := persistChainUpdate(ctx, cfg, chain, callDuration); err != nil {
			logger.WithError(err).Warn("harness: failed to persist chain")
		}

		// Log the agent's thinking/response to msglogs for frontend
		if result.Content != "" && cfg.MsgLog != nil {
			cfg.MsgLog.PutMsg(ctx, database.MsglogTypeAnswer, &cfg.TaskID, &cfg.SubtaskID, 0, "", result.Content)
		}

		// No tool calls → model is done (OpenCode exit pattern)
		if len(result.ToolCalls) == 0 {
			lastContent = result.Content
			logger.WithFields(logrus.Fields{
				"iteration":  iteration,
				"tool_calls": history.ToolCallCount(),
				"coverage":   coveragePct(cfg.CoverageState),
			}).Info("harness: model finished (no tool calls)")
			break
		}

		// Build AI message with tool calls
		aiMsg := llms.MessageContent{Role: llms.ChatMessageTypeAI}
		if result.Content != "" {
			aiMsg.Parts = append(aiMsg.Parts, llms.TextContent{Text: result.Content})
		}
		for _, tc := range result.ToolCalls {
			aiMsg.Parts = append(aiMsg.Parts, tc)
		}
		chain = append(chain, aiMsg)

		// Execute each tool call DIRECTLY (OpenCode pattern — no delegation)
		for _, tc := range result.ToolCalls {
			if tc.FunctionCall == nil {
				continue
			}

			funcName := tc.FunctionCall.Name
			funcArgs := tc.FunctionCall.Arguments
			history.RecordToolCall(funcName)

			logger.WithFields(logrus.Fields{
				"tool":      funcName,
				"iteration": iteration,
			}).Debug("harness: executing tool")

			tool, err := findTool(funcName, cfg.Tools)
			var response string
			if err != nil {
				response = fmt.Sprintf("Error: unknown tool '%s'. Available tools: %s", funcName, availableToolNames(cfg.Tools))
			} else {
				toolResult, toolErr := tool.Run(ctx, ToolCall{
					ID:   tc.ID,
					Name: funcName,
					Args: funcArgs,
				})
				if toolErr != nil {
					response = fmt.Sprintf("Error executing %s: %s", funcName, toolErr.Error())
				} else {
					response = toolResult.Content
				}
			}

			// Append tool response to chain
			chain = append(chain, llms.MessageContent{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: tc.ID,
						Name:       funcName,
						Content:    response,
					},
				},
			})
		}

		// Persist updated chain
		if err := persistChainUpdate(ctx, cfg, chain, 0); err != nil {
			logger.WithError(err).Warn("harness: failed to persist chain after tool calls")
		}

		// Harness checks every 3 iterations
		if iteration > 0 && iteration%3 == 0 {
			coveragePctVal := coveragePct(cfg.CoverageState)
			coverageSummary := ""
			if cfg.CoverageState != nil {
				coverageSummary = cfg.CoverageState.GetCoverageSummary()
			}

			if history.DetectStall(cfg.Harness.StallThreshold) {
				nudge := history.GenerateNudge("stall", coverageSummary)
				chain = append(chain, llms.TextParts(llms.ChatMessageTypeHuman, nudge))
				logger.WithField("coverage", coveragePctVal).Info("harness: stall detected, nudging")
			}

			if history.DetectToolRepetition(cfg.Harness.RepetitionThreshold) {
				nudge := history.GenerateNudge("repetition", coverageSummary)
				chain = append(chain, llms.TextParts(llms.ChatMessageTypeHuman, nudge))
				logger.Info("harness: tool repetition detected, nudging")
			}

			if coveragePctVal >= cfg.Harness.CoverageCompleteAt {
				nudge := BuildCoverageNudge(coveragePctVal, findingCount(cfg.CoverageState))
				chain = append(chain, llms.TextParts(llms.ChatMessageTypeHuman, nudge))
				logger.WithField("coverage", coveragePctVal).Info("harness: high coverage, suggesting wrap-up")
			}

			if iteration%15 == 0 {
				nudge := history.GenerateNudge("progress", coverageSummary)
				chain = append(chain, llms.TextParts(llms.ChatMessageTypeHuman, nudge))
			}
		}
	}

	return &LoopResult{
		FinalContent: lastContent,
		Iterations:   history.iterationCount,
		ToolCalls:    history.ToolCallCount(),
	}, nil
}

func persistChainUpdate(ctx context.Context, cfg LoopConfig, chain []llms.MessageContent, duration float64) error {
	chainBlob, err := json.Marshal(chain)
	if err != nil {
		return err
	}
	_, err = cfg.DB.UpdateMsgChain(ctx, database.UpdateMsgChainParams{
		Chain:           chainBlob,
		DurationSeconds: duration,
		Model:           cfg.Model,
		ModelProvider:    cfg.Provider,
		ID:              cfg.ChainID,
	})
	return err
}

func coveragePct(cs *tools.CoverageState) float64 {
	if cs == nil {
		return 0
	}
	return cs.CoveragePercent()
}

func findingCount(cs *tools.CoverageState) int {
	if cs == nil {
		return 0
	}
	return cs.FindingCount()
}

func availableToolNames(tools []BaseTool) string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Info().Name)
	}
	result := ""
	for i, n := range names {
		if i > 0 {
			result += ", "
		}
		result += n
	}
	return result
}

// pruneStaleToolResults keeps only the last N tool call/response pairs,
// replacing older ones with a short stub. Preserves system and first human messages.
func pruneStaleToolResults(chain []llms.MessageContent, keepRecent int) []llms.MessageContent {
	if len(chain) <= keepRecent*2+2 {
		return chain
	}

	// Find the boundary: keep system + first human + last keepRecent*2 messages
	boundary := len(chain) - keepRecent*2
	if boundary < 2 {
		return chain
	}

	pruned := make([]llms.MessageContent, 0, len(chain))
	pruned = append(pruned, chain[:2]...) // system + first human

	for i := 2; i < boundary; i++ {
		msg := chain[i]
		if msg.Role == llms.ChatMessageTypeTool {
			// Stub out old tool results
			for j, part := range msg.Parts {
				if tcr, ok := part.(llms.ToolCallResponse); ok {
					if len(tcr.Content) > 200 {
						tcr.Content = tcr.Content[:200] + "\n[output pruned for context management]"
						msg.Parts[j] = tcr
					}
				}
			}
		}
		pruned = append(pruned, msg)
	}

	pruned = append(pruned, chain[boundary:]...)
	return pruned
}
