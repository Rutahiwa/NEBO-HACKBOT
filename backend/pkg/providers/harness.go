package providers

import (
	"context"
	"fmt"
	"time"

	"pentagi/pkg/csum"
	obs "pentagi/pkg/observability"
	"pentagi/pkg/providers/pconfig"
	"pentagi/pkg/tools"

	"github.com/sirupsen/logrus"
	"github.com/vxcontrol/langchaingo/llms"
)

type HarnessConfig struct {
	MaxIterations          int
	MinToolCalls           int
	StallThreshold         int
	RepetitionThreshold    int
	MaxConsecutiveTextOnly int
	CoverageCompleteAt     float64
}

func DefaultHarnessConfig() HarnessConfig {
	return HarnessConfig{
		MaxIterations:          500,
		MinToolCalls:           5,
		StallThreshold:         5,
		RepetitionThreshold:    5,
		MaxConsecutiveTextOnly: 1,
		CoverageCompleteAt:     85.0,
	}
}

func (fp *flowProvider) performHarnessLoop(
	ctx context.Context,
	chainID int64,
	taskID, subtaskID *int64,
	chain []llms.MessageContent,
	executor tools.ContextToolsExecutor,
	summarizer csum.Summarizer,
	coverageState *tools.CoverageState,
	harnessConfig HarnessConfig,
) error {
	ctx, span := obs.Observer.NewSpan(ctx, obs.SpanKindInternal, "providers.flowProvider.performHarnessLoop")
	defer span.End()

	var (
		optAgentType       = pconfig.OptionsTypePentester
		detector           = &repeatingDetector{}
		groupID            = fp.cfg.GroupID(fp.flowID)
		toolTypeMapping    = tools.GetToolTypeMapping()
		summarizerHandler  = fp.GetSummarizeResultHandler(taskID, subtaskID)
		monitor            = &executionMonitor{enabled: false}
		history            = NewHarnessHistory()
		consecutiveTextOnly int
	)

	logger := logrus.WithContext(ctx).WithFields(enrichLogrusFields(fp.flowID, taskID, subtaskID, logrus.Fields{
		"provider":     fp.Type(),
		"agent":        optAgentType,
		"msg_chain_id": chainID,
		"harness_loop": true,
	}))

	lastUpdateTime := time.Now()
	rollLastUpdateTime := func() float64 {
		durationDelta := time.Since(lastUpdateTime).Seconds()
		lastUpdateTime = time.Now()
		return durationDelta
	}

	executionContext, err := fp.getExecutionContext(ctx, taskID, subtaskID)
	if err != nil {
		return fmt.Errorf("failed to get execution context: %w", err)
	}

	for iteration := 0; iteration < harnessConfig.MaxIterations; iteration++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		history.RecordIteration()

		chain = pruneStaleToolResults(chain, defaultKeepRecentToolResults)

		callChain := chain
		if fp.cfg.UseContextWindow {
			callChain = applyContextWindow(chain, fp.cfg.ContextWindowSize)
		}

		result, err := fp.callWithRetries(ctx, optAgentType, chainID, taskID, subtaskID, callChain, executor, executionContext)
		if err != nil {
			obs.LogErrorOrCancel(logger, err, "failed to call LLM in harness loop")
			return err
		}

		if err := fp.updateMsgChainUsage(ctx, chainID, optAgentType, result.info, rollLastUpdateTime()); err != nil {
			obs.LogErrorOrCancel(logger, err, "failed to update msg chain usage")
			return err
		}

		if len(result.funcCalls) == 0 {
			consecutiveTextOnly++
			fp.storeAgentResponseToGraphiti(ctx, groupID, optAgentType, result, taskID, subtaskID, chainID)

			msg := llms.MessageContent{Role: llms.ChatMessageTypeAI}
			if result.content != "" || !result.thinking.IsEmpty() {
				msg.Parts = append(msg.Parts, llms.TextPartWithReasoning(result.content, result.thinking))
			}
			chain = append(chain, msg)

			if consecutiveTextOnly >= harnessConfig.MaxConsecutiveTextOnly {
				logger.WithFields(logrus.Fields{
					"iteration":   iteration,
					"tool_calls":  history.ToolCallCount(),
					"coverage":    coverageState.CoveragePercent(),
				}).Info("harness loop: model done (consecutive text-only)")
				fp.updateMsgChain(ctx, optAgentType, chainID, chain, rollLastUpdateTime())
				return nil
			}

			nudge := history.GenerateNudgeWithCoverage("text_only", coverageState.GetCoverageSummary())
			chain = append(chain, llms.TextParts(llms.ChatMessageTypeHuman, nudge))
			if err := fp.updateMsgChain(ctx, optAgentType, chainID, chain, rollLastUpdateTime()); err != nil {
				return err
			}
			continue
		}

		consecutiveTextOnly = 0

		fp.storeAgentResponseToGraphiti(ctx, groupID, optAgentType, result, taskID, subtaskID, chainID)

		msg := llms.MessageContent{Role: llms.ChatMessageTypeAI}
		if result.content != "" || !result.thinking.IsEmpty() {
			msg.Parts = append(msg.Parts, llms.TextPartWithReasoning(result.content, result.thinking))
		}
		for _, toolCall := range result.funcCalls {
			msg.Parts = append(msg.Parts, toolCall)
		}
		chain = append(chain, msg)

		if err := fp.updateMsgChain(ctx, optAgentType, chainID, chain, rollLastUpdateTime()); err != nil {
			obs.LogErrorOrCancel(logger, err, "failed to update msg chain")
			return err
		}

		for idx, toolCall := range result.funcCalls {
			if toolCall.FunctionCall == nil {
				continue
			}

			funcName := toolCall.FunctionCall.Name
			history.RecordToolCall(funcName)

			response, err := fp.execToolCall(
				ctx, optAgentType, chainID, idx, result, monitor, detector, executor, taskID, subtaskID, chain,
			)

			if toolTypeMapping[funcName] != tools.AgentToolType {
				fp.storeToolExecutionToGraphiti(
					ctx, groupID, optAgentType, toolCall, response, err, executor, taskID, subtaskID, chainID,
				)
			}

			if err != nil {
				obs.LogErrorOrCancel(logger.WithFields(logrus.Fields{
					"func_name": funcName,
					"func_args": toolCall.FunctionCall.Arguments,
				}), err, "failed to exec tool call")
				return err
			}

			chain = append(chain, llms.MessageContent{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: toolCall.ID,
						Name:       funcName,
						Content:    response,
					},
				},
			})
			if err := fp.updateMsgChain(ctx, optAgentType, chainID, chain, rollLastUpdateTime()); err != nil {
				obs.LogErrorOrCancel(logger, err, "failed to update msg chain")
				return err
			}
		}

		if summarizer != nil {
			chain, err = summarizer.SummarizeChain(ctx, summarizerHandler, chain, fp.tcIDTemplate)
			if err != nil {
				logger.WithError(err).Warn("failed to summarize chain, continuing")
			} else if err := fp.updateMsgChain(ctx, optAgentType, chainID, chain, rollLastUpdateTime()); err != nil {
				obs.LogErrorOrCancel(logger, err, "failed to update msg chain after summarization")
				return err
			}
		}

		// Harness-driven checks (every 3 iterations)
		if iteration > 0 && iteration%3 == 0 {
			coveragePct := coverageState.CoveragePercent()
			coverageSummary := coverageState.GetCoverageSummary()

			if history.DetectStall(harnessConfig.StallThreshold) && history.ToolCallCount() >= harnessConfig.MinToolCalls {
				nudge := history.GenerateNudgeWithCoverage("stall", coverageSummary)
				chain = append(chain, llms.TextParts(llms.ChatMessageTypeHuman, nudge))
				logger.WithField("coverage", coveragePct).Info("harness loop: stall detected, nudging")
			}

			if history.DetectToolRepetition(harnessConfig.RepetitionThreshold) {
				nudge := history.GenerateNudgeWithCoverage("repetition", coverageSummary)
				chain = append(chain, llms.TextParts(llms.ChatMessageTypeHuman, nudge))
				logger.Info("harness loop: tool repetition detected, nudging")
			}

			if coveragePct >= harnessConfig.CoverageCompleteAt {
				nudge := buildCoverageNudge(coveragePct, coverageState.FindingCount())
				chain = append(chain, llms.TextParts(llms.ChatMessageTypeHuman, nudge))
				logger.WithField("coverage", coveragePct).Info("harness loop: high coverage, suggesting wrap-up")
			}

			if iteration%15 == 0 {
				progressNudge := history.GenerateNudgeWithCoverage("progress", coverageSummary)
				chain = append(chain, llms.TextParts(llms.ChatMessageTypeHuman, progressNudge))
			}
		}
	}

	logger.WithFields(logrus.Fields{
		"max_iterations": harnessConfig.MaxIterations,
		"tool_calls":     history.ToolCallCount(),
		"coverage":       coverageState.CoveragePercent(),
	}).Warn("harness loop exceeded maximum iterations")

	return nil
}
