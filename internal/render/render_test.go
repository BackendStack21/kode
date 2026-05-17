package render

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRenderer_Iteration(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true).WithModel("deepseek-chat")

	r.Iteration(3, 90)

	out := buf.String()
	if !strings.Contains(out, "iter 3/90") {
		t.Errorf("Iteration() missing iteration info: %q", out)
	}
	if !strings.Contains(out, "deepseek-chat") {
		t.Errorf("Iteration() missing model name: %q", out)
	}
}

func TestRenderer_Iteration_NoModel(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	r.Iteration(1, 10)

	out := buf.String()
	if !strings.Contains(out, "iter 1/10") {
		t.Errorf("Iteration() missing iteration info: %q", out)
	}
}

func TestRenderer_Thinking(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	r.Thinking("Let me check the file contents first.")

	out := buf.String()
	if !strings.Contains(out, "Let me check the file contents first.") {
		t.Errorf("Thinking() missing content: %q", out)
	}
	if !strings.Contains(out, "thinking") {
		t.Errorf("Thinking() missing label: %q", out)
	}
}

func TestRenderer_Thinking_Empty(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	r.Thinking("")

	if buf.Len() != 0 {
		t.Errorf("Thinking(empty) should produce no output, got %q", buf.String())
	}
}

func TestRenderer_ToolCall(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	r.ToolCall("shell", `{"command": "ls -la"}`)

	out := buf.String()
	if !strings.Contains(out, "shell") {
		t.Errorf("ToolCall() missing tool name: %q", out)
	}
	if !strings.Contains(out, `"command"`) {
		t.Errorf("ToolCall() missing args: %q", out)
	}
}

func TestRenderer_ToolCall_TruncatedArgs(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	longArgs := strings.Repeat("x", 200)
	r.ToolCall("read", longArgs)

	out := buf.String()
	if len(out) > len(longArgs)+100 {
		t.Errorf("ToolCall() should truncate long args, got %d chars", len(out))
	}
	if !strings.Contains(out, "…") {
		t.Errorf("ToolCall() missing truncation ellipsis: %q", out)
	}
}

func TestRenderer_ToolResult(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	r.ToolResult("file1.txt\nfile2.txt\nfile3.txt")

	out := buf.String()
	if !strings.Contains(out, "file1.txt") {
		t.Errorf("ToolResult() missing output: %q", out)
	}
	// Multi-line output should include ellipsis
	if !strings.Contains(out, "…") {
		t.Errorf("ToolResult() missing ellipsis for multi-line output: %q", out)
	}
	// Should NOT contain lines beyond the first
	if strings.Contains(out, "file2.txt") {
		t.Errorf("ToolResult() should only show first line, got: %q", out)
	}
}

func TestRenderer_ToolResult_SingleLine(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	r.ToolResult("short output")

	out := buf.String()
	if !strings.Contains(out, "short output") {
		t.Errorf("ToolResult() missing output: %q", out)
	}
	// Single short line — no ellipsis
	if strings.Contains(out, "…") {
		t.Errorf("ToolResult() should not have ellipsis for short single line: %q", out)
	}
}

func TestRenderer_ToolResult_GrayColor(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	r.ToolResult("result text")

	out := buf.String()
	// Should use gray (dim), not green
	if strings.Contains(out, green) {
		t.Errorf("ToolResult() should use gray, not green: %q", out)
	}
}

func TestRenderer_ToolResult_Truncation(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	// A single line longer than 120 chars should be truncated.
	longLine := strings.Repeat("x", 200)
	r.ToolResult(longLine)

	out := buf.String()
	if strings.Count(out, "x") >= 200 {
		t.Errorf("ToolResult() should truncate long line, got %d x chars (input was %d)", strings.Count(out, "x"), 200)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("ToolResult() missing truncation ellipsis: %q", out)
	}
}

func TestRenderer_ToolResult_Empty(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	r.ToolResult("")

	if buf.Len() != 0 {
		t.Errorf("ToolResult(empty) should produce no output, got %q", buf.String())
	}
}

func TestRenderer_FinalAnswer(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	r.FinalAnswer("The answer is 42.")

	out := buf.String()
	if !strings.Contains(out, "The answer is 42.") {
		t.Errorf("FinalAnswer() missing content: %q", out)
	}
	if !strings.Contains(out, "answer") {
		t.Errorf("FinalAnswer() missing label: %q", out)
	}
}

func TestRenderer_FinalAnswer_Empty(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	r.FinalAnswer("")

	if buf.Len() != 0 {
		t.Errorf("FinalAnswer(empty) should produce no output, got %q", buf.String())
	}
}

func TestRenderer_Error(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	r.Error(errors.New("something went wrong"))

	out := buf.String()
	if !strings.Contains(out, "something went wrong") {
		t.Errorf("Error() missing message: %q", out)
	}
}

func TestRenderer_Error_Nil(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)

	r.Error(nil)

	if buf.Len() != 0 {
		t.Errorf("Error(nil) should produce no output, got %q", buf.String())
	}
}

func TestRenderer_NoColor(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, false)

	r.Iteration(1, 5)

	out := buf.String()
	if strings.Contains(out, "\033[") {
		t.Errorf("NoColor should strip ANSI codes, got: %q", out)
	}
	if !strings.Contains(out, "iter 1/5") {
		t.Errorf("NoColor should still render text: %q", out)
	}
}

func TestRenderer_NilWriter(t *testing.T) {
	r := New(nil, true)

	// None of these should panic
	r.Iteration(1, 5)
	r.Thinking("hello")
	r.ToolCall("shell", "{}")
	r.ToolResult("output")
	r.FinalAnswer("answer")
	r.Error(errors.New("err"))
}

func TestRenderer_NilRenderer(t *testing.T) {
	var r *Renderer

	// None of these should panic on nil receiver
	r.Iteration(1, 5)
	r.Thinking("hello")
	r.ToolCall("shell", "{}")
	r.ToolResult("output")
	r.FinalAnswer("answer")
	r.Error(errors.New("err"))
}

func TestRenderer_FullCycle(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true).WithModel("deepseek-chat")

	// Simulate one full iteration
	r.Iteration(1, 90)
	r.Thinking("I need to read the file to understand its contents.")
	r.ToolCall("shell", `{"command": "cat main.go"}`)
	r.ToolResult("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}")
	r.Iteration(2, 90)
	r.FinalAnswer("The file contains a simple Go program that prints 'hello'.")

	out := buf.String()

	// Verify each phase is present
	phases := []string{"iter 1/90", "thinking", "shell", "package main", "iter 2/90", "answer"}
	for _, phase := range phases {
		if !strings.Contains(strings.ToLower(out), phase) {
			t.Errorf("FullCycle missing phase %q in output:\n%s", phase, out)
		}
	}

	// Verify ANSI codes present
	if !strings.Contains(out, "\033[") {
		t.Error("FullCycle should contain ANSI color codes")
	}
}

func TestEvent_String(t *testing.T) {
	tests := []struct {
		e    Event
		want string
	}{
		{IterStart, "iter"},
		{Thinking, "thinking"},
		{ToolCall, "tool_call"},
		{ToolResult, "tool_result"},
		{FinalAnswer, "answer"},
		{Error, "error"},
		{Event(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.e.String(); got != tt.want {
			t.Errorf("Event(%d).String() = %q, want %q", tt.e, tt.want, got)
		}
	}
}
