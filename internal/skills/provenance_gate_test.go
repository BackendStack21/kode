package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSkillFile writes a SKILL.md under dir/<name>/SKILL.md.
func writeSkillFile(t *testing.T, dir, name, frontmatter, body string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\n" + frontmatter + "---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestScanDirs_NeedsReviewSkillsLanIn Lazy verifies the provenance gate:
// a skill with auto_load=true but needs_review=true must NOT appear in
// AutoLoad. This is what stops a poisoned auto-saved skill from
// activating on the next session.
func TestScanDirs_NeedsReviewSkillsLandInLazy(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "clean-skill",
		"name: clean-skill\ndescription: clean\nodek:\n  auto_load: true\n",
		"## Overview\nclean body\n## Common Pitfalls\nnone\n")
	writeSkillFile(t, dir, "tainted-skill",
		"name: tainted-skill\ndescription: tainted\nodek:\n  auto_load: true\n  provenance:\n    untrusted: true\n    needs_review: true\n",
		"## Overview\ntainted body\n## Common Pitfalls\nnone\n")

	res := ScanDirs("", dir, nil)
	if res == nil {
		t.Fatal("ScanDirs returned nil")
	}

	var sawCleanAuto, sawTaintedAuto, sawTaintedLazy bool
	for _, s := range res.AutoLoad {
		if s.Name == "clean-skill" {
			sawCleanAuto = true
		}
		if s.Name == "tainted-skill" {
			sawTaintedAuto = true
		}
	}
	for _, s := range res.Lazy {
		if s.Name == "tainted-skill" {
			sawTaintedLazy = true
		}
	}
	if !sawCleanAuto {
		t.Error("clean skill missing from AutoLoad")
	}
	if sawTaintedAuto {
		t.Error("tainted needs-review skill was placed in AutoLoad — provenance gate failed")
	}
	if !sawTaintedLazy {
		t.Error("tainted needs-review skill missing from Lazy fallback")
	}
}

// TestScanDirs_PromotedSkillLandsInAutoLoad confirms the inverse: a
// skill that had Untrusted=true but the user cleared NeedsReview (via
// `odek skill promote`) IS auto-loaded.
func TestScanDirs_PromotedSkillLandsInAutoLoad(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "promoted-skill",
		"name: promoted-skill\ndescription: promoted\nodek:\n  auto_load: true\n  provenance:\n    sources: browser\n",
		"## Overview\nbody\n## Common Pitfalls\nnone\n")

	res := ScanDirs("", dir, nil)
	if res == nil {
		t.Fatal("ScanDirs returned nil")
	}
	var sawAuto bool
	for _, s := range res.AutoLoad {
		if s.Name == "promoted-skill" {
			sawAuto = true
			break
		}
	}
	if !sawAuto {
		t.Error("promoted (NeedsReview=false) skill missing from AutoLoad")
	}
}
