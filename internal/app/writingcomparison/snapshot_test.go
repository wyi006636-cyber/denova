package writingcomparison

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"denova/config"
)

func TestFreezeWritingSnapshotRecordsMissingTargetAndEmptyCursor(t *testing.T) {
	layout := testLayout(t)
	if err := os.MkdirAll(filepath.Join(layout.ContentRoot, "setting"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layout.ContentRoot, "setting", "progress.md"), []byte("progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	task := testTask(layout)
	task.Target = TargetRef{Path: "chapters/new.md", Exists: false, BaseRevision: "missing", ChapterCursor: ""}
	snapshot, err := freezeWritingSnapshot(context.Background(), config.Config{}, layout, task, NewStore(layout))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TargetRevision != "missing" || snapshot.ID == "" {
		t.Fatalf("snapshot target = %+v", snapshot)
	}
	if len(snapshot.Omitted) == 0 {
		t.Fatal("expected missing files to be recorded as omitted")
	}
	for _, item := range snapshot.Omitted {
		if item.Path == "chapters/new.md" && item.Reason == "missing" {
			return
		}
	}
	// The target file itself is part of the pack and must be explicitly absent.
	t.Fatalf("snapshot omitted records do not contain missing target: %+v", snapshot.Omitted)
}

func TestFreezeWritingSnapshotRecordsBudgetOmission(t *testing.T) {
	layout := testLayout(t)
	if err := os.MkdirAll(filepath.Join(layout.ContentRoot, "setting"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layout.ContentRoot, "setting", "progress.md"), []byte(strings.Repeat("progress ", 800)), 0o600); err != nil {
		t.Fatal(err)
	}
	limit := 2000
	fragmentLimit := 1024
	cfg := config.Config{AgentContexts: config.AgentContextSettings{IDE: config.AgentContextOverride{MaxTotalInjectedBytes: &limit, MaxFragmentBytes: &fragmentLimit}}}
	snapshot, err := freezeWritingSnapshot(context.Background(), cfg, layout, testTask(layout), NewStore(layout))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range snapshot.Omitted {
		if item.Reason == "budget" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected budget omission, got %+v", snapshot.Omitted)
	}
}
