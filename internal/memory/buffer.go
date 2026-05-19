package memory

import (
	"fmt"
	"time"
)

// Buffer is a simple ring buffer for session-level turn summaries.
// Thread-safe only if accessed from a single goroutine (the loop engine
// owns the buffer and serializes all access).
type Buffer struct {
	lines []string
	cap   int
}

// NewBuffer creates a ring buffer with the given capacity. cap=0 means
// disabled (all appends are silently discarded).
func NewBuffer(cap int) *Buffer {
	if cap < 0 {
		cap = 0
	}
	return &Buffer{
		lines: make([]string, 0, cap),
		cap:   cap,
	}
}

// Append adds a line to the buffer. If the buffer is at capacity, the
// oldest line is evicted first.
func (b *Buffer) Append(line string) {
	if b.cap <= 0 {
		return
	}
	if len(b.lines) >= b.cap {
		// Evict oldest
		b.lines = b.lines[1:]
	}
	// Sanitize: strip newlines from the line
	line = sanitizeLine(line)
	b.lines = append(b.lines, line)
}

// Lines returns a copy of the current buffer contents (oldest first).
func (b *Buffer) Lines() []string {
	out := make([]string, len(b.lines))
	copy(out, b.lines)
	return out
}

// Clear removes all lines from the buffer.
func (b *Buffer) Clear() {
	b.lines = make([]string, 0, b.cap)
}

// Cap returns the maximum number of lines.
func (b *Buffer) Cap() int { return b.cap }

// Len returns the current number of lines.
func (b *Buffer) Len() int { return len(b.lines) }

// sanitizeLine removes newlines, tabs, and trims whitespace.
func sanitizeLine(line string) string {
	// Remove newlines and tabs
	result := make([]byte, 0, len(line))
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\n' || c == '\r' || c == '\t' {
			result = append(result, ' ')
		} else {
			result = append(result, c)
		}
	}
	return fmt.Sprintf("%s", string(result))
}

// FormatBufferLine creates a timestamped buffer line.
// Format: "HH:MM  role  message"
func FormatBufferLine(role, message string) string {
	now := time.Now().UTC().Format("15:04")
	return fmt.Sprintf("%s  %s  %s", now, role, message)
}
