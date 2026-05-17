// Package render provides colored terminal rendering for the kode agent loop.
//
// It produces structured output for each phase of the ReAct cycle:
// thinking, tool calls, tool results, and the final answer. When a Renderer
// is nil or disabled, no output is produced — this keeps the programmatic API
// silent and the CLI colorful.
//
// # Design
//
//   - Zero dependencies. Uses ANSI escape codes directly.
//   - Color detection: respects NO_COLOR env var and tty detection.
//   - Truncation: tool output is truncated to prevent flooding the terminal.
//   - Maintainable: each rendering method is self-contained; adding a new
//     event type requires one constant + one method.
package render

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ── Events ────────────────────────────────────────────────────────────

// Event identifies a point in the agent loop lifecycle.
// Programmatic consumers can type-switch on Event values.
type Event int

const (
	// IterStart marks the beginning of an iteration cycle.
	IterStart Event = iota
	// Thinking is the model's reasoning text before tool calls.
	Thinking
	// ToolCall is a tool invocation requested by the model.
	ToolCall
	// ToolResult is the output from a completed tool call.
	ToolResult
	// FinalAnswer is the model's final response (no tool calls).
	FinalAnswer
	// Error is a non-fatal error during the loop.
	Error
)

func (e Event) String() string {
	switch e {
	case IterStart:
		return "iter"
	case Thinking:
		return "thinking"
	case ToolCall:
		return "tool_call"
	case ToolResult:
		return "tool_result"
	case FinalAnswer:
		return "answer"
	case Error:
		return "error"
	default:
		return "unknown"
	}
}

// ── ANSI Styles ───────────────────────────────────────────────────────

// Style constants. Use the method form (e.g., r.dim(...)) so we can
// skip codes when color is disabled.
const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	dim     = "\033[2m"
	italic  = "\033[3m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	gray    = "\033[90m"
)

// ── Renderer ──────────────────────────────────────────────────────────

// MaxToolOutput is the maximum number of characters to print from tool results.
// Output longer than this is truncated with an ellipsis.
const MaxToolOutput = 2000

// Renderer writes formatted agent loop output to an io.Writer.
// The zero value is usable but won't produce any output — call New()
// to create a properly initialized Renderer.
type Renderer struct {
	w     io.Writer
	color bool
	model string
	iter  int // current iteration number, set by Iteration()
	maxN  int // max iterations, set by Iteration()
}

// New creates a Renderer that writes to w. If color is false, ANSI escape
// codes are stripped from the output.
func New(w io.Writer, color bool) *Renderer {
	return &Renderer{
		w:     w,
		color: color,
	}
}

// WithModel sets the model name displayed in iteration headers.
func (r *Renderer) WithModel(name string) *Renderer {
	r.model = name
	return r
}

// disable returns true when the renderer should produce no output.
func (r *Renderer) disable() bool {
	if r == nil {
		return true
	}
	return r.w == nil
}

// ── Rendering methods ─────────────────────────────────────────────────

// Iteration prints the cycle header: "━━ iter 3/90 · deepseek-chat ━━".
func (r *Renderer) Iteration(n, maxN int) {
	if r.disable() {
		return
	}
	r.iter, r.maxN = n, maxN
	// Build prefix: iter N/M · model
	var prefix string
	if r.model != "" {
		prefix = fmt.Sprintf("iter %d/%d · %s", n, maxN, r.model)
	} else {
		prefix = fmt.Sprintf("iter %d/%d", n, maxN)
	}

	// Horizontal rule framing
	bar := strings.Repeat("━", 4)
	line := fmt.Sprintf("%s %s %s", bar, prefix, bar)
	fmt.Fprintln(r.w)
	fmt.Fprintln(r.w, r.style(bold+blue, line))
}

// Thinking prints the model's reasoning text in a dimmed, italic style.
// This is the "thinking aloud" before the model decides on tool calls.
func (r *Renderer) Thinking(text string) {
	if r.disable() || text == "" {
		return
	}
	r.label("thinking", dim+italic, text)
}

// ToolCall prints a tool invocation with name and arguments.
// Arguments are JSON — we pretty-print the name and show a compact arg summary.
func (r *Renderer) ToolCall(name, args string) {
	if r.disable() {
		return
	}
	label := r.style(cyan+bold, "⚙ "+name)
	fmt.Fprintf(r.w, "%s %s\n", label, r.style(gray, r.truncate(args, 120)))
}

// ToolResult prints the output from a tool call.
// Long output is truncated to MaxToolOutput characters.
func (r *Renderer) ToolResult(output string) {
	if r.disable() || output == "" {
		return
	}
	r.label("result", green, output)
}

// FinalAnswer prints the model's concluding response.
func (r *Renderer) FinalAnswer(text string) {
	if r.disable() || text == "" {
		return
	}
	fmt.Fprintln(r.w)
	r.label("answer", bold, text)
	fmt.Fprintln(r.w)
}

// Error prints a non-fatal loop error.
func (r *Renderer) Error(err error) {
	if r.disable() || err == nil {
		return
	}
	msg := r.style(red, "error: "+err.Error())
	fmt.Fprintln(r.w, msg)
}

// ── Helpers ───────────────────────────────────────────────────────────

// label prints a labeled block: "─label──" followed by indented text.
func (r *Renderer) label(name, style, text string) {
	header := r.style(style, "── "+name+" ──")
	fmt.Fprintln(r.w, header)
	// Indent each line of text
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintln(r.w, " "+r.truncate(line, MaxToolOutput))
	}
}

// style wraps text in ANSI codes. Returns plain text when color is off.
func (r *Renderer) style(code, text string) string {
	if !r.color {
		return text
	}
	return code + text + reset
}

// truncate limits s to n chars, adding "…" if truncated.
func (r *Renderer) truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ── Auto-detection ────────────────────────────────────────────────────

// ColorEnabled returns true when the terminal supports ANSI colors and
// the user hasn't set NO_COLOR.
func ColorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	// Terminals that aren't character devices (pipes, redirects) get no color.
	return (fi.Mode() & os.ModeCharDevice) != 0
}
