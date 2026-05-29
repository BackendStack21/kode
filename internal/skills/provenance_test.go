package skills

import (
	"testing"
)

func msgWithTool(name string) LlmMessage {
	tc := LlmToolCall{}
	tc.Function.Name = name
	return LlmMessage{
		Role:      "assistant",
		ToolCalls: []LlmToolCall{tc},
	}
}

func TestDeriveProvenance_EmptyIsTrusted(t *testing.T) {
	prov := DeriveProvenance(nil)
	if prov.Untrusted {
		t.Errorf("empty session should be trusted, got %+v", prov)
	}
}

func TestDeriveProvenance_PureShellIsTrusted(t *testing.T) {
	msgs := []LlmMessage{msgWithTool("shell"), msgWithTool("patch")}
	prov := DeriveProvenance(msgs)
	if prov.Untrusted {
		t.Errorf("shell+patch session should be trusted, got %+v", prov)
	}
}

func TestDeriveProvenance_BrowserTaints(t *testing.T) {
	msgs := []LlmMessage{msgWithTool("shell"), msgWithTool("browser"), msgWithTool("patch")}
	prov := DeriveProvenance(msgs)
	if !prov.Untrusted {
		t.Fatalf("browser call should taint provenance, got %+v", prov)
	}
	if !prov.NeedsReview {
		t.Errorf("NeedsReview should be true when untrusted, got %+v", prov)
	}
	if len(prov.Sources) != 1 || prov.Sources[0] != "browser" {
		t.Errorf("Sources should list 'browser', got %v", prov.Sources)
	}
}

func TestDeriveProvenance_MCPAdapterTaints(t *testing.T) {
	// MCP tools follow the "<server>__<tool>" naming convention.
	msgs := []LlmMessage{msgWithTool("github__list_issues")}
	prov := DeriveProvenance(msgs)
	if !prov.Untrusted {
		t.Fatalf("MCP tool should taint provenance, got %+v", prov)
	}
	if len(prov.Sources) != 1 || prov.Sources[0] != "github__list_issues" {
		t.Errorf("Sources should list the MCP tool name, got %v", prov.Sources)
	}
}

func TestDeriveProvenance_MultipleSourcesDeduped(t *testing.T) {
	msgs := []LlmMessage{
		msgWithTool("browser"),
		msgWithTool("browser"),
		msgWithTool("read_file"),
		msgWithTool("read_file"),
	}
	prov := DeriveProvenance(msgs)
	if !prov.Untrusted {
		t.Fatalf("expected Untrusted=true, got %+v", prov)
	}
	if len(prov.Sources) != 2 {
		t.Errorf("expected 2 deduped sources, got %v", prov.Sources)
	}
}

func TestSaveSuggestion_PropagatesProvenance(t *testing.T) {
	dir := t.TempDir()
	s := SkillSuggestion{
		Name:        "test-skill",
		Description: "verifying provenance flows through SaveSuggestion",
		Body:        "## Overview\nbody\n\n## Common Pitfalls\nnone",
		Provenance:  SkillProvenance{Untrusted: true, Sources: []string{"browser"}},
	}
	if err := SaveSuggestion(dir, s); err != nil {
		t.Fatalf("SaveSuggestion: %v", err)
	}

	loaded := scanDir(dir)
	if len(loaded) != 1 {
		t.Fatalf("scanDir found %d skills, want 1", len(loaded))
	}
	got := loaded[0].Provenance
	if !got.Untrusted {
		t.Errorf("saved skill missing Untrusted=true, got %+v", got)
	}
	if !got.NeedsReview {
		t.Errorf("Untrusted skill should be saved with NeedsReview=true, got %+v", got)
	}
}
