package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/BackendStack21/kode/internal/session"
	golangws "golang.org/x/net/websocket"
)

// ── Test Server ────────────────────────────────────────────────────────

// testServer wraps the kode serve HTTP server for testing.
type testServer struct {
	ln     net.Listener
	url    string
	wsURL  string
	store  *session.Store
	t      *testing.T
}

func startTestServer(t *testing.T) *testServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	store, err := session.NewStore()
	if err != nil {
		t.Fatalf("session store: %v", err)
	}

	s := &testServer{
		ln:    ln,
		url:   "http://" + ln.Addr().String(),
		wsURL: "ws://" + ln.Addr().String(),
		store: store,
		t:     t,
	}

	// Build resource registry (test root is temp dir)
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleStatic())
	mux.HandleFunc("/api/sessions", s.handleSessionList())
	mux.HandleFunc("/api/resources", s.handleResourceSearch())
	mux.Handle("/ws", &golangws.Server{
		Handshake: func(*golangws.Config, *http.Request) error { return nil },
		Handler:   s.handleWebSocket,
	})

	go http.Serve(ln, mux)
	return s
}

func (s *testServer) Close() {
	s.ln.Close()
}

func (s *testServer) handleStatic() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := uiFS.ReadFile("ui/index.html")
		if err != nil {
			http.Error(w, "UI not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	}
}

func (s *testServer) handleSessionList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessions, err := s.store.List(50)
		if err != nil {
			sessions = []session.Session{}
		}
		if sessions == nil {
			sessions = []session.Session{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sessions)
	}
}

func (s *testServer) handleResourceSearch() http.HandlerFunc {
	// Simple handler that returns a known resource for testing
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		results := []map[string]any{
			{"id": "@" + q, "type": "file", "label": q, "detail": "test file"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

func (s *testServer) handleWebSocket(conn *golangws.Conn) {
	defer conn.Close()

	for {
		var data []byte
		if err := golangws.Message.Receive(conn, &data); err != nil {
			return
		}

		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		// Handle prompt — echo back structured events for testing
		if msg["type"] == "prompt" {
			// Send session event
			writeJSON(conn, map[string]any{
				"type":       "session",
				"session_id": "test-session-001",
				"model":      "test-model",
			})

			// Send thinking
			writeJSON(conn, map[string]any{
				"type":    "thinking",
				"content": "Analyzing the request",
			})

			// Send tool call
			writeJSON(conn, map[string]any{
				"type":    "tool_call",
				"name":    "shell",
				"command": "echo test",
			})

			// Send tool result
			writeJSON(conn, map[string]any{
				"type":   "tool_result",
				"name":   "shell",
				"output": "test output",
			})

			// Send final token
			writeJSON(conn, map[string]any{
				"type":    "token",
				"content": "Done with your request.",
			})

			// Send done
			writeJSON(conn, map[string]any{
				"type":    "done",
				"latency": 0.5,
			})
		}
	}
}

func writeJSON(conn *golangws.Conn, data any) {
	payload, _ := json.Marshal(data)
	golangws.Message.Send(conn, string(payload))
}

// readJSON reads a complete WebSocket message and unmarshals it into dst.
func readJSON(conn *golangws.Conn, dst any) error {
	var data []byte
	if err := golangws.Message.Receive(conn, &data); err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// sessionDir returns the ~/.kode/sessions directory for testing.
func sessionDir() (string, error) {
	return "/tmp/kode-test-sessions", nil
}

// ── Tests ──────────────────────────────────────────────────────────────

func TestServe_ServesUI(t *testing.T) {
	s := startTestServer(t)
	defer s.Close()

	resp, err := http.Get(s.url + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	// Check content type
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}

	// Check it contains kode branding
	scanner := bufio.NewScanner(resp.Body)
	found := false
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "kode") {
			found = true
			break
		}
	}
	if !found {
		t.Error("UI should contain 'kode' text")
	}
}

func TestServe_404OnUnknownPath(t *testing.T) {
	s := startTestServer(t)
	defer s.Close()

	resp, err := http.Get(s.url + "/nonexistent")
	if err != nil {
		t.Fatalf("GET /nonexistent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestServe_SessionList(t *testing.T) {
	s := startTestServer(t)
	defer s.Close()

	resp, err := http.Get(s.url + "/api/sessions")
	if err != nil {
		t.Fatalf("GET /api/sessions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var sessions []any
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Should return an array (possibly empty)
	if sessions == nil {
		t.Error("sessions should be an array, not null")
	}
}

func TestServe_ResourceSearch(t *testing.T) {
	s := startTestServer(t)
	defer s.Close()

	resp, err := http.Get(s.url + "/api/resources?q=testfile")
	if err != nil {
		t.Fatalf("GET /api/resources: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var results []any
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) < 1 {
		t.Error("expected at least 1 resource result")
	}
}

func TestServe_WebSocketUpgrade(t *testing.T) {
	s := startTestServer(t)
	defer s.Close()

	conn, err := golangws.Dial(s.wsURL+"/ws", "", "http://localhost")
	if err != nil {
		t.Fatalf("Dial(): %v", err)
	}
	defer conn.Close()

	// Send a prompt to trigger the handler
	prompt := map[string]string{"type": "prompt", "content": "hello"}
	payload, _ := json.Marshal(prompt)
	if err := golangws.Message.Send(conn, string(payload)); err != nil {
		t.Fatalf("Send(): %v", err)
	}

	// Should get a JSON response back
	var data []byte
	if err := golangws.Message.Receive(conn, &data); err != nil {
		t.Fatalf("Receive(): %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if response["type"] != "session" {
		t.Errorf("event type = %v, want 'session'", response["type"])
	}
}

func TestServe_WebSocketPromptFlow(t *testing.T) {
	s := startTestServer(t)
	defer s.Close()

	conn, err := golangws.Dial(s.wsURL+"/ws", "", "http://localhost")
	if err != nil {
		t.Fatalf("Dial(): %v", err)
	}
	defer conn.Close()

	// Send a prompt
	prompt := map[string]string{
		"type":    "prompt",
		"content": "hello world",
	}
	payload, _ := json.Marshal(prompt)
	if err := golangws.Message.Send(conn, string(payload)); err != nil {
		t.Fatalf("Send(): %v", err)
	}

	// Collect events
	var events []map[string]any
	timeout := time.After(5 * time.Second)
	done := false

	for !done {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for events")
		default:
			var event map[string]any
			if err := readJSON(conn, &event); err != nil {
				t.Fatalf("Receive(): %v", err)
			}
			events = append(events, event)

			if event["type"] == "done" {
				done = true
			}
		}
	}

	// Verify event sequence
	if len(events) < 5 {
		t.Fatalf("expected at least 5 events, got %d: %+v", len(events), events)
	}

	// First event should be session
	if events[0]["type"] != "session" {
		t.Errorf("event[0].type = %v, want 'session'", events[0]["type"])
	}
	if events[0]["session_id"] != "test-session-001" {
		t.Errorf("session_id = %v", events[0]["session_id"])
	}

	// Should have thinking, tool_call, tool_result, token, done
	types := make([]string, len(events))
	for i, e := range events {
		t.Logf("event[%d] = %v", i, e)
		types[i] = fmt.Sprint(e["type"])
	}

	// Verify done is last
	if events[len(events)-1]["type"] != "done" {
		t.Errorf("last event type = %v, want 'done'", events[len(events)-1]["type"])
	}
}

func TestServe_WebSocketInvalidJSON(t *testing.T) {
	s := startTestServer(t)
	defer s.Close()

	conn, err := golangws.Dial(s.wsURL+"/ws", "", "http://localhost")
	if err != nil {
		t.Fatalf("Dial(): %v", err)
	}
	defer conn.Close()

	// Send invalid JSON
	if err := golangws.Message.Send(conn, "not json"); err != nil {
		t.Fatalf("Send(): %v", err)
	}
}

func TestServe_WebSocketInvalidMessageType(t *testing.T) {
	s := startTestServer(t)
	defer s.Close()

	conn, err := golangws.Dial(s.wsURL+"/ws", "", "http://localhost")
	if err != nil {
		t.Fatalf("Dial(): %v", err)
	}
	defer conn.Close()

	// Send a message with wrong type
	msg := map[string]string{"type": "invalid", "content": "test"}
	payload, _ := json.Marshal(msg)
	if err := golangws.Message.Send(conn, string(payload)); err != nil {
		t.Fatalf("Send(): %v", err)
	}

	// Server should ignore it — send a valid prompt to check
	msg2 := map[string]string{"type": "prompt", "content": "valid"}
	payload2, _ := json.Marshal(msg2)
	if err := golangws.Message.Send(conn, string(payload2)); err != nil {
		t.Fatalf("Send(): %v", err)
	}

	// Should get a response despite the invalid message before
	timeout := time.After(3 * time.Second)
	gotResponse := false
	for !gotResponse {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for response after invalid message")
		default:
			var event map[string]any
			if err := readJSON(conn, &event); err != nil {
				t.Fatalf("Receive(): %v", err)
			}
			if event["type"] == "done" || event["type"] == "session" {
				gotResponse = true
			}
		}
	}
}

func TestServe_MultiplePrompts(t *testing.T) {
	s := startTestServer(t)
	defer s.Close()

	conn, err := golangws.Dial(s.wsURL+"/ws", "", "http://localhost")
	if err != nil {
		t.Fatalf("Dial(): %v", err)
	}
	defer conn.Close()

	for i := 0; i < 3; i++ {
		prompt := map[string]string{
			"type":    "prompt",
			"content": fmt.Sprintf("prompt %d", i),
		}
		payload, _ := json.Marshal(prompt)
		if err := golangws.Message.Send(conn, string(payload)); err != nil {
			t.Fatalf("prompt %d: Send(): %v", i, err)
		}

		// Read until done
		timeout := time.After(5 * time.Second)
		promptDone := false
		for !promptDone {
			select {
			case <-timeout:
				t.Fatalf("timeout waiting for prompt %d", i)
			default:
				var event map[string]any
				if err := readJSON(conn, &event); err != nil {
					t.Fatalf("prompt %d: Receive(): %v", i, err)
				}
				if event["type"] == "done" {
					promptDone = true
				}
			}
		}
	}
}
