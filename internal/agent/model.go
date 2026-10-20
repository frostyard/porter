// Package agent is Porter's agent loop: it builds each request to the
// model from the system prompt, the owner's memory, the rolling summary
// and the recent turns, offers the tools the registry allows, runs the
// tools the model calls, and writes the reply as a turn with cards.
package agent

import (
	"context"
	"encoding/json/jsontext"

	"github.com/frostyard/lodge/kit/inference"
)

// Message is one chat message, in the OpenAI shape lodge's inference
// socket passes through.
type Message struct {
	Role string `json:"role"`
	// Content is nil for an assistant message that only calls tools.
	Content    *string    `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitzero"`
	ToolCallID string     `json:"tool_call_id,omitzero"`
}

// ToolCall is the model's call of one tool.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ToolDef is a tool offered to the model.
type ToolDef struct {
	Type     string  `json:"type"`
	Function FuncDef `json:"function"`
}

// FuncDef is a tool's name, description and parameters.
type FuncDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  jsontext.Value `json:"parameters"`
}

// Request is a chat completion request.
type Request struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []ToolDef `json:"tools,omitzero"`
	// TemplateArgs reach the local model's chat template; enable_thinking
	// turns its thinking on or off.
	TemplateArgs map[string]any `json:"chat_template_kwargs,omitzero"`
}

// Response is a chat completion.
type Response struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
}

// Model answers chat completions.
type Model interface {
	Chat(ctx context.Context, p inference.Priority, req *Request) (*Response, error)
}

// Lodge is the Model behind lodge's inference socket.
type Lodge struct{ C *inference.Client }

// Chat implements Model.
func (l Lodge) Chat(ctx context.Context, p inference.Priority, req *Request) (*Response, error) {
	var res Response
	if err := l.C.Chat(ctx, p, req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func text(s string) *string { return &s }
