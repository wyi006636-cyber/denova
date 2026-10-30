package writingcomparison

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	projectdomain "denova/internal/project"
)

func testLayout(t *testing.T) projectdomain.Layout {
	t.Helper()
	root := t.TempDir()
	content := filepath.Join(root, "book")
	store := filepath.Join(root, "store")
	if err := os.MkdirAll(content, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	return projectdomain.Layout{ProjectID: "book-1", Type: projectdomain.TypeBook, ContentRoot: content, StoreRoot: store}
}

func testTask(layout projectdomain.Layout) Task {
	return Task{ID: "comparison-1", ProjectID: layout.ProjectID, Target: TargetRef{Path: "chapters/ch00001.md", BaseRevision: "missing"}, ChapterTask: ChapterTask{Text: "Write the opening", Source: "author"}, Phase: PhaseConfiguring, Execution: ExecutionIdle, Candidates: []Candidate{
		{ID: "candidate-1", Position: 1, Status: CandidatePending, Attempt: 1},
		{ID: "candidate-2", Position: 2, Status: CandidatePending, Attempt: 1},
	}}
}

func TestStoreAppendOrderCASAndIndependentHandles(t *testing.T) {
	ctx := context.Background()
	layout := testLayout(t)
	store := NewStore(layout)
	created, err := store.Create(ctx, testTask(layout))
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 {
		t.Fatalf("created revision = %d, want 1", created.Revision)
	}
	h1, err := store.Open(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer h1.Close()
	h2, err := store.Open(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Close()
	phaseEvent := Event{Kind: EventPhaseChanged, FromPhase: PhaseConfiguring, ToPhase: PhaseGenerating, Reason: "run"}
	updated, err := h1.Append(ctx, 1, phaseEvent)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.Phase != PhaseGenerating {
		t.Fatalf("unexpected update: %+v", updated)
	}
	if _, err := h2.Append(ctx, 1, phaseEvent); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale append error = %v, want revision conflict", err)
	}
	// Reopening through a new handle observes the canonical journal, not a
	// stale in-memory task projection.
	reopened, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Revision != 2 || reopened.Phase != PhaseGenerating {
		t.Fatalf("reopened task = %+v", reopened)
	}
	entries, err := os.ReadDir(filepath.Join(layout.ComparisonsDir(), created.ID))
	if err != nil {
		t.Fatal(err)
	}
	seenJournal := false
	for _, entry := range entries {
		if entry.Name() == journalFilename {
			seenJournal = true
		}
	}
	if !seenJournal {
		t.Fatal("journal.jsonl was not created")
	}
}

func TestStoreListProjection(t *testing.T) {
	layout := testLayout(t)
	store := NewStore(layout)
	if _, err := store.List(context.Background(), ListOptions{}); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 2; index++ {
		task := testTask(layout)
		task.ID = "comparison-" + string(rune('0'+index))
		if _, err := store.Create(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.List(context.Background(), ListOptions{IncludeApplied: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].CandidateCount != 2 {
		t.Fatalf("list projection = %+v", items)
	}
}
