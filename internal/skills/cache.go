package skills

import (
	"os"
	"path/filepath"
	"time"
)

// fileCache tracks the last-modified time of each known SKILL.md file.
// Used by scanDirCached to skip re-parsing files that haven't changed.
type fileCache map[string]time.Time

// scanDirsCached is the multi-directory equivalent of ScanDirs that uses
// file modification time caching to skip unchanged files. Dirs are scanned
// in project → user → extras priority order.
func scanDirsCached(projectDir, userDir string, extraDirs []string, fc fileCache, prev map[string]Skill) *ScanResult {
	var dirs []string
	if projectDir != "" {
		dirs = append(dirs, projectDir)
	}
	if userDir != "" {
		dirs = append(dirs, userDir)
	}
	dirs = append(dirs, extraDirs...)

	seen := make(map[string]bool)
	autoLoad := make([]Skill, 0, 10)
	lazy := make([]Skill, 0, 20)

	for _, dir := range dirs {
		skills := scanDirCached(dir, fc, prev)
		for _, s := range skills {
			if seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			if s.AutoLoad {
				autoLoad = append(autoLoad, s)
			} else {
				lazy = append(lazy, s)
			}
		}
	}

	return &ScanResult{AutoLoad: autoLoad, Lazy: lazy}
}

// scanDirCached reads all SKILL.md files in a skill directory, skipping
// files whose mod time has not changed since the last scan. Returns the
// parsed skills and updates the cache with current mod times.
func scanDirCached(dir string, fc fileCache, prevSkills map[string]Skill) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(dir, e.Name(), "SKILL.md")
		info, err := os.Stat(skillPath)
		if err != nil {
			// File was deleted or inaccessible — remove from cache
			delete(fc, skillPath)
			continue
		}

		currentMod := info.ModTime()
		prevMod, known := fc[skillPath]

		// If mod time is unchanged and we have a cached parse result, reuse it
		if known && currentMod.Equal(prevMod) {
			if cached, ok := prevSkills[skillPath]; ok {
				skills = append(skills, cached)
				continue
			}
		}

		// Parse and cache
		s := parseSkillFile(skillPath)
		if s == nil {
			delete(fc, skillPath)
			continue
		}
		s.Source = SkillSource{Dir: dir, Path: skillPath}
		fc[skillPath] = currentMod
		prevSkills[skillPath] = *s
		skills = append(skills, *s)
	}
	return skills
}
