package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BackendStack21/kode/internal/llm"
	"github.com/BackendStack21/kode/internal/tool"
)

// fakeTool is a simple tool for testing.
type fakeTool struct {
	name        string
	description string
	output      string
}

func (f *fakeTool) Name() string              { return f.name }
func (f *fakeTool) Description() string       { return f.description }
func (f *fakeTool) Schema() any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}
func (f *fakeTool) Call(args string) (string, error) { return f.output, nil }

func TestEngine_Run_SimpleAnswer(t *testing.T) {
	// Fake server that returns a final answer immediately (no tool calls).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Hello from kode!"}}]}`)
	}))
	defer server.Close()

	client := llm.New(server.URL, "sk-test", "test-model", "")
	registry := tool.NewRegistry(nil)
	engine := New(client, registry, 10, "")

	result, err := engine.Run(context.Background(), "Say hello")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result != "Hello from kode!" {
		t.Errorf("result = %q, want %q", result, "Hello from kode!")
	}
}

func TestEngine_Run_ToolCallLoop(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: model requests a tool
			fmt.Fprint(w, `{
				"choices":[{
					"message":{
						"content":"Let me check.",
						"tool_calls":[{
							"id":"call_1",
							"function":{
								"name":"echo",
								"arguments":"{\"text\":\"hello\"}"
							}
						}]
					}
				}]
			}`)
		} else {
			// Second call: final answer
			fmt.Fprint(w, `{"choices":[{"message":{"content":"The tool said: hello output"}}]}`)
		}
	}))
	defer server.Close()

	echoTool := &fakeTool{name: "echo", description: "echoes input", output: "hello output"}
	registry := tool.NewRegistry([]tool.Tool{echoTool})
	client := llm.New(server.URL, "sk-test", "test-model", "")
	engine := New(client, registry, 10, "")

	result, err := engine.Run(context.Background(), "Echo hello")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result != "The tool said: hello output" {
		t.Errorf("result = %q, want %q", result, "The tool said: hello output")
	}
	if callCount != 2 {
		t.Errorf("expected 2 LLM calls, got %d", callCount)
	}
}

func TestEngine_Run_MaxIterations(t *testing.T) {
	// Server that always requests a tool call, never gives a final answer.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"choices":[{
				"message":{
					"content":"",
					"tool_calls":[{
						"id":"call_1",
						"function":{
							"name":"echo",
							"arguments":"{}"
						}
					}]
				}
			}]
		}`)
	}))
	defer server.Close()

	echoTool := &fakeTool{name: "echo", description: "echo", output: "ok"}
	registry := tool.NewRegistry([]tool.Tool{echoTool})
	client := llm.New(server.URL, "sk-test", "test-model", "")
	engine := New(client, registry, 3, "")

	_, err := engine.Run(context.Background(), "Loop forever")
	if err == nil {
		t.Fatal("expected max iterations error")
	}
}

func TestEngine_Run_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"answer"}}]}`)
	}))
	defer server.Close()

	client := llm.New(server.URL, "sk-test", "test-model", "")
	engine := New(client, tool.NewRegistry(nil), 10, "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := engine.Run(ctx, "task")
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestEngine_Run_SystemMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the system message is injected as the first message.
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			if len(body.Messages) > 0 && body.Messages[0].Role == "system" {
				if body.Messages[0].Content != "You are a test bot." {
					t.Errorf("system message = %q, want %q", body.Messages[0].Content, "You are a test bot.")
				}
			} else {
				t.Error("system message not found or wrong role")
			}
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	client := llm.New(server.URL, "sk-test", "test-model", "")
	engine := New(client, tool.NewRegistry(nil), 10, "You are a test bot.")

	result, err := engine.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
}

func TestEngine_Run_ToolNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"choices":[{
				"message":{
					"content":"",
					"tool_calls":[{
						"id":"call_x",
						"function":{
							"name":"nonexistent",
							"arguments":"{}"
						}
					}]
				}
			}]
		}`)
	}))
	defer server.Close()

	// No tools registered — the tool call will fail
	client := llm.New(server.URL, "sk-test", "test-model", "")
	engine := New(client, tool.NewRegistry(nil), 10, "")

	// The loop should handle the missing tool gracefully — the tool error
	// is fed back to the model as a tool response message. The test server
	// only returns one response, so we'll hit max iterations.
	_, err := engine.Run(context.Background(), "use missing tool")
	if err == nil {
		t.Fatal("expected error (max iterations or similar)")
	}
}

func TestEngine_BuildToolDefs(t *testing.T) {
	t1 := &fakeTool{name: "read", description: "read files"}
	t2 := &fakeTool{name: "write", description: "write files"}
	registry := tool.NewRegistry([]tool.Tool{t1, t2})

	engine := New(nil, registry, 10, "")
	defs := engine.buildToolDefs()

	if len(defs) != 2 {
		t.Fatalf("expected 2 tool defs, got %d", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		if d.Type != "function" {
			t.Errorf("ToolDef.Type = %q, want %q", d.Type, "function")
		}
		names[d.Function.Name] = true
	}

	if !names["read"] || !names["write"] {
		t.Errorf("missing expected tool names: got %v", names)
	}
}
