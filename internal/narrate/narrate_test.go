package narrate

import (
	"strings"
	"testing"
)

func TestNarrator_ToolCallMessage_Offline(t *testing.T) {
	n := New(true) // enabled, template mode
	msg := n.ToolCallMessage("read_file", `{"path": "main.go"}`)
	if msg == "" {
		t.Error("expected non-empty fallback message")
	}
	if !strings.Contains(msg, "main.go") {
		t.Errorf("expected message to contain filename, got: %q", msg)
	}
}

func TestNarrator_Disabled(t *testing.T) {
	n := New(false) // explicitly disabled
	msg := n.ToolCallMessage("shell", `{"command": "go test ./..."}`)
	if msg != "" {
		t.Errorf("expected empty when disabled, got: %q", msg)
	}
}

func TestNarrator_ThinkingMessage_Offline(t *testing.T) {
	n := New(true)
	msg := n.ThinkingMessage("I should read the config file first")
	if msg == "" {
		t.Error("expected non-empty thinking fallback")
	}
}

func TestNarrator_AllFallbackTools(t *testing.T) {
	n := New(true)
	tools := []string{"read_file", "write_file", "shell", "search_files", "delegate_tasks", "browser", "memory", "unknown_tool_xyz"}
	for _, name := range tools {
		msg := n.ToolCallMessage(name, `{}`)
		if msg == "" {
			t.Errorf("tool %q: expected non-empty fallback", name)
		}
	}
}
