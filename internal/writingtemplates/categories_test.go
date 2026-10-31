package writingtemplates

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinCategoryTreeShape(t *testing.T) {
	categories, err := builtinCategories()
	if err != nil {
		t.Fatal(err)
	}
	if len(categories) != 32 {
		t.Fatalf("categories = %d, want 32", len(categories))
	}
	roots, leaves := 0, 0
	ids := map[string]bool{}
	for _, category := range categories {
		if ids[category.ID] {
			t.Fatalf("duplicate category %s", category.ID)
		}
		ids[category.ID] = true
		if category.Parent == "" {
			roots++
		} else {
			leaves++
		}
		if category.ID == "style" {
			t.Fatal("empty voice.style leaf must not be present")
		}
	}
	if roots != 7 || leaves != 25 {
		t.Fatalf("roots/leaves = %d/%d, want 7/25", roots, leaves)
	}
	if !ids["deai"] || ids["voice.style"] {
		t.Fatal("voice category shape changed")
	}
}

func TestValidateCategoriesRejectsMalformedTrees(t *testing.T) {
	base := []Category{{ID: "draft"}, {ID: "opening", Parent: "draft"}}
	cases := []struct {
		name string
		mut  func([]Category) []Category
	}{
		{"duplicate", func(v []Category) []Category { return append(v, Category{ID: "draft"}) }},
		{"bad slug", func(v []Category) []Category { v[0].ID = "Draft"; return v }},
		{"missing parent", func(v []Category) []Category { v[1].Parent = "missing"; return v }},
		{"too deep", func(v []Category) []Category { return append(v, Category{ID: "leaf", Parent: "opening"}) }},
		{"unknown task", func(v []Category) []Category { v[1].TaskHint = "nope"; return v }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateCategories(tc.mut(append([]Category(nil), base...))); err == nil {
				t.Fatal("ValidateCategories accepted malformed tree")
			}
		})
	}
}

func TestUserCategoryOverrideMissingCategoryDoesNotBreakCatalog(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	if err := catalog.ensureDir(nil); err != nil {
		t.Fatal(err)
	}
	data := []byte(`[{"id":"draft","title":{"en-US":"Draft"},"order":1},{"id":"continuation","parent":"draft","title":{"en-US":"Continuation"},"order":1,"task_hint":"chapter_full_draft"}]`)
	if err := osWrite(filepath.Join(catalog.dir(), "categories.json"), data); err != nil {
		t.Fatal(err)
	}
	items, err := catalog.List(nil, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	invalid := 0
	for _, item := range items {
		if item.Invalid {
			invalid++
			if item.Error == "" {
				t.Errorf("invalid %s has no error", item.ID)
			}
		}
	}
	if invalid == 0 || len(items) != 5 {
		t.Fatalf("items=%d invalid=%d, want all five and some invalid", len(items), invalid)
	}
	item, err := catalog.Get(nil, "draft.chapter-continue")
	if err != nil || item.Invalid {
		t.Fatalf("valid category Get = %+v, err=%v", item, err)
	}
	item, err = catalog.Get(nil, "diagnose.consistency-review")
	if err != nil || !item.Invalid {
		t.Fatalf("missing category Get = %+v, err=%v", item, err)
	}
	if _, err := catalog.Categories(nil); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedCategoryOverrideOnlyBreaksCategoriesEndpoint(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	if err := catalog.ensureDir(nil); err != nil {
		t.Fatal(err)
	}
	if err := osWrite(filepath.Join(catalog.dir(), "categories.json"), []byte("{")); err != nil {
		t.Fatal(err)
	}
	items, err := catalog.List(nil, ListFilter{})
	if err != nil || len(items) != 5 {
		t.Fatalf("List items=%d err=%v, want five embedded templates", len(items), err)
	}
	if _, err := catalog.Get(nil, "draft.chapter-continue"); err != nil {
		t.Fatalf("Get should fall back to builtin categories: %v", err)
	}
	if _, err := catalog.Categories(nil); err == nil {
		t.Fatal("Categories accepted malformed replacement")
	}
}

func osWrite(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }
