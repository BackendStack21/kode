package ws

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── Helpers ────────────────────────────────────────────────────────────

func startTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	wsURL := "ws://" + srv.Listener.Addr().String()
	return srv, wsURL
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := Upgrade(w, r)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		opcode, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		// Echo back
		conn.WriteMessage(opcode, data)
	}
}

// ── Upgrade ────────────────────────────────────────────────────────────

func TestUpgrade_ValidHandshake(t *testing.T) {
	srv, wsURL := startTestServer(t, wsHandler)
	defer srv.Close()

	conn, _, err := Dial(wsURL)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer conn.Close()
}

func TestUpgrade_RejectsNonWebSocket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := Upgrade(w, r)
		if err == nil {
			t.Error("Upgrade should fail for non-WebSocket request")
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	resp, err := http.Get("http://" + srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// ── Read/Write ─────────────────────────────────────────────────────────

func TestWriteRead_TextMessage(t *testing.T) {
	srv, wsURL := startTestServer(t, wsHandler)
	defer srv.Close()

	conn, _, err := Dial(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	msg := "hello websocket"
	if err := conn.WriteMessage(OpText, []byte(msg)); err != nil {
		t.Fatalf("WriteMessage() error: %v", err)
	}

	opcode, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error: %v", err)
	}
	if opcode != OpText {
		t.Errorf("opcode = %d, want %d", opcode, OpText)
	}
	if string(data) != msg {
		t.Errorf("payload = %q, want %q", string(data), msg)
	}
}

func TestWriteRead_BinaryMessage(t *testing.T) {
	srv, wsURL := startTestServer(t, wsHandler)
	defer srv.Close()

	conn, _, err := Dial(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	payload := []byte{0x00, 0xFF, 0xAB, 0xCD}
	if err := conn.WriteMessage(OpBinary, payload); err != nil {
		t.Fatalf("WriteMessage() error: %v", err)
	}

	opcode, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error: %v", err)
	}
	if opcode != OpBinary {
		t.Errorf("opcode = %d, want %d", opcode, OpBinary)
	}
	for i := range payload {
		if data[i] != payload[i] {
			t.Errorf("byte %d = %x, want %x", i, data[i], payload[i])
		}
	}
}

func TestWriteRead_EmptyMessage(t *testing.T) {
	srv, wsURL := startTestServer(t, wsHandler)
	defer srv.Close()

	conn, _, err := Dial(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(OpText, []byte{}); err != nil {
		t.Fatalf("WriteMessage() error: %v", err)
	}

	opcode, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error: %v", err)
	}
	if opcode != OpText {
		t.Errorf("opcode = %d, want %d", opcode, OpText)
	}
	if len(data) != 0 {
		t.Errorf("payload len = %d, want 0", len(data))
	}
}

func TestWriteRead_LargeMessage(t *testing.T) {
	srv, wsURL := startTestServer(t, wsHandler)
	defer srv.Close()

	conn, _, err := Dial(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// >125 bytes to test extended length (16-bit)
	msg := strings.Repeat("A", 200)
	if err := conn.WriteMessage(OpText, []byte(msg)); err != nil {
		t.Fatalf("WriteMessage() error: %v", err)
	}

	opcode, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error: %v", err)
	}
	if opcode != OpText {
		t.Errorf("opcode = %d, want %d", opcode, OpText)
	}
	if string(data) != msg {
		t.Errorf("payload length = %d, want %d", len(data), len(msg))
	}
}

func TestWriteRead_VeryLargeMessage(t *testing.T) {
	srv, wsURL := startTestServer(t, wsHandler)
	defer srv.Close()

	conn, _, err := Dial(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// >65535 bytes to test 64-bit extended length
	msg := strings.Repeat("B", 70000)
	if err := conn.WriteMessage(OpText, []byte(msg)); err != nil {
		t.Fatalf("WriteMessage() error: %v", err)
	}

	opcode, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error: %v", err)
	}
	if opcode != OpText {
		t.Errorf("opcode = %d, want %d", opcode, OpText)
	}
	if len(data) != 70000 {
		t.Errorf("payload length = %d, want 70000", len(data))
	}
}

func TestWriteRead_MultipleMessages(t *testing.T) {
	srv, wsURL := startTestServer(t, wsHandler)
	defer srv.Close()

	conn, _, err := Dial(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	messages := []string{"first", "second", "third"}
	for _, msg := range messages {
		if err := conn.WriteMessage(OpText, []byte(msg)); err != nil {
			t.Fatalf("WriteMessage(%q) error: %v", msg, err)
		}
	}

	for _, expected := range messages {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage() error: %v", err)
		}
		if string(data) != expected {
			t.Errorf("expected %q, got %q", expected, string(data))
		}
	}
}

// ── Close ──────────────────────────────────────────────────────────────

func TestClose(t *testing.T) {
	srv, wsURL := startTestServer(t, wsHandler)
	defer srv.Close()

	conn, _, err := Dial(wsURL)
	if err != nil {
		t.Fatal(err)
	}

	// Close from client side
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	// Subsequent read should fail
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Error("ReadMessage after Close should return error")
	}
}

// ── Dial Error Cases ───────────────────────────────────────────────────

func TestDial_InvalidURL(t *testing.T) {
	_, _, err := Dial("invalid-url")
	if err == nil {
		t.Error("Dial with invalid URL should return error")
	}
}

func TestDial_ConnectionRefused(t *testing.T) {
	_, _, err := Dial("ws://127.0.0.1:1")
	if err == nil {
		t.Error("Dial to closed port should return error")
	}
}

// ── Raw Frame Parsing ──────────────────────────────────────────────────

func TestUpgrade_ValidatesKey(t *testing.T) {
	// Connect with raw HTTP, missing Sec-WebSocket-Key
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := Upgrade(w, r)
		if err == nil {
			t.Error("expected error for missing key")
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	req := "GET / HTTP/1.1\r\nHost: test\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"
	conn.Write([]byte(req))
	br := bufio.NewReader(conn)
	resp, _ := http.ReadResponse(br, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// ── Concurrent Messages ────────────────────────────────────────────────

func TestConcurrentWrites(t *testing.T) {
	srv, wsURL := startTestServer(t, wsHandler)
	defer srv.Close()

	conn, _, err := Dial(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Concurrent writes from multiple goroutines
	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func(n int) {
			msg := []byte("msg-" + string(rune('0'+n)))
			done <- conn.WriteMessage(OpText, msg)
		}(i)
	}

	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent write error: %v", err)
		}
	}

	// Read back all three
	received := make(map[string]bool)
	for i := 0; i < 3; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage() error: %v", err)
		}
		received[string(data)] = true
	}

	for _, expected := range []string{"msg-0", "msg-1", "msg-2"} {
		if !received[expected] {
			t.Errorf("missing message: %s", expected)
		}
	}
}
