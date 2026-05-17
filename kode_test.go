package kode

import (
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
