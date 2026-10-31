package book

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WritingContextPackPaths returns the deterministic file set that a frozen
// comparison input may inline. It is an index only; callers still read every
// file at freeze time and record its revision and hash.
func WritingContextPackPaths(state *State, targetPath string) []string {
	if state == nil {
		return nil
	}
	targetPath = filepath.ToSlash(strings.TrimSpace(targetPath))
	paths := make([]string, 0, 24)
	// The current target and its immediate predecessor carry the most useful
	// continuity signal, so they must win before indexes when the budget is
	// tight.
	if targetPath != "" {
		paths = append(paths, targetPath)
	}
	entries, err := os.ReadDir(filepath.Join(state.Workspace(), "chapters"))
	chapters := []string{}
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			chapters = append(chapters, filepath.ToSlash(filepath.Join("chapters", entry.Name())))
		}
		sort.Strings(chapters)
		previous := ""
		for _, chapter := range chapters {
			if chapter < targetPath {
				previous = chapter
			}
		}
		if previous != "" && previous != targetPath {
			paths = append(paths, previous)
		}
	}
	paths = append(paths, "ideas.md", "setting/outline.md", "setting/progress.md", "setting/character-states.md", "setting/story-ledger.md")
	if len(chapters) > 12 {
		chapters = chapters[len(chapters)-12:]
	}
	for _, chapter := range chapters {
		if chapter != targetPath && chapter != "" && !containsPath(paths, chapter) {
			paths = append(paths, chapter)
		}
	}
	paths = append(paths, "setting/case-index.md", "setting/lore/items.json")
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	return result
}

func containsPath(paths []string, target string) bool {
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}

// WritingContextPackPaths is also available as a State method for callers
// that already own a book state.
func (s *State) WritingContextPackPaths(targetPath string) []string {
	return WritingContextPackPaths(s, targetPath)
}
