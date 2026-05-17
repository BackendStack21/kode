package llm

import (
	"encoding/json"
	"testing"
)

func TestCallParamsMarshaling_NoThinking(t *testing.T) {
	body := CallParams{
		Model: "deepseek-chat",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		Stream: false,
	}

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	// Thinking field should be absent (omitempty)
	if _, ok := result["thinking"]; ok {
		t.Error("thinking field should be absent when not set")
	}
	if _, ok := result["reasoning_effort"]; ok {
		t.Error("reasoning_effort field should be absent when not set")
	}
}

func TestCallParamsMarshaling_ThinkingEnabled(t *testing.T) {
	body := CallParams{
		Model:    "deepseek-chat",
		Messages: []Message{{Role: "user", Content: "hello"}},
		Stream:   false,
		Thinking: &ThinkingConfig{Type: "enabled"},
	}

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	thinking, ok := result["thinking"]
	if !ok {
		t.Fatal("thinking field should be present when set")
	}
	thinkingMap, ok := thinking.(map[string]any)
	if !ok {
		t.Fatal("thinking field should be an object")
	}
	if thinkingMap["type"] != "enabled" {
		t.Errorf("thinking.type = %q, want %q", thinkingMap["type"], "enabled")
	}
}

func TestCallParamsMarshaling_ThinkingDisabled(t *testing.T) {
	body := CallParams{
		Model:    "deepseek-chat",
		Messages: []Message{{Role: "user", Content: "hello"}},
		Stream:   false,
		Thinking: &ThinkingConfig{Type: "disabled"},
	}

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	thinking, ok := result["thinking"]
	if !ok {
		t.Fatal("thinking field should be present when set")
	}
	thinkingMap := thinking.(map[string]any)
	if thinkingMap["type"] != "disabled" {
		t.Errorf("thinking.type = %q, want %q", thinkingMap["type"], "disabled")
	}
}

func TestCallParamsMarshaling_ReasoningEffort(t *testing.T) {
	tests := []string{"low", "medium", "high"}

	for _, level := range tests {
		body := CallParams{
			Model:          "o1",
			Messages:       []Message{{Role: "user", Content: "hello"}},
			Stream:         false,
			ReasoningEffort: level,
		}

		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}

		var result map[string]any
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}

		effort, ok := result["reasoning_effort"]
		if !ok {
			t.Errorf("reasoning_effort should be present for %q", level)
			continue
		}
		if effort != level {
			t.Errorf("reasoning_effort = %q, want %q", effort, level)
		}
	}
}

func TestParseResponse_ContentOnly(t *testing.T) {
	raw := `{
		"choices": [{
			"message": {
				"content": "Hello, world!"
			}
		}]
	}`

	result, err := parseResponse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "Hello, world!" {
		t.Errorf("Content = %q, want %q", result.Content, "Hello, world!")
	}
	if len(result.ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(result.ToolCalls))
	}
}

func TestParseResponse_ToolCalls(t *testing.T) {
	raw := `{
		"choices": [{
			"message": {
				"content": null,
				"tool_calls": [{
					"id": "call_123",
					"function": {
						"name": "shell",
						"arguments": "{\"command\":\"ls\"}"
					}
				}]
			}
		}]
	}`

	result, err := parseResponse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "" {
		t.Errorf("Content should be empty, got %q", result.Content)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ID != "call_123" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call_123")
	}
	if tc.Function.Name != "shell" {
		t.Errorf("ToolCall.Function.Name = %q, want %q", tc.Function.Name, "shell")
	}
	if tc.Function.Arguments != `{"command":"ls"}` {
		t.Errorf("ToolCall.Function.Arguments = %q, want %q", tc.Function.Arguments, `{"command":"ls"}`)
	}
}

func TestParseResponse_ContentAndToolCalls(t *testing.T) {
	raw := `{
		"choices": [{
			"message": {
				"content": "Let me check that file.",
				"tool_calls": [{
					"id": "call_456",
					"function": {
						"name": "shell",
						"arguments": "{\"command\":\"cat file.txt\"}"
					}
				}]
			}
		}]
	}`

	result, err := parseResponse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "Let me check that file." {
		t.Errorf("Content = %q, want %q", result.Content, "Let me check that file.")
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Function.Name != "shell" {
		t.Errorf("ToolCall name = %q, want %q", result.ToolCalls[0].Function.Name, "shell")
	}
}

func TestParseResponse_EmptyChoices(t *testing.T) {
	raw := `{"choices": []}`

	_, err := parseResponse([]byte(raw))
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestParseResponse_InvalidJSON(t *testing.T) {
	_, err := parseResponse([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCallParamsMarshaling_WithTools(t *testing.T) {
	body := CallParams{
		Model: "deepseek-chat",
		Messages: []Message{
			{Role: "user", Content: "list files"},
		},
		Tools: []ToolDef{
			{
				Type: "function",
				Function: FunctionDef{
					Name:        "shell",
					Description: "Run a command",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"command": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
		Stream: false,
	}

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	tools, ok := result["tools"]
	if !ok {
		t.Fatal("tools field should be present")
	}
	toolsArr, ok := tools.([]any)
	if !ok || len(toolsArr) != 1 {
		t.Fatalf("expected 1 tool, got %v", tools)
	}
}

func TestClient_ThinkingSwitch(t *testing.T) {
	tests := []struct {
		name         string
		thinking     string
		expectThink  bool
		expectReason bool
	}{
		{"enabled", "enabled", true, false},
		{"disabled", "disabled", true, false},
		{"low", "low", false, true},
		{"medium", "medium", false, true},
		{"high", "high", false, true},
		{"empty", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate what Call() does — construct the same body
			body := CallParams{
				Model:    "test-model",
				Messages: []Message{{Role: "user", Content: "hi"}},
				Stream:   false,
			}

			switch tt.thinking {
			case "enabled", "disabled":
				body.Thinking = &ThinkingConfig{Type: tt.thinking}
			case "low", "medium", "high":
				body.ReasoningEffort = tt.thinking
			}

			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}

			var result map[string]any
			json.Unmarshal(data, &result)

			_, hasThinking := result["thinking"]
			_, hasReasoning := result["reasoning_effort"]

			if hasThinking != tt.expectThink {
				t.Errorf("thinking field present = %v, want %v", hasThinking, tt.expectThink)
			}
			if hasReasoning != tt.expectReason {
				t.Errorf("reasoning_effort present = %v, want %v", hasReasoning, tt.expectReason)
			}
		})
	}
}
