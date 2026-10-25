package lore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHealthFindsConflictsEmptyBrokenAndUnusedLore(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	if _, err := store.Create(ItemInput{Name: "Hero", Keywords: []string{"主角"}, Content: "人物正文"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ItemInput{Name: "Village", Keywords: []string{"Hero"}, Content: "# Village\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ItemInput{Name: "Unused", Content: "设定"}); err != nil {
		t.Fatal(err)
	}
	chapter := filepath.Join(workspace, "chapters", "001.md")
	if err := os.MkdirAll(filepath.Dir(chapter), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chapter, []byte("# 第一章\n[[Hero]]\n[[Missing]]"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := store.Health(10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.DuplicateNames) != 1 || len(report.EmptyItems) != 1 || len(report.BrokenReferences) != 1 || len(report.UnusedItems) != 2 {
		t.Fatalf("unexpected health report: %#v", report)
	}
	if duplicate := report.DuplicateNames[0]; duplicate.Name != "Village" || duplicate.Reference != "Hero" || duplicate.OtherName != "Hero" {
		t.Fatalf("duplicate should name the conflicting item, term, and owner: %#v", duplicate)
	}
	if report.BrokenReferences[0].Path != "chapters/001.md" || report.BrokenReferences[0].Line != 3 {
		t.Fatalf("unexpected broken reference location: %#v", report.BrokenReferences[0])
	}
}

func TestHealthBoundsChapterAndIssueResults(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	if _, err := store.Create(ItemInput{Name: "Known", Content: "body"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "chapters"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		path := filepath.Join(workspace, "chapters", string(rune('a'+i))+".md")
		if err := os.WriteFile(path, []byte("[[Missing]]"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	report, err := store.Health(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if report.ChaptersTotal != 3 || report.ChaptersScanned != 1 || !report.ResultsTruncated || len(report.BrokenReferences) != 1 {
		t.Fatalf("health bounds not applied: %#v", report)
	}
}
