package memory

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMemoryToolName(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, DefaultMemoryConfig())
	tool := NewMemoryTool(mm)

	if tool.Name() != "memory" {
		t.Errorf("expected 'memory', got %q", tool.Name())
	}
}

func TestMemoryToolSchema(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, DefaultMemoryConfig())
	tool := NewMemoryTool(mm)

	schema := tool.Schema()
	if schema == nil {
		t.Fatal("schema is nil")
	}
}

func TestMemoryToolAddAndRead(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, DefaultMemoryConfig())
	tool := NewMemoryTool(mm)

	// Add
	result, err := tool.Call(`{"action":"add","target":"user","content":"User likes Go"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "true") {
		t.Errorf("expected success, got %q", result)
	}

	// Read
	result, err = tool.Call(`{"action":"read"}`)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatal(err)
	}
	if !parsed.Success {
		t.Errorf("expected success, got %q", result)
	}
	if !strings.Contains(parsed.Message, "User Profile") {
		t.Errorf("expected User Profile section, got %q", parsed.Message)
	}
}

func TestMemoryToolReplace(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, DefaultMemoryConfig())
	tool := NewMemoryTool(mm)

	tool.Call(`{"action":"add","target":"user","content":"User likes Go"}`)
	result, err := tool.Call(`{"action":"replace","target":"user","old_text":"Go","content":"User prefers Rust"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "true") {
		t.Errorf("expected success, got %q", result)
	}

	// Verify via read
	user, _, _ := mm.ReadFacts()
	if !strings.Contains(user, "Rust") {
		t.Errorf("expected Rust, got %q", user)
	}
}

func TestMemoryToolRemove(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, DefaultMemoryConfig())
	tool := NewMemoryTool(mm)

	tool.Call(`{"action":"add","target":"user","content":"entry to remove"}`)
	result, err := tool.Call(`{"action":"remove","target":"user","old_text":"to remove"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "true") {
		t.Errorf("expected success, got %q", result)
	}

	user, _, _ := mm.ReadFacts()
	if user != "" {
		t.Errorf("expected empty after remove, got %q", user)
	}
}

func TestMemoryToolMissingContent(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, DefaultMemoryConfig())
	tool := NewMemoryTool(mm)

	result, err := tool.Call(`{"action":"add","target":"user"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "false") {
		t.Errorf("expected failure, got %q", result)
	}
}

func TestMemoryToolMissingOldText(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, DefaultMemoryConfig())
	tool := NewMemoryTool(mm)

	result, err := tool.Call(`{"action":"remove","target":"user"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "false") {
		t.Errorf("expected failure, got %q", result)
	}
}

func TestMemoryToolBadAction(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, DefaultMemoryConfig())
	tool := NewMemoryTool(mm)

	result, err := tool.Call(`{"action":"nonexistent"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "false") {
		t.Errorf("expected failure, got %q", result)
	}
}

func TestMemoryToolSearch(t *testing.T) {
	dir := t.TempDir()
	llm := &mockLLM{
		responses: map[string]string{
			"sess-001": "found auth fix",
			"sess-002": "query results",
		},
	}

	// Pre-populate episodes directly
	es := NewEpisodeStore(dir, func(query string, episodes []EpisodeMeta) ([]EpisodeMeta, error) {
		return episodes, nil
	})
	es.Write("sess-001", "fixed auth token validation", 5)

	mm := NewMemoryManager(dir, llm, DefaultMemoryConfig())
	mm.episodes = es

	tool := NewMemoryTool(mm)
	result, err := tool.Call(`{"action":"search","query":"auth"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "sess-001") {
		t.Errorf("expected sess-001 in results, got %q", result)
	}
}

func TestMemoryToolConsolidate(t *testing.T) {
	dir := t.TempDir()
	llm := &mockLLM{
		responses: map[string]string{
			"Consolidate": "Merged fact one § Merged fact two",
		},
	}
	mm := NewMemoryManager(dir, llm, DefaultMemoryConfig())
	tool := NewMemoryTool(mm)

	tool.Call(`{"action":"add","target":"env","content":"Project uses Go 1.22"}`)
	tool.Call(`{"action":"add","target":"env","content":"Uses chi router"}`)

	result, err := tool.Call(`{"action":"consolidate","target":"env"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "true") {
		t.Errorf("expected success, got %q", result)
	}
}

func TestMemoryToolReturnsJSON(t *testing.T) {
	mm := NewMemoryManager(t.TempDir(), nil, DefaultMemoryConfig())
	tool := NewMemoryTool(mm)

	result, err := tool.Call(`{"action":"read"}`)
	if err != nil {
		t.Fatal(err)
	}
	// Should be valid JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Errorf("result must be valid JSON: %v", err)
	}

	_, hasSuccess := parsed["success"]
	_, hasMessage := parsed["message"]
	if !hasSuccess || !hasMessage {
		t.Errorf("result should have 'success' and 'message' fields, got keys: %v", parsed)
	}
}
