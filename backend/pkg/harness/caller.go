package harness

import (
	"context"
	"fmt"

	"pentagi/pkg/providers/pconfig"

	"github.com/vxcontrol/langchaingo/llms"
	"github.com/vxcontrol/langchaingo/llms/streaming"
)

type LLMCaller interface {
	CallWithTools(
		ctx context.Context,
		opt pconfig.ProviderOptionsType,
		chain []llms.MessageContent,
		tools []llms.Tool,
		streamCb streaming.Callback,
	) (*llms.ContentResponse, error)
}

type CallResult struct {
	Content   string
	ToolCalls []llms.ToolCall
	Usage     map[string]any
}

func callLLM(ctx context.Context, caller LLMCaller, chain []llms.MessageContent, tools []BaseTool) (*CallResult, error) {
	llmTools := toolsToLLMTools(tools)

	resp, err := caller.CallWithTools(ctx, pconfig.OptionsTypePentester, chain, llmTools, nil)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	if resp == nil || len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from LLM")
	}

	result := &CallResult{}

	for _, choice := range resp.Choices {
		if choice.Content != "" {
			result.Content += choice.Content
		}
		if choice.GenerationInfo != nil {
			result.Usage = choice.GenerationInfo
		}
		for _, tc := range choice.ToolCalls {
			result.ToolCalls = append(result.ToolCalls, tc)
		}
	}

	return result, nil
}
