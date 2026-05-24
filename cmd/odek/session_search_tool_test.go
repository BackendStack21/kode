package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BackendStack21/kode/internal/llm"
	"github.com/BackendStack21/kode/internal/session"
)

// ── Test Setup ──────────────────────────────────────────────────────────

// seedSessionStore populates a session store with test data and returns it.
// Sets HOME to a temp dir so NewStore() creates isolated session files.
func seedSessionStore(t *testing.T) (*session.Store, func()) {
	t.Helper()

	// Create a temporary home directory so NewStore() uses isolated paths.
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)

	store, err := session.NewStore()
	if err != nil {
		os.Setenv("HOME", origHome)
		t.Fatalf("session.NewStore: %v", err)
	}

	now := time.Now().UTC()

	seed := []struct {
		id    string
		task  string
		model string
		turns int
		buf   []string
		msgs  []llm.Message
		age   time.Duration
	}{
		{
			id: "20260520-auth-fix", task: "fix O_NOFOLLOW in file_tool.go",
			model: "deepseek-v4-flash", turns: 8,
			buf: []string{"10:00 user asked about symlink attack", "10:02 agent patched file_tool.go with O_NOFOLLOW"},
			msgs: []llm.Message{
				{Role: "user", Content: "We have a symlink attack in file_tool.go — read_file follows symlinks"},
				{Role: "assistant", Content: "Adding O_NOFOLLOW to all file opens... done."},
			},
			age: 4 * 24 * time.Hour,
		},
		{
			id: "20260522-native-tools", task: "add sort/head_tail/base64 native tools",
			model: "deepseek-v4-flash", turns: 12,
			buf: []string{"11:00 user requested 5 native perf tools", "11:05 agent implemented sort tool"},
			msgs: []llm.Message{
				{Role: "user", Content: "Add sort, head_tail, base64, tr, word_count as native tools"},
				{Role: "assistant", Content: "Implemented all 5 tools with tests."},
			},
			age: 2 * 24 * time.Hour,
		},
		{
			id: "20260524-transcribe", task: "implement audio transcription via whisper.cpp",
			model: "deepseek-v4-flash", turns: 15,
			buf: []string{"12:00 discussed transcribe tool proposal", "12:30 implemented transcribe_tool.go"},
			msgs: []llm.Message{
				{Role: "user", Content: "I want a transcribe tool using local whisper.cpp"},
				{Role: "assistant", Content: "Created transcribe tool with model download, config, and tests."},
			},
			age: 1 * time.Hour,
		},
		{
			id: "20260510-old-setup", task: "initial project setup",
			model: "claude-sonnet-4", turns: 3,
			buf:   []string{"09:00 user set up project structure"},
			msgs: []llm.Message{
				{Role: "user", Content: "Set up the project with Go modules"},
			},
			age: 14 * 24 * time.Hour,
		},
	}

	for i, s := range seed {
		created := now.Add(-s.age)
		updated := created.Add(time.Duration(s.turns) * time.Minute)
		sess := &session.Session{
			ID:        s.id,
			CreatedAt: created,
			UpdatedAt: updated,
			Model:     s.model,
			Turns:     s.turns,
			Task:      s.task,
			Messages:  s.msgs,
			Buffer:    s.buf,
		}
		if err := store.Save(sess); err != nil {
			os.Setenv("HOME", origHome)
			t.Fatalf("save session %d: %v", i, err)
		}
	}

	return store, func() { os.Setenv("HOME", origHome) }
}

// mustParse is a test helper that calls the tool and unmarshals the basic result type.
type sessionSearchBasicResult struct {
	Action   string `json:"action"`
	Count    int    `json:"count"`
	Error    string `json:"error,omitempty"`
	ID       string `json:"id,omitempty"`
	Task     string `json:"task,omitempty"`
	Turns    int    `json:"turns,omitempty"`
	Messages int    `json:"messages,omitempty"`
	// Sessions array — decoded dynamically since structure varies
}

func callSessionSearch(t *testing.T, tool *sessionSearchTool, args string) string {
	t.Helper()
	result, err := tool.Call(args)
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	return result
}

func parseResult(t *testing.T, data string) sessionSearchBasicResult {
	t.Helper()
	var r sessionSearchBasicResult
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, data)
	}
	return r
}

// ── Tests ───────────────────────────────────────────────────────────────

func TestSessionSearch_List(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"list"}`)
	r := parseResult(t, result)

	if r.Action != "list" {
		t.Errorf("action = %q, want 'list'", r.Action)
	}
	if r.Count == 0 {
		t.Fatal("expected sessions in list")
	}
	if r.Count > 5 {
		t.Errorf("count = %d, want <= 5 (default limit)", r.Count)
	}
	// Most recent session should come first
	// We seeded 4, limit default is 5, so all 4 should appear
	if r.Count != 4 {
		t.Errorf("count = %d, want 4 (seeded sessions)", r.Count)
	}
}

func TestSessionSearch_ListWithLimit(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"list","limit":2}`)
	r := parseResult(t, result)

	if r.Count != 2 {
		t.Errorf("count = %d, want 2", r.Count)
	}
}

func TestSessionSearch_Get(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"get","query":"20260520-auth-fix"}`)
	r := parseResult(t, result)

	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.ID != "20260520-auth-fix" {
		t.Errorf("id = %q, want '20260520-auth-fix'", r.ID)
	}
	if r.Task != "fix O_NOFOLLOW in file_tool.go" {
		t.Errorf("task = %q", r.Task)
	}
	if r.Turns != 8 {
		t.Errorf("turns = %d, want 8", r.Turns)
	}
	if r.Messages == 0 {
		t.Errorf("messages = %d, want > 0", r.Messages)
	}
}

func TestSessionSearch_GetNotFound(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"get","query":"nonexistent-id"}`)
	r := parseResult(t, result)

	if r.Error == "" {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestSessionSearch_GetEmptyID(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"get","query":""}`)
	r := parseResult(t, result)

	if r.Error == "" {
		t.Fatal("expected error for empty session ID")
	}
}

func TestSessionSearch_Find(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"find","query":"audio transcription"}`)
	r := parseResult(t, result)

	if r.Count == 0 {
		t.Fatal("expected session matching 'audio transcription'")
	}
	if !strings.Contains(result, "implement audio transcription") {
		t.Errorf("expected session about 'implement audio transcription', got: %s", result)
	}
}

func TestSessionSearch_FindNoMatch(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"find","query":"zzz_nonexistent_zzz"}`)
	r := parseResult(t, result)

	if r.Count != 0 {
		t.Errorf("expected 0 matches, got %d", r.Count)
	}
}

func TestSessionSearch_Search(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"search","query":"O_NOFOLLOW symlink"}`)
	r := parseResult(t, result)

	if r.Count == 0 {
		t.Fatal("expected search results for 'O_NOFOLLOW'")
	}
	// Should find the auth-fix session (O_NOFOLLOW in task + symlink in buffer)
	if !strings.Contains(result, "20260520-auth-fix") {
		t.Errorf("expected auth-fix session in results, got: %s", result)
	}
}

func TestSessionSearch_SearchDeep(t *testing.T) {
	// Search for content only in full session messages (not in task or buffer)
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)

	// "Go modules" appears only in the message content of the old-session,
	// not in its task or buffer
	result := callSessionSearch(t, tool, `{"action":"search","query":"Go modules project setup"}`)
	r := parseResult(t, result)

	if r.Count == 0 {
		t.Fatal("expected deep search result for 'Go modules'")
	}
	if !strings.Contains(result, "20260510-old-setup") {
		t.Errorf("expected old-setup session in deep search results")
	}
}

func TestSessionSearch_SearchEmptyQuery(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"search","query":""}`)
	r := parseResult(t, result)

	if r.Error == "" {
		t.Fatal("expected error for empty search query")
	}
}

func TestSessionSearch_FindEmptyQuery(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"find","query":""}`)
	r := parseResult(t, result)

	if r.Error == "" {
		t.Fatal("expected error for empty find query")
	}
}

func TestSessionSearch_UnknownAction(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"unknown"}`)
	r := parseResult(t, result)

	if r.Error == "" {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(r.Error, "unknown") {
		t.Errorf("error should mention 'unknown', got: %s", r.Error)
	}
}

func TestSessionSearch_InvalidJSON(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result, err := tool.Call(`{bad json}`)
	if err != nil {
		return // error is acceptable
	}
	r := parseResult(t, result)
	if !strings.Contains(r.Error, "invalid") {
		t.Errorf("expected 'invalid' in error, got: %s", r.Error)
	}
}

func TestSessionSearch_MissingAction(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{}`)
	r := parseResult(t, result)

	if r.Error == "" {
		t.Fatal("expected error for missing action")
	}
}

func TestSessionSearch_EmptyStore(t *testing.T) {
	// Create an empty store (no seed sessions)
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	store, err := session.NewStore()
	if err != nil {
		os.Setenv("HOME", origHome)
		t.Fatalf("session.NewStore: %v", err)
	}

	tool := newSessionSearchTool(store)

	// List with empty store should return 0 results, not error
	result := callSessionSearch(t, tool, `{"action":"list"}`)
	r := parseResult(t, result)
	if r.Count != 0 {
		t.Errorf("count = %d, want 0 for empty store", r.Count)
	}

	os.Setenv("HOME", origHome)
}

func TestSessionSearch_Metadata(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	if n := tool.Name(); n != "session_search" {
		t.Errorf("Name = %q, want 'session_search'", n)
	}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
	if tool.Schema() == nil {
		t.Error("Schema should not be nil")
	}

	// Verify schema enum includes all actions
	schema := tool.Schema().(map[string]any)
	props := schema["properties"].(map[string]any)
	action := props["action"].(map[string]any)
	enumRaw := action["enum"]
	var actions []string
	switch e := enumRaw.(type) {
	case []string:
		actions = e
	case []any:
		for _, v := range e {
			actions = append(actions, v.(string))
		}
	default:
		t.Fatalf("enum has unexpected type %T", enumRaw)
	}
	expected := []string{"list", "search", "get", "find"}
	for _, e := range expected {
		found := false
		for _, a := range actions {
			if a == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("schema enum missing action %q, got %v", e, actions)
		}
	}
}

func TestSessionSearch_ListOrder(t *testing.T) {
	// Verify list returns most recent first
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"list","limit":4}`)

	// Parse into sessions array
	var resp struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Sessions) != 4 {
		t.Fatalf("expected 4 sessions, got %d", len(resp.Sessions))
	}

	// Most recent first (transcribe is 1h old, native-tools is 2d, auth-fix is 4d, old-setup is 14d)
	expected := []string{
		"20260524-transcribe",  // 1 hour
		"20260522-native-tools", // 2 days
		"20260520-auth-fix",    // 4 days
		"20260510-old-setup",   // 14 days
	}
	for i, e := range expected {
		if resp.Sessions[i].ID != e {
			t.Errorf("position %d = %q, want %q", i, resp.Sessions[i].ID, e)
		}
	}
}

func TestSessionSearch_GetFullContent(t *testing.T) {
	// Verify get returns buffer and message count
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"get","query":"20260524-transcribe"}`)

	var resp struct {
		Buffer   []string `json:"buffer"`
		Messages int      `json:"messages"`
		Error    string   `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("error: %s", resp.Error)
	}
	if len(resp.Buffer) == 0 {
		t.Error("expected buffer to be populated")
	}
	if resp.Messages == 0 {
		t.Error("expected messages > 0")
	}
}

func TestSessionSearch_SearchByBuffer(t *testing.T) {
	// Search for content in buffer (not task)
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"search","query":"symlink attack"}`)

	var resp struct {
		Sessions []struct {
			ID   string `json:"id"`
			Task string `json:"task"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Sessions) == 0 {
		t.Fatal("expected sessions matching 'symlink attack'")
	}
	found := false
	for _, s := range resp.Sessions {
		if s.ID == "20260520-auth-fix" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected auth-fix session in buffer search results")
	}
}

func TestSessionSearch_LimitMax(t *testing.T) {
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"list","limit":100}`)
	r := parseResult(t, result)

	// Should be capped at 20
	if r.Count > 20 {
		t.Errorf("count = %d, should be capped at 20", r.Count)
	}
}

func TestSessionSearch_SearchNoDeepFallback(t *testing.T) {
	// Verify that search works even without loading full sessions
	// when task/buffer matches are sufficient
	store, cleanup := seedSessionStore(t)
	defer cleanup()

	tool := newSessionSearchTool(store)
	result := callSessionSearch(t, tool, `{"action":"search","query":"native tools sort"}`)

	// Should find "add sort/head_tail/base64 native tools" from task alone
	var resp struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	found := false
	for _, s := range resp.Sessions {
		if s.ID == "20260522-native-tools" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected native-tools session in task-only search results")
	}
}
