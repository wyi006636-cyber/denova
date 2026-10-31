package writingcomparison

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"denova/config"
	projectdomain "denova/internal/project"
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

func TestCandidateSnapshotContextRemovesToolInstructionsAndPreservesUTF8(t *testing.T) {
	contextText := candidateContextText("keep this\nUse `read` for known files.\nuse a tool to inspect more\n结尾")
	if strings.Contains(strings.ToLower(contextText), "tool") || strings.Contains(contextText, "`read`") {
		t.Fatalf("tool instruction leaked into candidate context: %q", contextText)
	}
	if !strings.Contains(contextText, "结尾") {
		t.Fatalf("candidate context lost UTF-8 content: %q", contextText)
	}
	truncated := truncateUTF8("中文正文内容", len([]byte("中文正文")))
	if !strings.Contains(truncated, "中文") || !utf8.ValidString(truncated) {
		t.Fatalf("UTF-8 truncation = %q", truncated)
	}
}

func TestFreezeWritingSnapshotBudgetFailureUsesStableCode(t *testing.T) {
	layout := testLayout(t)
	limit := 1
	cfg := config.Config{AgentContexts: config.AgentContextSettings{IDE: config.AgentContextOverride{MaxTotalInjectedBytes: &limit}}}
	_, err := freezeWritingSnapshot(context.Background(), cfg, layout, testTask(layout), NewStore(layout))
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != CodeContextBudgetExceeded {
		if typed == nil {
			t.Fatalf("budget error=%v", err)
		}
		t.Fatalf("budget error=%v code=%q", err, typed.Code)
	}
}

func TestFreezeWritingSnapshotTruncatesChineseFileOnRuneBoundary(t *testing.T) {
	layout := testLayout(t)
	path := filepath.Join(layout.ContentRoot, "chapters", "ch00001.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("中文正文内容继续"), 0o600); err != nil {
		t.Fatal(err)
	}
	limit := 2000
	fragmentLimit := 8
	cfg := config.Config{AgentContexts: config.AgentContextSettings{IDE: config.AgentContextOverride{MaxTotalInjectedBytes: &limit, MaxFragmentBytes: &fragmentLimit}}}
	task := testTask(layout)
	task.Target = TargetRef{Path: "chapters/ch00001.md", Exists: true, BaseRevision: "sha256:" + AssetSHA256([]byte("中文正文内容继续")), ChapterCursor: "ch00001"}
	snapshot, err := freezeWritingSnapshot(context.Background(), cfg, layout, task, NewStore(layout))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range snapshot.Fragments {
		if fragment.Path == task.Target.Path {
			if !fragment.Truncated || !utf8.ValidString(string(mustReadSnapshotAsset(t, layout, task.ID, fragment.AssetPath))) {
				t.Fatalf("Chinese fragment=%+v", fragment)
			}
			return
		}
	}
	t.Fatalf("target fragment missing: %+v", snapshot.Fragments)
}

func mustReadSnapshotAsset(t *testing.T, layout projectdomain.Layout, taskID, relative string) []byte {
	t.Helper()
	path, err := NewStore(layout).AssetPath(taskID, relative)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
