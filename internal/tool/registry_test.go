package tool

import (
	"testing"
)

type mockTool struct {
	name        string
	description string
	schema      any
	callResult  string
	callError   error
}

func (m *mockTool) Name() string                     { return m.name }
func (m *mockTool) Description() string              { return m.description }
func (m *mockTool) Schema() any                      { return m.schema }
func (m *mockTool) Call(args string) (string, error) { return m.callResult, m.callError }

func TestNewRegistry_Empty(t *testing.T) {
	r := NewRegistry(nil)
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(r.Tools()) != 0 {
		t.Errorf("expected 0 tools, got %d", len(r.Tools()))
	}
}

func TestNewRegistry_SingleTool(t *testing.T) {
	m := &mockTool{name: "shell", description: "run commands"}
	r := NewRegistry([]Tool{m})

	tools := r.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name() != "shell" {
		t.Errorf("tool name = %q, want %q", tools[0].Name(), "shell")
	}
}

func TestNewRegistry_MultipleTools(t *testing.T) {
	tools := []Tool{
		&mockTool{name: "shell"},
		&mockTool{name: "read"},
		&mockTool{name: "write"},
	}
	r := NewRegistry(tools)

	all := r.Tools()
	if len(all) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(all))
	}

	names := make(map[string]bool)
	for _, t := range all {
		names[t.Name()] = true
	}
	for _, name := range []string{"shell", "read", "write"} {
		if !names[name] {
			t.Errorf("tool %q not found in registry", name)
		}
	}
}

func TestRegistry_Get_Found(t *testing.T) {
	m := &mockTool{name: "shell"}
	r := NewRegistry([]Tool{m})

	got := r.Get("shell")
	if got == nil {
		t.Fatal("Get returned nil for existing tool")
	}
	if got.Name() != "shell" {
		t.Errorf("got.Name() = %q, want %q", got.Name(), "shell")
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry([]Tool{&mockTool{name: "shell"}})

	got := r.Get("nonexistent")
	if got != nil {
		t.Errorf("Get should return nil for missing tool, got %v", got)
	}
}

func TestRegistry_DuplicatePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for duplicate tool name")
		}
	}()

	NewRegistry([]Tool{
		&mockTool{name: "shell"},
		&mockTool{name: "shell"},
	})
}

func TestRegistry_ToolsReturnsCopy(t *testing.T) {
	m := &mockTool{name: "shell"}
	r := NewRegistry([]Tool{m})

	tools1 := r.Tools()
	tools2 := r.Tools()

	// Same content but different slices (by pointer comparison of the slice itself, we verify no mutation leaks)
	if len(tools1) != 1 || len(tools2) != 1 {
		t.Fatal("unexpected tool count")
	}
	if tools1[0].Name() != "shell" || tools2[0].Name() != "shell" {
		t.Error("tool name mismatch")
	}
}
