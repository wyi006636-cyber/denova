package book

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWritingContextPackPrioritizesTargetAndPreviousChapter(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "chapters"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ch00001.md", "ch00002.md", "ch00003.md"} {
		if err := os.WriteFile(filepath.Join(root, "chapters", name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state := NewState(root)
	paths := WritingContextPackPaths(state, "chapters/ch00003.md")
	if len(paths) < 3 || paths[0] != "chapters/ch00003.md" || paths[1] != "chapters/ch00002.md" {
		t.Fatalf("target priority paths=%v", paths)
	}
	caseIndex, targetIndex := -1, -1
	for index, path := range paths {
		if path == "setting/case-index.md" {
			caseIndex = index
		}
		if path == "chapters/ch00003.md" {
			targetIndex = index
		}
	}
	if targetIndex < 0 || caseIndex < targetIndex {
		t.Fatalf("case index outranks target: paths=%v", paths)
	}
}
