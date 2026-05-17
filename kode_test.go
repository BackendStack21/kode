package kode

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	os.Unsetenv("DEEPSEEK_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")

	cfg := Config{
		APIKey: "sk-test",
	}

	if cfg.MaxIterations != 0 {
		t.Error("MaxIterations should default to 0")
	}

	_, err := New(cfg)
	if err != nil {
		t.Fatalf("New() with explicit APIKey should not error: %v", err)
	}
}

func TestConfigDefaultModel(t *testing.T) {
	cfg := Config{APIKey: "sk-test"}
	agent, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if agent.config.Model != "deepseek-chat" {
		t.Errorf("default model = %q, want %q", agent.config.Model, "deepseek-chat")
	}
}

func TestConfigDefaultBaseURL(t *testing.T) {
	cfg := Config{APIKey: "sk-test"}
	agent, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if agent.config.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("default BaseURL = %q, want %q", agent.config.BaseURL, "https://api.deepseek.com/v1")
	}
}

func TestConfigDefaultMaxIterations(t *testing.T) {
	cfg := Config{APIKey: "sk-test"}
	agent, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if agent.config.MaxIterations != 90 {
		t.Errorf("default MaxIterations = %d, want 90", agent.config.MaxIterations)
	}
}

func TestConfigCustomModel(t *testing.T) {
	cfg := Config{
		APIKey: "sk-test",
		Model:  "deepseek-v4-flash",
	}
	agent, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if agent.config.Model != "deepseek-v4-flash" {
		t.Errorf("model = %q, want %q", agent.config.Model, "deepseek-v4-flash")
	}
}

func TestConfigCustomBaseURL(t *testing.T) {
	cfg := Config{
		APIKey:  "sk-test",
		BaseURL: "https://api.openai.com/v1",
	}
	agent, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if agent.config.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("BaseURL = %q, want %q", agent.config.BaseURL, "https://api.openai.com/v1")
	}
}

func TestConfigCustomMaxIterations(t *testing.T) {
	cfg := Config{
		APIKey:        "sk-test",
		MaxIterations: 42,
	}
	agent, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if agent.config.MaxIterations != 42 {
		t.Errorf("MaxIterations = %d, want 42", agent.config.MaxIterations)
	}
}

func TestConfigThinkingPassthrough(t *testing.T) {
	tests := []struct {
		thinking string
	}{
		{"enabled"},
		{"disabled"},
		{"low"},
		{"medium"},
		{"high"},
		{""},
	}

	for _, tt := range tests {
		cfg := Config{
			APIKey:   "sk-test",
			Thinking: tt.thinking,
		}
		agent, err := New(cfg)
		if err != nil {
			t.Fatalf("New() with thinking=%q: %v", tt.thinking, err)
		}
		if agent.config.Thinking != tt.thinking {
			t.Errorf("Thinking = %q, want %q", agent.config.Thinking, tt.thinking)
		}
	}
}

func TestConfigAPIKeyEnvFallback(t *testing.T) {
	t.Run("DEEPSEEK_API_KEY", func(t *testing.T) {
		os.Unsetenv("OPENAI_API_KEY")
		os.Setenv("DEEPSEEK_API_KEY", "sk-deepseek-test")
		defer os.Unsetenv("DEEPSEEK_API_KEY")

		cfg := Config{}
		agent, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if agent.config.APIKey != "sk-deepseek-test" {
			t.Errorf("APIKey = %q, want %q", agent.config.APIKey, "sk-deepseek-test")
		}
	})

	t.Run("OPENAI_API_KEY fallback", func(t *testing.T) {
		os.Unsetenv("DEEPSEEK_API_KEY")
		os.Setenv("OPENAI_API_KEY", "sk-openai-test")
		defer os.Unsetenv("OPENAI_API_KEY")

		cfg := Config{}
		agent, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if agent.config.APIKey != "sk-openai-test" {
			t.Errorf("APIKey = %q, want %q", agent.config.APIKey, "sk-openai-test")
		}
	})

	t.Run("explicit overrides env", func(t *testing.T) {
		os.Setenv("DEEPSEEK_API_KEY", "sk-env")
		defer os.Unsetenv("DEEPSEEK_API_KEY")

		cfg := Config{APIKey: "sk-explicit"}
		agent, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if agent.config.APIKey != "sk-explicit" {
			t.Errorf("APIKey = %q, want %q", agent.config.APIKey, "sk-explicit")
		}
	})
}

func TestConfigNoAPIKey(t *testing.T) {
	os.Unsetenv("DEEPSEEK_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")

	cfg := Config{}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestConfigSystemMessage(t *testing.T) {
	cfg := Config{
		APIKey:        "sk-test",
		SystemMessage: "You are a helpful assistant.",
	}
	agent, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if agent.config.SystemMessage != "You are a helpful assistant." {
		t.Errorf("SystemMessage = %q, want %q", agent.config.SystemMessage, "You are a helpful assistant.")
	}
}

func TestAgent_Run(t *testing.T) {
	// Agent.Run delegates to engine.Run. Test that it doesn't panic.
	agent, err := New(Config{
		APIKey: "sk-test",
		Model:  "deepseek-chat",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Run with a cancelled context — should return error quickly
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = agent.Run(ctx, "test task")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestAgent_Close_NoSandbox(t *testing.T) {
	agent, err := New(Config{APIKey: "sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	// Close with no sandbox cleanup should return nil
	if err := agent.Close(); err != nil {
		t.Errorf("Close() with no sandbox should return nil, got: %v", err)
	}
}

func TestAgent_Close_WithSandbox(t *testing.T) {
	cleanupCalled := false
	cleanup := func() error {
		cleanupCalled = true
		return nil
	}

	agent, err := New(Config{
		APIKey:         "sk-test",
		SandboxCleanup: cleanup,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
	if !cleanupCalled {
		t.Error("sandbox cleanup was not called")
	}
}

func TestAgent_Close_WithSandboxError(t *testing.T) {
	cleanup := func() error {
		return fmt.Errorf("cleanup failed")
	}

	agent, err := New(Config{
		APIKey:         "sk-test",
		SandboxCleanup: cleanup,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = agent.Close()
	if err == nil {
		t.Fatal("expected error from cleanup")
	}
}

func TestToolAdapter(t *testing.T) {
	// Create a fake tool
	fake := &fakeKodeTool{
		name:        "test",
		description: "a test tool",
		schema:      map[string]any{"type": "object"},
		callResult:  "result",
	}

	adapter := &toolAdapter{t: fake}

	if adapter.Name() != "test" {
		t.Errorf("Name() = %q, want %q", adapter.Name(), "test")
	}
	if adapter.Description() != "a test tool" {
		t.Errorf("Description() = %q, want %q", adapter.Description(), "a test tool")
	}
	if adapter.Schema() == nil {
		t.Error("Schema() returned nil")
	}

	result, err := adapter.Call(`{"arg": "value"}`)
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if result != "result" {
		t.Errorf("Call() = %q, want %q", result, "result")
	}
}

// fakeKodeTool implements kode.Tool for testing.
type fakeKodeTool struct {
	name        string
	description string
	schema      any
	callResult  string
	callError   error
}

func (f *fakeKodeTool) Name() string                     { return f.name }
func (f *fakeKodeTool) Description() string              { return f.description }
func (f *fakeKodeTool) Schema() any                      { return f.schema }
func (f *fakeKodeTool) Call(args string) (string, error) { return f.callResult, f.callError }

// Test that New() works with tools, covering the tool adapter loop (lines 109-112 in kode.go).
func TestNew_WithTools(t *testing.T) {
	fake := &fakeKodeTool{
		name:        "test_tool",
		description: "a test tool",
		schema:      map[string]any{"type": "object"},
		callResult:  "ok",
	}
	cfg := Config{
		APIKey: "sk-test",
		Tools:  []Tool{fake},
	}
	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() with tools error: %v", err)
	}
	// Verify the tool was registered in the internal registry
	tools := agent.registry.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool in registry, got %d", len(tools))
	}
	if tools[0].Name() != "test_tool" {
		t.Errorf("tool name = %q, want %q", tools[0].Name(), "test_tool")
	}
}
