package telegram

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestSendChunk_FailureSendsFallback verifies that when both MarkdownV2 and
// plain-text sends fail, OnError is called so the user knows.
func TestSendChunk_FailureSendsFallback(t *testing.T) {
	var sendAttempts atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sendAttempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"ok":false,"description":"Internal server error"}`))
	}))
	defer ts.Close()

	h := &Handler{
		Bot: &Bot{
			Token:       "test:token",
			BaseURL:     ts.URL,
			FileBaseURL: ts.URL + "/file",
			Client:      ts.Client(),
			log:         NewNopLogger(),
		},
		log:    NewNopLogger(),
		Config: HandlerConfig{MaxMsgLength: 4096},
	}

	var errCalled atomic.Bool
	h.OnError = func(chatID int64, err error) {
		if chatID == 42 {
			errCalled.Store(true)
		}
	}

	h.sendChunk(42, "test content", 99)

	if !errCalled.Load() {
		t.Error("OnError was not called after sendChunk total failure")
	}

	// Should have tried MarkdownV2 + plain text retry = 2 attempts
	attempts := int(sendAttempts.Load())
	if attempts < 2 {
		t.Errorf("expected at least 2 sendMessage attempts, got %d", attempts)
	}
}

// TestSendChunk_MarkdownRetryOnParseError verifies the retry-on-parse-error
// path: MarkdownV2 fails with "can't parse", retries with plain text.
func TestSendChunk_MarkdownRetryOnParseError(t *testing.T) {
	var attemptCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attemptCount.Add(1)
		if count == 1 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"ok":false,"description":"Bad Request: can't parse entities"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"result":{"message_id":2}}`))
	}))
	defer ts.Close()

	h := &Handler{
		Bot: &Bot{
			Token:       "test:token",
			BaseURL:     ts.URL,
			FileBaseURL: ts.URL + "/file",
			Client:      ts.Client(),
			log:         NewNopLogger(),
		},
		log:    NewNopLogger(),
		Config: HandlerConfig{MaxMsgLength: 4096},
	}

	var errCalled atomic.Bool
	h.OnError = func(chatID int64, err error) {
		errCalled.Store(true)
	}

	h.sendChunk(42, "test content", 0)

	if errCalled.Load() {
		t.Error("OnError should NOT be called on successful retry")
	}

	attempts := int(attemptCount.Load())
	if attempts != 2 {
		t.Errorf("expected 2 sendMessage attempts, got %d", attempts)
	}
}
