package harness

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vxcontrol/langchaingo/llms"
)

type ToolInfo struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

type ToolCall struct {
	ID   string
	Name string
	Args string
}

type ToolResponse struct {
	Content string
	IsError bool
}

type BaseTool interface {
	Info() ToolInfo
	Run(ctx context.Context, call ToolCall) (ToolResponse, error)
}

func toolsToLLMTools(tools []BaseTool) []llms.Tool {
	out := make([]llms.Tool, 0, len(tools))
	for _, t := range tools {
		info := t.Info()
		var params map[string]any
		if len(info.Parameters) > 0 {
			json.Unmarshal(info.Parameters, &params)
		}
		out = append(out, llms.Tool{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        info.Name,
				Description: info.Description,
				Parameters:  params,
			},
		})
	}
	return out
}

func findTool(name string, tools []BaseTool) (BaseTool, error) {
	for _, t := range tools {
		if t.Info().Name == name {
			return t, nil
		}
	}
	return nil, fmt.Errorf("unknown tool: %s", name)
}

type handlerTool struct {
	info    ToolInfo
	handler func(ctx context.Context, name string, args json.RawMessage) (string, error)
}

func NewHandlerTool(name, description string, params json.RawMessage, handler func(ctx context.Context, name string, args json.RawMessage) (string, error)) BaseTool {
	return &handlerTool{
		info: ToolInfo{
			Name:        name,
			Description: description,
			Parameters:  params,
		},
		handler: handler,
	}
}

func (t *handlerTool) Info() ToolInfo { return t.info }

func (t *handlerTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	result, err := t.handler(ctx, call.Name, json.RawMessage(call.Args))
	if err != nil {
		return ToolResponse{Content: err.Error(), IsError: true}, nil
	}
	return ToolResponse{Content: result}, nil
}
