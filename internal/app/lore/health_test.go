package loreapp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	booklore "denova/internal/book/lore"
)

type healthTestHost struct{ workspace string }

func (host healthTestHost) WithLoreStore(_ context.Context, _ string, action func(*booklore.Store) error) (string, error) {
	return host.workspace, action(booklore.NewStore(host.workspace))
}

func (healthTestHost) ClassifyLoreItems(context.Context, string, []booklore.ClassificationInput) ([]booklore.ClassificationSuggestion, error) {
	return nil, nil
}

func TestHealthScansProjectChaptersThroughLoreService(t *testing.T) {
	workspace := t.TempDir()
	store := booklore.NewStore(workspace)
	if _, err := store.Create(booklore.ItemInput{Name: "Hero", Content: "Hero body"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(booklore.ItemInput{Name: "Empty", Content: "# Empty\n"}); err != nil {
		t.Fatal(err)
	}
	chapter := filepath.Join(workspace, "chapters", "001.md")
	if err := os.MkdirAll(filepath.Dir(chapter), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chapter, []byte("# One\n[[Hero]]\n[[Missing]]"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := NewService(healthTestHost{workspace: workspace}, nil)
	gotWorkspace, report, err := service.Health(context.Background(), "book", 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if gotWorkspace != workspace || report.ChaptersScanned != 1 || len(report.BrokenReferences) != 1 || len(report.EmptyItems) != 1 {
		t.Fatalf("unexpected service health result: workspace=%q report=%#v", gotWorkspace, report)
	}
}
