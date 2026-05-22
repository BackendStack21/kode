package telegram

import (
	"strings"
	"testing"
)

// TestSplitChunks_CodeBlockIntact verifies that a code block smaller than
// maxBytes stays in one chunk — \n\n inside the block doesn't trigger a split.
func TestSplitChunks_CodeBlockIntact(t *testing.T) {
	// Code block with internal \n\n — should NOT be split
	before := strings.Repeat("x", 3500)
	code := "\n\n```\nsome code\n\ninside block\n```"
	after := "\n\nfinal text"

	input := before + code + after

	got := splitChunks(input, 4096)

	// The code block must appear BALANCED (even number of ```) in one chunk.
	// Since the code block fits within 4096, it should be entirely in one chunk.
	foundComplete := false
	for _, chunk := range got {
		opens := strings.Count(chunk, "```")
		if opens == 2 {
			// Both open and close in same chunk
			foundComplete = true
			if !strings.Contains(chunk, "some code") {
				t.Error("code block appears intact but content missing")
			}
		}
		if opens == 1 {
			t.Errorf("unbalanced ``` in chunk (len=%d): %q", len(chunk), chunk[:min(80, len(chunk))])
		}
	}

	if !foundComplete {
		t.Error("no chunk contains the complete code block")
	}

	// All content must be present
	combined := strings.Join(got, "")
	if !strings.Contains(combined, "some code") {
		t.Error("lost code block content")
	}
	if !strings.Contains(combined, "final text") {
		t.Error("lost after-text content")
	}
}

// TestSplitChunks_MultipleCodeBlocks verifies that multiple code blocks
// are each kept intact during chunking.
func TestSplitChunks_MultipleCodeBlocks(t *testing.T) {
	before := strings.Repeat("a", 3000)
	code1 := "\n\n```go\nfunc main() {}\n```"
	mid := "\n\n" + strings.Repeat("b", 1000)
	code2 := "\n\n```json\n{\"key\": \"val\"}\n```"

	input := before + code1 + mid + code2

	got := splitChunks(input, 4096)

	for _, chunk := range got {
		opens := strings.Count(chunk, "```")
		if opens%2 != 0 {
			t.Errorf("unbalanced ``` in chunk: %q", chunk[:min(200, len(chunk))])
		}
	}

	combined := strings.Join(got, "")
	if !strings.Contains(combined, "func main()") {
		t.Error("lost code1 content")
	}
	if !strings.Contains(combined, `"key": "val"`) {
		t.Error("lost code2 content")
	}
}

// TestSplitChunks_InlineCodeUnaffected verifies that inline `code` spans
// (single backtick) do NOT affect chunk splitting — only fenced blocks.
func TestSplitChunks_InlineCodeUnaffected(t *testing.T) {
	before := strings.Repeat("a", 3000)
	mid := "\n\nUse `code` here."
	after := "\n\n" + strings.Repeat("b", 2000)

	input := before + mid + after

	got := splitChunks(input, 4096)

	if len(got) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(got))
	}

	combined := strings.Join(got, "")
	if !strings.Contains(combined, "`code`") {
		t.Error("lost inline code content")
	}
}
