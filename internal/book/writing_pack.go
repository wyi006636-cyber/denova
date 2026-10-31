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
	paths := []string{"ideas.md", "setting/outline.md", "setting/progress.md", "setting/character-states.md", "setting/story-ledger.md", "setting/case-index.md", "setting/lore/items.json"}
	if targetPath = filepath.ToSlash(strings.TrimSpace(targetPath)); targetPath != "" {
		paths = append(paths, targetPath)
	}
	entries, err := os.ReadDir(filepath.Join(state.Workspace(), "chapters"))
	if err == nil {
		chapters := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			chapters = append(chapters, filepath.ToSlash(filepath.Join("chapters", entry.Name())))
		}
		sort.Strings(chapters)
		if len(chapters) > 12 {
			chapters = chapters[len(chapters)-12:]
		}
		paths = append(paths, chapters...)
	}
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

// WritingContextPackPaths is also available as a State method for callers
// that already own a book state.
func (s *State) WritingContextPackPaths(targetPath string) []string {
	return WritingContextPackPaths(s, targetPath)
}
