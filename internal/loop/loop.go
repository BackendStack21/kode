// Package loop implements the ReAct (Reasoning + Acting) agent loop.
package loop

import (
	"context"
	"fmt"
	"strings"

	"github.com/BackendStack21/kode/internal/llm"
	"github.com/BackendStack21/kode/internal/render"
	"github.com/BackendStack21/kode/internal/tool"
)

// Engine runs the agent loop: observe → think → act → repeat.
type Engine struct {
	client   *llm.Client
	registry *tool.Registry
	renderer *render.Renderer // optional: colored terminal output
	maxIter  int
	system   string
}

// New creates a new loop Engine.
func New(client *llm.Client, registry *tool.Registry, maxIterations int, systemMessage string, renderer *render.Renderer) *Engine {
	return &Engine{
		client:   client,
		registry: registry,
		renderer: renderer,
		maxIter:  maxIterations,
		system:   systemMessage,
	}
}

// Run executes the loop for a given task and returns the final response.
func (e *Engine) Run(ctx context.Context, task string) (string, error) {
	messages := []llm.Message{
		{Role: "user", Content: task},
	}
	if e.system != "" {
		messages = append([]llm.Message{{Role: "system", Content: e.system}}, messages...)
	}

	tools := e.buildToolDefs()

	for i := 0; i < e.maxIter; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		// Render iteration header (1-indexed for humans)
		if e.renderer != nil {
			e.renderer.Iteration(i+1, e.maxIter)
		}

		// THINK
		result, err := e.client.Call(ctx, messages, tools)
		if err != nil {
			return "", fmt.Errorf("iteration %d: %w", i, err)
		}

		// No tool calls = final answer
		if len(result.ToolCalls) == 0 {
			if e.renderer != nil {
				e.renderer.FinalAnswer(result.Content)
			}
			return result.Content, nil
		}

		// Render the model's thinking (reasoning before tool calls)
		if e.renderer != nil && result.Content != "" {
			e.renderer.Thinking(result.Content)
		}

		// Build assistant message with tool calls
		assistantMsg := llm.Message{
			Role:      "assistant",
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		// ACT: execute each tool call
		for _, tc := range result.ToolCalls {
			if e.renderer != nil {
				e.renderer.ToolCall(tc.Function.Name, tc.Function.Arguments)
			}

			t := e.registry.Get(tc.Function.Name)
			output := fmt.Sprintf("error: tool %q not found", tc.Function.Name)
			if t != nil {
				res, err := t.Call(tc.Function.Arguments)
				if err != nil {
					output = fmt.Sprintf("error: %s", err.Error())
				} else {
					output = res
				}
			}

			if e.renderer != nil {
				e.renderer.ToolResult(output)
			}

			messages = append(messages, llm.Message{
				Role:       "tool",
				Content:    output,
				Name:       tc.Function.Name,
				ToolCallID: tc.ID,
			})
		}
	}

	return "", fmt.Errorf("reached max iterations (%d) without final answer", e.maxIter)
}

// buildToolDefs converts the registry's tools to LLM-compatible definitions.
func (e *Engine) buildToolDefs() []llm.ToolDef {
	all := e.registry.Tools()
	defs := make([]llm.ToolDef, 0, len(all))
	for _, t := range all {
		schema := t.Schema()
		var params any
		if s, ok := schema.(string); ok {
			if strings.TrimSpace(s) != "" {
				params = map[string]any{"type": "object", "raw_schema": s}
			} else {
				params = map[string]any{"type": "object", "properties": map[string]any{}}
			}
		} else {
			params = schema
		}

		defs = append(defs, llm.ToolDef{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  params,
			},
		})
	}
	return defs
}
