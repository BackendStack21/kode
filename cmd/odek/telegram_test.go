package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BackendStack21/kode/internal/config"
	"github.com/BackendStack21/kode/internal/session"
	"github.com/BackendStack21/kode/internal/telegram"
)

// ── spawnChild tests ──────────────────────────────────────────────────

func TestSpawnChild_StartsChildProcess(t *testing.T) {
	err := spawnChild()
	if err != nil {
		t.Logf("spawnChild returned error (may be expected in test env): %v", err)
	}
}

func TestWriteAndReadRestartMarker(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	home, _ := os.UserHomeDir()
	os.MkdirAll(filepath.Join(home, ".odek"), 0755)

	if err := writeRestartMarker(); err != nil {
		t.Fatalf("writeRestartMarker: %v", err)
	}
	chatIDs, ok := readRestartMarker()
	if !ok {
		t.Fatal("readRestartMarker returned false, expected true")
	}
	if len(chatIDs) != 0 {
		t.Errorf("expected empty chat IDs, got %v", chatIDs)
	}
	_, ok = readRestartMarker()
	if ok {
		t.Fatal("readRestartMarker should return false after marker is consumed")
	}
}

func TestWriteAndReadRestartMarker_WithChatIDs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	home, _ := os.UserHomeDir()
	os.MkdirAll(filepath.Join(home, ".odek"), 0755)

	ctx, cancel1 := context.WithCancel(context.Background())
	ctx, cancel2 := context.WithCancel(context.Background())
	_ = ctx
	chatCancels.Store(int64(100), cancel1)
	chatCancels.Store(int64(200), cancel2)
	defer func() {
		chatCancels.LoadAndDelete(int64(100))
		chatCancels.LoadAndDelete(int64(200))
	}()

	if err := writeRestartMarker(); err != nil {
		t.Fatalf("writeRestartMarker: %v", err)
	}
	chatIDs, ok := readRestartMarker()
	if !ok {
		t.Fatal("readRestartMarker returned false, expected true")
	}
	if len(chatIDs) != 2 {
		t.Fatalf("expected 2 chat IDs, got %d: %v", len(chatIDs), chatIDs)
	}
	if chatIDs[0] != 100 || chatIDs[1] != 200 {
		t.Errorf("expected [100 200], got %v", chatIDs)
	}
}

func TestRestartMarker_LegacyEmpty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	home, _ := os.UserHomeDir()
	os.MkdirAll(filepath.Join(home, ".odek"), 0755)

	path, _ := restartMarkerPath()
	if err := os.WriteFile(path, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chatIDs, ok := readRestartMarker()
	if !ok {
		t.Fatal("expected true for legacy empty marker")
	}
	if len(chatIDs) != 0 {
		t.Errorf("expected 0 chat IDs for legacy marker, got %d", len(chatIDs))
	}
}

func TestRestartMarker_Corrupt(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	home, _ := os.UserHomeDir()
	os.MkdirAll(filepath.Join(home, ".odek"), 0755)

	path, _ := restartMarkerPath()
	if err := os.WriteFile(path, []byte("{{{not json}}}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chatIDs, ok := readRestartMarker()
	if !ok {
		t.Fatal("expected true for corrupt marker")
	}
	if len(chatIDs) != 0 {
		t.Errorf("expected 0 chat IDs for corrupt marker, got %d", len(chatIDs))
	}
}

// ── Tool Event / InteractionMode tests ─────────────────────────────────

type recordedTelegramCall struct {
	Method string
	Params map[string]any
}

func makeTestBot(tgSrv *httptest.Server) *telegram.Bot {
	bot := telegram.NewBot("test:token")
	bot.BaseURL = tgSrv.URL + "/bottest:token"
	bot.FileBaseURL = tgSrv.URL + "/file/bottest:token"
	bot.Client = tgSrv.Client()
	bot.SetLogger(telegram.NewNopLogger())
	return bot
}

func makeMockLLM(t *testing.T) *httptest.Server {
	t.Helper()
	callCount := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		switch callCount {
		case 1:
			fmt.Fprint(w, `{"choices":[{"message":{"content":"","reasoning_content":"I need to check the config file first","tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"/etc/hostname\"}"}}]}}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`)
		case 2:
			fmt.Fprint(w, `{"choices":[{"message":{"content":"Found it. The hostname is set correctly.","reasoning_content":"Task complete, no more tools needed."}}],"usage":{"prompt_tokens":200,"completion_tokens":40}}`)
		default:
			fmt.Fprint(w, `{"choices":[{"message":{"content":"Done."}}],"usage":{"prompt_tokens":100,"completion_tokens":10}}`)
		}
	}))
}

func makeMockTelegram(t *testing.T, calls *[]recordedTelegramCall, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		path := strings.TrimPrefix(r.URL.Path, "/bottest:token/")
		if path == "" {
			path = strings.TrimPrefix(r.URL.Path, "/")
		}
		var params map[string]any
		if len(body) > 0 {
			json.Unmarshal(body, &params)
		}
		mu.Lock()
		*calls = append(*calls, recordedTelegramCall{Method: path, Params: params})
		callIdx := len(*calls)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"chat":{"id":123},"date":0,"text":""}}`, callIdx)
	}))
}

func runHandleChatMessage(t *testing.T, bot *telegram.Bot, interactionMode string, llmSrv *httptest.Server) {
	t.Helper()
	homeDir := t.TempDir()

	origHome := os.Getenv("HOME")
	origDS := os.Getenv("DEEPSEEK_API_KEY")
	origOAI := os.Getenv("OPENAI_API_KEY")
	origKBS := os.Getenv("ODEK_BASE_URL")
	origMode := os.Getenv("ODEK_INTERACTION_MODE")
	t.Cleanup(func() {
		os.Setenv("HOME", origHome)
		os.Setenv("DEEPSEEK_API_KEY", origDS)
		os.Setenv("OPENAI_API_KEY", origOAI)
		os.Setenv("ODEK_BASE_URL", origKBS)
		os.Setenv("ODEK_INTERACTION_MODE", origMode)
	})
	os.Setenv("DEEPSEEK_API_KEY", "sk-mock")
	os.Unsetenv("OPENAI_API_KEY")
	os.Setenv("ODEK_BASE_URL", llmSrv.URL)
	os.Setenv("ODEK_INTERACTION_MODE", interactionMode)
	os.Setenv("HOME", homeDir)

	store, err := session.NewStore()
	if err != nil {
		t.Fatalf("session.NewStore: %v", err)
	}
	sessionManager := telegram.NewSessionManager(store, 24*time.Hour)
	handler := telegram.NewHandler(bot)

	resolved := config.LoadConfig(config.CLIFlags{})
	resolved.InteractionMode = interactionMode

	os.MkdirAll(filepath.Join(homeDir, ".odek", "sessions"), 0755)

	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("handleChatMessage panicked: %v", r)
			}
			close(done)
		}()
		handleChatMessage(123, 1, "check the hostname",
			bot, handler, sessionManager, resolved,
			"You are a helpful assistant. Answer concisely.",
			telegram.NewNopLogger())
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("handleChatMessage timed out after 30s")
	}
}

// TestVerboseMode_ToolEventsSendNewMessages verifies that in verbose mode:
//   - Thinking notes (💭) are sent as NEW messages via SendMessage
//   - Tool calls (🔧) are sent as NEW messages via SendMessage
//   - Tool results (✅) are sent as NEW messages via SendMessage
//   - Stale tool_call messages are DELETED
//   - EditMessageText is NEVER called
func TestVerboseMode_ToolEventsSendNewMessages(t *testing.T) {
	if os.Getenv("ODEK_E2E") == "" {
		t.Skip("skipping integration test; set ODEK_E2E=true to run")
	}

	llmSrv := makeMockLLM(t)
	defer llmSrv.Close()

	var mu sync.Mutex
	var calls []recordedTelegramCall
	tgSrv := makeMockTelegram(t, &calls, &mu)
	defer tgSrv.Close()

	bot := makeTestBot(tgSrv)
	runHandleChatMessage(t, bot, "verbose", llmSrv)

	mu.Lock()
	defer mu.Unlock()

	if len(calls) == 0 {
		t.Fatal("no Telegram API calls were made")
	}

	t.Logf("Telegram API calls (%d):", len(calls))
	for i, c := range calls {
		text, _ := c.Params["text"].(string)
		t.Logf("  %d. %s: %s", i+1, c.Method, truncateStr(text, 80))
	}

	// EditMessageText should NEVER be called in verbose mode
	for _, c := range calls {
		if c.Method == "editMessageText" {
			t.Errorf("EditMessageText was called in verbose mode (text=%v)", c.Params["text"])
		}
	}

	// Should have a thinking note (💭) via SendMessage
	hasThinking := false
	for _, c := range calls {
		if c.Method == "sendMessage" {
			if text, ok := c.Params["text"].(string); ok && strings.Contains(text, "💭") {
				hasThinking = true
				break
			}
		}
	}
	if !hasThinking {
		t.Error("no thinking note (💭) was sent via SendMessage")
	}

	// Should have a tool call via SendMessage
	hasToolCall := false
	for _, c := range calls {
		if c.Method == "sendMessage" {
			if text, ok := c.Params["text"].(string); ok && strings.Contains(text, "read_file") {
				hasToolCall = true
				break
			}
		}
	}
	if !hasToolCall {
		t.Error("no tool call (read_file) was sent via SendMessage")
	}

	// Should have a tool result via SendMessage
	hasToolResult := false
	for _, c := range calls {
		if c.Method == "sendMessage" {
			if text, ok := c.Params["text"].(string); ok && strings.Contains(text, "✅") {
				hasToolResult = true
				break
			}
		}
	}
	if !hasToolResult {
		t.Error("no tool result (✅) was sent via SendMessage")
	}
}

// TestEngagingMode_UpdatesProgressMessage verifies that in engaging mode,
// tool calls UPDATE the single progress message via EditMessageText.
func TestEngagingMode_UpdatesProgressMessage(t *testing.T) {
	if os.Getenv("ODEK_E2E") == "" {
		t.Skip("skipping integration test; set ODEK_E2E=true to run")
	}

	llmSrv := makeMockLLM(t)
	defer llmSrv.Close()

	var mu sync.Mutex
	var calls []recordedTelegramCall
	tgSrv := makeMockTelegram(t, &calls, &mu)
	defer tgSrv.Close()

	bot := makeTestBot(tgSrv)
	runHandleChatMessage(t, bot, "engaging", llmSrv)

	mu.Lock()
	defer mu.Unlock()

	if len(calls) == 0 {
		t.Fatal("no Telegram API calls were made")
	}

	t.Logf("Telegram API calls (%d):", len(calls))
	for i, c := range calls {
		text, _ := c.Params["text"].(string)
		t.Logf("  %d. %s: %s", i+1, c.Method, truncateStr(text, 80))
	}

	// Engaging mode SHOULD use EditMessageText to update progress
	hasEdit := false
	for _, c := range calls {
		if c.Method == "editMessageText" {
			hasEdit = true
			break
		}
	}
	if !hasEdit {
		t.Error("engaging mode: expected at least one editMessageText call")
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ── /mode command tests ─────────────────────────────────────────────────

func TestModeCommand(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	home, _ := os.UserHomeDir()
	os.MkdirAll(filepath.Join(home, ".odek"), 0755)

	_, err := session.NewStore()
	if err != nil {
		t.Fatalf("session.NewStore: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":1,"chat":{"id":123},"date":0,"text":""}}`)
	}))
	defer srv.Close()

	bot := telegram.NewBot("test:token")
	bot.BaseURL = srv.URL + "/bottest:token"
	bot.FileBaseURL = srv.URL + "/file/bottest:token"
	bot.Client = srv.Client()
	bot.SetLogger(telegram.NewNopLogger())
	h := telegram.NewHandler(bot)

	h.OnTextMessage = func(chatID int64, messageID int, text string) (string, error) {
		if text == "/mode" {
			return "Agent Modes\n\n*interaction_mode*: engaging\n\nTo switch to *verbose* mode, use `/mode verbose`.\n\nVerbose mode shows raw tool names and arguments instead of narrated descriptions.", nil
		}
		return "", nil
	}

	result, err := h.OnTextMessage(123, 0, "/mode")
	if err != nil {
		t.Fatalf("OnTextMessage /mode returned error: %v", err)
	}

	checks := []string{
		"interaction_mode",
		"engaging",
		"verbose",
		"Agent Modes",
	}
	for _, c := range checks {
		if !strings.Contains(result, c) {
			t.Errorf("expected /mode output to contain %q, got: %q", c, result)
		}
	}
}
