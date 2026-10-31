package writingtemplates

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func userTemplate() Template {
	return Template{Title: Localized{ZH: "用户模板", EN: "User Template"}, Summary: Localized{EN: "A user draft"}, Kind: KindTemplate, Category: []string{"draft", "continuation"}, Tasks: []string{TaskChapterDraft}, Body: "Write a complete chapter using the supplied facts."}
}

func TestCatalogListReadsEmbedWithoutMaterializingBuiltins(t *testing.T) {
	root := t.TempDir()
	catalog := NewCatalog(root)
	items, err := catalog.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("items = %d, want 5", len(items))
	}
	for _, item := range items {
		if item.Body != "" {
			t.Errorf("list item %s contains body", item.ID)
		}
	}
	files, _ := filepath.Glob(filepath.Join(catalog.dir(), "*.json"))
	if len(files) != 0 {
		t.Fatalf("List materialized files: %v", files)
	}
	item, err := catalog.Get(context.Background(), "draft.payoff-climax")
	if err != nil || item.Source != "builtin" || item.Body == "" {
		t.Fatalf("Get builtin = %+v, err=%v", item, err)
	}
	if item.BuiltinContentVersion != 0 {
		t.Fatalf("builtin content version marker = %d, want zero", item.BuiltinContentVersion)
	}
}

func TestCatalogCreateAutoIDAndUserIDBoundary(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	created, err := catalog.Create(context.Background(), userTemplate())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.ID, "user.user-template-") || !templateIDPattern.MatchString(created.ID) {
		t.Fatalf("generated ID = %q", created.ID)
	}
	if _, err := catalog.Create(context.Background(), Template{ID: "draft.bad", Title: Localized{EN: "Bad"}, Kind: KindTemplate, Category: []string{"draft", "continuation"}, Tasks: []string{TaskChapterDraft}, Body: "body"}); err == nil {
		t.Fatal("accepted non-user ID")
	}
	data, err := os.ReadFile(created.Path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if _, exists := persisted["builtin_content_version"]; exists {
		t.Fatal("derived builtin version was persisted")
	}
	created, err = catalog.Create(context.Background(), Template{Title: Localized{EN: "中文标题"}, Kind: KindTemplate, Category: []string{"draft", "continuation"}, Tasks: []string{TaskChapterDraft}, Body: "body"})
	if err != nil || !strings.HasPrefix(created.ID, "user.template-") {
		t.Fatalf("fallback ID = %+v, err=%v", created, err)
	}
}

func TestCatalogCASVersionAndMissingUpdate(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	created, err := catalog.Create(context.Background(), userTemplate())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Update(context.Background(), created.ID, userTemplate(), ""); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("missing base revision err=%v", err)
	}
	changed := userTemplate()
	changed.Body = "Changed body"
	changed.ContentVersion = created.ContentVersion
	if _, err := catalog.Update(context.Background(), created.ID, changed, created.Revision); err == nil {
		t.Fatal("accepted body change without version bump")
	}
	changed.ContentVersion = created.ContentVersion + 1
	updated, err := catalog.Update(context.Background(), created.ID, changed, created.Revision)
	if err != nil || updated.Revision == created.Revision {
		t.Fatalf("bumped update = %+v, err=%v", updated, err)
	}
	changed = userTemplate()
	changed.ContentVersion = 1
	if _, err := catalog.Update(context.Background(), created.ID, changed, updated.Revision); err == nil {
		t.Fatal("accepted content version downgrade")
	}
	if _, err := catalog.Update(context.Background(), "user.missing", userTemplate(), "sha256:nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing update err=%v", err)
	}
}

func TestCatalogConcurrentCASOneWins(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	created, err := catalog.Create(context.Background(), userTemplate())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			item := userTemplate()
			item.Title.EN = "Update"
			errs <- func() error {
				_, e := catalog.Update(context.Background(), created.ID, item, created.Revision)
				return e
			}()
		}(i)
	}
	wg.Wait()
	close(errs)
	var success, conflict int
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success/conflict=%d/%d", success, conflict)
	}
}

func TestCatalogUserOverrideAndStaleMaterializedRestore(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	builtin, ok := builtinTemplateByID("draft.payoff-climax")
	if !ok {
		t.Fatal("missing builtin")
	}
	path := filepath.Join(catalog.dir(), builtin.ID+".json")
	if err := os.MkdirAll(catalog.dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	bytes, err := canonicalTemplateBytes(builtin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	item, err := catalog.Get(context.Background(), builtin.ID)
	if err != nil || item.Source != "builtin" || item.BuiltinOverridden {
		t.Fatalf("stale copy = %+v, err=%v", item, err)
	}
	var persisted map[string]any
	if data, readErr := os.ReadFile(path); readErr == nil {
		if json.Unmarshal(data, &persisted) != nil {
			t.Fatal("stale file is not JSON")
		}
	}
	changed := builtin
	changed.Body += "\nExtra author text."
	changed.ContentVersion = 2
	changed.CreatedAt = "now"
	changed.UpdatedAt = "now"
	bytes, _ = canonicalTemplateBytes(changed)
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	item, err = catalog.Get(context.Background(), builtin.ID)
	if err != nil || item.Source != "user" || !item.BuiltinOverridden || item.BuiltinContentVersion != builtin.ContentVersion {
		t.Fatalf("override = %+v, err=%v", item, err)
	}
	if err := catalog.Delete(context.Background(), builtin.ID); err != nil {
		t.Fatal(err)
	}
	item, err = catalog.Get(context.Background(), builtin.ID)
	if err != nil || item.Source != "builtin" {
		t.Fatalf("restored builtin = %+v, err=%v", item, err)
	}
}

func TestCatalogBadUserFileIsolated(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	if err := catalog.ensureDir(nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(catalog.dir(), "user.bad.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	items, err := catalog.List(context.Background(), ListFilter{})
	if err != nil || len(items) != 6 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	found := false
	for _, item := range items {
		if item.ID == "user.bad" {
			found = item.Invalid && item.Error != ""
		}
	}
	if !found {
		t.Fatal("bad user file was not isolated as Invalid")
	}
}

func TestCatalogListFiltersAndDerivedPayload(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	checks := []struct {
		name   string
		filter ListFilter
		want   int
		ids    []string
	}{
		{name: "kind", filter: ListFilter{Kind: KindTemplate}, want: 3},
		{name: "task", filter: ListFilter{Task: TaskChapterOpening}, want: 1, ids: []string{"draft.first-chapter-hook"}},
		{name: "leaf category", filter: ListFilter{Category: "opening"}, want: 1, ids: []string{"draft.first-chapter-hook"}},
		{name: "path category", filter: ListFilter{Category: "draft.opening"}, want: 1, ids: []string{"draft.first-chapter-hook"}},
		{name: "root category", filter: ListFilter{Category: "draft"}, want: 3},
		{name: "Chinese title", filter: ListFilter{Query: "续写"}, want: 1, ids: []string{"draft.chapter-continue"}},
		{name: "English body", filter: ListFilter{Query: "foreshadowing"}, want: 2},
		{name: "invalid kind", filter: ListFilter{Kind: "unknown"}, want: 0},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			items, err := catalog.List(context.Background(), check.filter)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != check.want {
				t.Fatalf("items = %d, want %d", len(items), check.want)
			}
			for _, item := range items {
				if item.Body != "" {
					t.Errorf("list item %s includes body", item.ID)
				}
				payload, marshalErr := json.Marshal(item)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				if strings.Contains(string(payload), `"body"`) {
					t.Errorf("list item %s serializes body: %s", item.ID, payload)
				}
			}
			for index, wantID := range check.ids {
				if index >= len(items) || items[index].ID != wantID {
					t.Fatalf("items = %+v, want ID %s", items, wantID)
				}
			}
		})
	}
	items, err := catalog.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index < len(items); index++ {
		if items[index-1].Source == "user" || (items[index].Source == "builtin" && items[index-1].Source != "builtin") {
			t.Fatalf("builtins are not sorted before users: %q then %q", items[index-1].ID, items[index].ID)
		}
	}
}
