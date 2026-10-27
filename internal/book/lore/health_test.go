package lore

import (
	"encoding/json"
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

func TestHealthFindsLoreValidityRangeProblems(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	if _, err := store.Create(ItemInput{
		Name: "Secret", Content: "", Spans: []Span{
			{ID: "bad-prefix", From: "chapter-30", Content: "bad"},
			{ID: "reversed", From: "ch00030", Until: "ch00020", Content: "reversed"},
			{ID: "missing", From: "ch00099", Content: "missing"},
		},
	}); err == nil {
		// Persistence rejects malformed ranges by design. Seed the fixture
		// directly so Health can still diagnose imported or hand-edited files.
		t.Fatal("expected malformed range persistence to fail")
	}
	path := filepath.Join(workspace, "setting", "lore", "items.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(Collection{Version: 4, Categories: DefaultCategories(), Items: []Item{{ID: "secret", Enabled: true, Type: "world", Name: "Secret", Importance: "important", Tags: []string{}, Keywords: []string{}, LoadMode: "auto", Spans: []Span{{ID: "bad-prefix", From: "chapter-30", Content: "bad"}, {ID: "reversed", From: "ch00030", Until: "ch00020", Content: "reversed"}, {ID: "missing", From: "ch00099", Content: "missing"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	data := append(raw, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	chapterDir := filepath.Join(workspace, "chapters")
	if err := os.MkdirAll(chapterDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chapterDir, "ch00020.md"), []byte("chapter"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chapterDir, "ch00020-alt.md"), []byte("chapter"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := store.Health(10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.ValidityRanges) != 5 {
		t.Fatalf("validity issues = %#v", report.ValidityRanges)
	}
	keys := map[string]bool{}
	for _, issue := range report.ValidityRanges {
		keys[issue.DetailKey] = true
	}
	for _, key := range []string{"lore.health.detail.range.invalid_prefix", "lore.health.detail.range.reversed", "lore.health.detail.range.unmatched"} {
		if !keys[key] {
			t.Fatalf("missing detail key %q in %#v", key, report.ValidityRanges)
		}
	}
	if len(report.EmptyItems) != 0 {
		t.Fatalf("an item with saved spans is not empty content: %#v", report.EmptyItems)
	}
	ambiguous := false
	for _, issue := range report.ValidityRanges {
		if issue.DetailKey == "lore.health.detail.range.ambiguous" {
			ambiguous = true
		}
	}
	if !ambiguous {
		t.Fatalf("missing ambiguous chapter-prefix issue: %#v", report.ValidityRanges)
	}
}
