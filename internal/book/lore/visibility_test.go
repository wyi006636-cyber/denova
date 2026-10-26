package lore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVisibleItemsFiltersChapterWindowsAndHidesFutureOnlyItems(t *testing.T) {
	items := []Item{
		{ID: "stable", Name: "Stable", Content: "always", Spans: []Span{{ID: "future", From: "ch00030", Content: "future"}}},
		{ID: "windows", Name: "Windows", Spans: []Span{{ID: "before", Until: "ch00030", Content: "before"}, {ID: "after", From: "ch00030", Content: "after"}}},
	}
	if got := VisibleItems(items, "ch00020", LoreScopeAsOf); len(got) != 2 || len(got[0].Spans) != 0 || got[1].Spans[0].Content != "before" {
		t.Fatalf("ch00020 projection = %#v", got)
	}
	if got := VisibleItems(items, "ch00030", LoreScopeAsOf); len(got) != 2 || got[1].Spans[0].Content != "after" {
		t.Fatalf("ch00030 projection = %#v", got)
	}
	if got := VisibleItems(items, "", LoreScopeAsOf); len(got) != 1 || got[0].ID != "stable" || len(got[0].Spans) != 0 {
		t.Fatalf("no-cursor projection = %#v", got)
	}
	if got := VisibleItems(items, "", LoreScopeBook); len(got) != 2 || len(got[1].Spans) != 2 {
		t.Fatalf("book projection = %#v", got)
	}
}

func TestVisibleSpansMarkdownDoesNotTruncate(t *testing.T) {
	items := []Item{{ID: "secret", Name: "Secret", Spans: []Span{{ID: "s", From: "ch00030", Content: "secret body"}}}}
	if got, err := VisibleSpansMarkdown(items, "ch00030", LoreScopeAsOf, 64); err == nil || got != "" {
		t.Fatalf("expected complete-context overflow, got %q err=%v", got, err)
	}
	got, err := VisibleSpansMarkdown(items, "ch00030", LoreScopeAsOf, 1024)
	if err != nil || !strings.Contains(got, "secret body") {
		t.Fatalf("visible span markdown = %q err=%v", got, err)
	}
}

func TestSpanValidationRejectsInvalidUpdatesWithoutChangingFile(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	item, err := store.Create(ItemInput{Name: "Hero", Content: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	path := ItemsPath(workspace)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(item.ID, ItemInput{Name: item.Name, Type: item.Type, Content: item.Content, Spans: []Span{{ID: "bad", From: "ch30", Content: "bad"}}})
	if err == nil || !strings.Contains(err.Error(), "invalid from chapter prefix") {
		t.Fatalf("invalid prefix error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("invalid span update changed items.json")
	}
}

func TestUpdatingContentWithoutSpansPreservesSpansAndEmptyArrayClears(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	item, err := store.Create(ItemInput{Name: "Hero", Content: "stable", Spans: []Span{{ID: "later", From: "ch00020", Content: "later"}}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(item.ID, ItemInput{Name: item.Name, Type: item.Type, Content: "changed"})
	if err != nil || len(updated.Spans) != 1 || updated.Spans[0].Content != "later" {
		t.Fatalf("content-only update = %#v err=%v", updated, err)
	}
	cleared, err := store.Update(item.ID, ItemInput{Name: item.Name, Type: item.Type, Content: "changed", Spans: []Span{}})
	if err != nil || len(cleared.Spans) != 0 {
		t.Fatalf("explicit empty spans update = %#v err=%v", cleared, err)
	}
}

func TestVersion3ReadAndSaveMigratesSpansAndCreatesBackup(t *testing.T) {
	workspace := t.TempDir()
	path := ItemsPath(workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := map[string]any{
		"version":    3,
		"categories": DefaultCategories(),
		"items":      []Item{{ID: "hero", Name: "Hero", Type: "character", Content: "body", Spans: []Span{{ID: "legacy", From: "ch00020", Content: "must be ignored"}}}},
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewStore(workspace).Ensure(); err != nil {
		t.Fatal(err)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Version int    `json:"version"`
		Items   []Item `json:"items"`
	}
	if err := json.Unmarshal(persisted, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Version != loreItemsVersion || len(envelope.Items) != 1 || envelope.Items[0].Content != "body" || len(envelope.Items[0].Spans) != 0 {
		t.Fatalf("migrated envelope = %#v", envelope)
	}
	backups, err := filepath.Glob(filepath.Join(workspace, "setting", "lore", "backups", "categories-migration-*.json"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("migration backups = %v err=%v", backups, err)
	}
}

func TestStableLoreIndexAndRevisionIgnoreChapterSpans(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	item, err := store.Create(ItemInput{
		ID: "hero", Name: "Hero", Content: "stable canon", LoadMode: LoadModeResident,
		Spans: []Span{{ID: "later", From: "ch00030", Content: "later canon"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	indexBefore, err := store.ProgressiveContextMarkdown()
	if err != nil {
		t.Fatal(err)
	}
	revisionBefore, err := store.Revision()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(indexBefore, "later canon") || !strings.Contains(indexBefore, "stable canon") {
		t.Fatalf("stable index = %q", indexBefore)
	}
	item.Spans[0].Content = "updated later canon"
	if _, err := store.Update(item.ID, ItemInput{Name: item.Name, Type: item.Type, Content: item.Content, LoadMode: item.LoadMode, Spans: item.Spans}); err != nil {
		t.Fatal(err)
	}
	indexAfter, err := store.ProgressiveContextMarkdown()
	if err != nil {
		t.Fatal(err)
	}
	revisionAfter, err := store.Revision()
	if err != nil {
		t.Fatal(err)
	}
	if indexBefore != indexAfter || revisionBefore != revisionAfter {
		t.Fatalf("chapter span changed stable projection: before index/revision=%q/%q after=%q/%q", indexBefore, revisionBefore, indexAfter, revisionAfter)
	}
}
