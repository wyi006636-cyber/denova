package lore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReleasedCategoriesMigrateWithBackupAndRestore(t *testing.T) {
	store := NewStore(t.TempDir())
	legacy := []byte(`{"version":2,"items":[{"id":"hero","type":"character","name":"Hero","content":"Identity","tags":["主角"]},{"id":"guild","type":"faction","name":"Guild","content":"Organization"},{"id":"rule","type":"rule","name":"Rule","content":"Canon"},{"id":"template","type":"other","name":"Template","content":"Keep this text","tags":["模板"],"enabled":false}]}`)
	if err := os.MkdirAll(filepath.Dir(store.itemsPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.itemsPath(), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.Ensure(); err != nil {
			t.Fatal(err)
		}
	}
	collection, err := store.loadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(collection.Categories, DefaultCategories()) {
		t.Fatalf("unexpected categories: %+v", collection.Categories)
	}
	want := []string{"character", "faction", "world", "world"}
	for i, item := range collection.Items {
		if item.Type != want[i] {
			t.Fatalf("item %s category = %s", item.ID, item.Type)
		}
	}
	if collection.Items[3].Enabled || collection.Items[3].Content != "Keep this text" || !reflect.DeepEqual(collection.Items[3].Tags, []string{"模板"}) {
		t.Fatal("migration changed user content")
	}
	backups, err := filepath.Glob(filepath.Join(store.workspace, "setting", "lore", "backups", "categories-migration-*.json"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one retry-safe backup: %v %v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, legacy) {
		t.Fatal("backup does not preserve the released bytes", err)
	}
	// Restoring a released version follows the same conversion and preserves it.
	if err := os.WriteFile(store.itemsPath(), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(store.itemsPath())
	var persisted Collection
	if err := json.Unmarshal(raw, &persisted); err != nil || persisted.Version != loreItemsVersion {
		t.Fatalf("migration was not persisted: %s", raw)
	}
}

func TestCustomCategoryRenameMoveDeletePreservesLore(t *testing.T) {
	store := NewStore(t.TempDir())
	categories, err := store.MutateCategory(CategoryMutation{Op: "create", Name: "Abilities"})
	if err != nil {
		t.Fatal(err)
	}
	id := categories[len(categories)-1].ID
	item, err := store.Create(ItemInput{ID: "skill", Type: id, Name: "Flight", Content: "Stable ability", Tags: []string{"air"}, LoadMode: LoadModeManual})
	if err != nil {
		t.Fatal(err)
	}
	revision, _ := store.AllRevision()
	if _, err := store.MutateCategory(CategoryMutation{Op: "rename", ID: id, Name: "Techniques"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyTypeChanges(revision, []TypeChange{{ID: item.ID, Type: "world"}}); err != ErrRevisionConflict {
		t.Fatalf("stale category preview accepted: %v", err)
	}
	categories, err = store.MutateCategory(CategoryMutation{Op: "move", ID: id, Index: 1})
	if err != nil || categories[1].ID != id {
		t.Fatalf("move failed: %+v %v", categories, err)
	}
	catalog, err := store.NameCatalogMarkdown(NameCatalogOptions{})
	if err != nil || !strings.Contains(catalog, id+": Techniques") || !strings.Contains(catalog, "Flight") {
		t.Fatalf("custom category is undiscoverable: %s %v", catalog, err)
	}
	if _, err := store.MutateCategory(CategoryMutation{Op: "delete", ID: "character", DestinationID: "world"}); err == nil {
		t.Fatal("removed functional character category")
	}
	if _, err := store.MutateCategory(CategoryMutation{Op: "delete", ID: id, DestinationID: "missing"}); err == nil {
		t.Fatal("accepted missing destination")
	}
	if _, err := store.MutateCategory(CategoryMutation{Op: "delete", ID: id, DestinationID: "world"}); err != nil {
		t.Fatal(err)
	}
	saved, err := NewStore(store.workspace).ReadAny(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Type != "world" || saved.Content != item.Content || saved.Name != item.Name || saved.LoadMode != item.LoadMode || !reflect.DeepEqual(saved.Tags, item.Tags) {
		t.Fatalf("category removal changed content: %+v", saved)
	}
	backups, _ := filepath.Glob(filepath.Join(store.workspace, "setting", "lore", "backups", "categories-removal-*.json"))
	if len(backups) != 1 {
		t.Fatal("category removal has no recovery copy")
	}
}

func TestNameCatalogReportsCursorHiddenIDsWithoutLeakingNames(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Create(ItemInput{ID: "future-secret", Type: "world", Name: "Future Secret", Spans: []Span{{ID: "later", From: "ch00030", Content: "future body"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ItemInput{ID: "current-secret", Type: "world", Name: "Current Secret", Spans: []Span{{ID: "now", From: "ch00020", Content: "current body"}}}); err != nil {
		t.Fatal(err)
	}
	catalog, err := store.NameCatalogMarkdown(NameCatalogOptions{Cursor: "ch00020", Scope: LoreScopeAsOf})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(catalog, "Current Secret") || strings.Contains(catalog, "Future Secret") || strings.Contains(catalog, "future body") {
		t.Fatalf("catalog leaked hidden lore: %s", catalog)
	}
	if !strings.Contains(catalog, "1 lore item(s) are not visible at the current chapter cursor") || !strings.Contains(catalog, "future-secret") {
		t.Fatalf("catalog omitted hidden-id notice: %s", catalog)
	}
}

func TestNameCatalogCapsHiddenIDsAndKeepsVisibleEntries(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	items := make([]Item, 0, 1303)
	for index := 0; index < 1300; index++ {
		items = append(items, Item{
			ID: fmt.Sprintf("future-%04d", index), Enabled: true, Type: "world", Name: fmt.Sprintf("Future %04d", index),
			LoadMode: LoadModeAuto, Spans: []Span{{ID: "later", From: "ch00030", Content: "future body"}},
		})
	}
	for index := 0; index < 3; index++ {
		items = append(items, Item{
			ID: fmt.Sprintf("current-%02d", index), Enabled: true, Type: "world", Name: fmt.Sprintf("Current %02d", index),
			LoadMode: LoadModeAuto, Spans: []Span{{ID: "now", From: "ch00020", Content: "current body"}},
		})
	}
	if err := os.MkdirAll(filepath.Dir(store.itemsPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(Collection{Version: loreItemsVersion, Categories: DefaultCategories(), Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.itemsPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, err := store.NameCatalogMarkdown(NameCatalogOptions{Cursor: "ch00020", Scope: LoreScopeAsOf})
	if err != nil {
		t.Fatalf("large hidden catalog failed: %v", err)
	}
	for index := 0; index < 3; index++ {
		if !strings.Contains(catalog, fmt.Sprintf("Current %02d", index)) {
			t.Fatalf("visible lore item %d missing from catalog: %s", index, catalog)
		}
	}
	if !strings.Contains(catalog, "1300 lore item(s) are not visible") || !strings.Contains(catalog, "+1270 more") {
		t.Fatalf("hidden-id notice was not bounded: %s", catalog)
	}
	if strings.Contains(catalog, "Future 0000") || strings.Contains(catalog, "future body") {
		t.Fatalf("hidden lore leaked into catalog: %s", catalog)
	}
}

func TestNameCatalogTinyBudgetFallsBackToHiddenCount(t *testing.T) {
	store := NewStore(t.TempDir())
	for index := 0; index < 40; index++ {
		id := fmt.Sprintf("future-%02d-%s", index, strings.Repeat("x", 45))
		if _, err := store.Create(ItemInput{ID: id, Type: "world", Name: fmt.Sprintf("Future %02d", index), Spans: []Span{{ID: "later", From: "ch00030", Content: "future body"}}}); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := store.NameCatalogMarkdown(NameCatalogOptions{Cursor: "ch00020", Scope: LoreScopeAsOf, MaxBytes: 512})
	if err != nil {
		t.Fatalf("tiny budget should fall back to a count-only notice: %v", err)
	}
	if len(catalog) > 512 || !strings.Contains(catalog, "40 lore item(s) are hidden for this request") {
		t.Fatalf("unexpected tiny-budget catalog (%d bytes): %s", len(catalog), catalog)
	}
	if strings.Contains(catalog, "Future 00") || strings.Contains(catalog, "future body") {
		t.Fatalf("hidden lore leaked into catalog: %s", catalog)
	}
}

func TestNameCatalogExplainsMissingCursor(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Create(ItemInput{ID: "future-secret", Type: "world", Name: "Future Secret", Spans: []Span{{ID: "later", From: "ch00030", Content: "future body"}}}); err != nil {
		t.Fatal(err)
	}
	catalog, err := store.NameCatalogMarkdown(NameCatalogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(catalog, "no chapter cursor for this request") || strings.Contains(catalog, "current chapter cursor") {
		t.Fatalf("missing-cursor notice is unclear: %s", catalog)
	}
}

func TestCreateAndReclassifyAfterDefaultCategoryRemoval(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.MutateCategory(CategoryMutation{Op: "delete", ID: "world", DestinationID: "item"}); err != nil {
		t.Fatal(err)
	}
	created, err := store.Create(ItemInput{Name: "New fact", Content: "Keep body"})
	if err != nil || created.Type != "location" {
		t.Fatalf("missing default category not handled: %+v %v", created, err)
	}
	revision, _ := store.AllRevision()
	applied, err := store.ApplyTypeChanges(revision, []TypeChange{{ID: created.ID, Type: "item"}})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := store.AllRevision()
	if applied.Revision != current {
		t.Fatal("applied revision does not match current collection")
	}
	if _, err := store.MutateCategory(CategoryMutation{Op: "create", Name: "角色"}); err == nil {
		t.Fatal("accepted duplicate localized name")
	}
	if _, err := store.Create(ItemInput{Name: "Stale type", Type: "world"}); err == nil {
		t.Fatal("accepted deleted category")
	}
}
