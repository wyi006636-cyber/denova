package writingcomparison

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"denova/config"
	"denova/internal/book"
	"denova/internal/book/lore"
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

// frozenCandidateForbidden matches tool and interactive onboarding wording
// that a read-only, tool-less candidate must never receive.
var frozenCandidateForbidden = regexp.MustCompile("`read`|`glob`|`grep`|query_lore_items|(?s:confirm.*with the author)|update CREATOR\\.md|generate setting/outline\\.md|use a tool|with Lore tools")

func frozenSnapshotText(t *testing.T, layout projectdomain.Layout, taskID string, snapshot *SnapshotRef) string {
	t.Helper()
	var builder strings.Builder
	for _, fragment := range snapshot.Fragments {
		builder.Write(mustReadSnapshotAsset(t, layout, taskID, fragment.AssetPath))
		builder.WriteString("\n")
	}
	return builder.String()
}

func TestFrozenCandidateContextHasNoToolInstructionsAndKeepsLoreBodies(t *testing.T) {
	layout := testLayout(t)
	files := map[string]string{
		"ideas.md":                    "A revenge story on a flooded planet.",
		"setting/outline.md":          "outline body",
		"setting/progress.md":         "progress body",
		"chapters/ch00001-opening.md": "opening prose",
	}
	for relative, content := range files {
		path := filepath.Join(layout.ContentRoot, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store := lore.NewStore(layout.ContentRoot)
	if _, err := store.Create(lore.ItemInput{ID: "smithy", Type: "location", Name: "Smithy", LoadMode: lore.LoadModeResident, Content: "The smith keeps every tool polished.\nThe forge opens at dawn."}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(lore.ItemInput{ID: "archive", Type: "location", Name: "Hidden Archive", LoadMode: lore.LoadModeManual, BriefDescription: "Sealed records", Content: "on-demand body"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(lore.ItemInput{ID: "seal", Type: "item", Name: "Old Seal", Content: "base", Spans: []lore.Span{{ID: "broken", From: "ch00001", Content: "The seal cracked; every tool failed to open it."}}}); err != nil {
		t.Fatal(err)
	}
	task := testTask(layout)
	task.Target = TargetRef{Path: "chapters/ch00002.md", BaseRevision: "missing", ChapterCursor: "ch00002"}
	snapshot, err := freezeWritingSnapshot(context.Background(), config.Config{}, layout, task, NewStore(layout))
	if err != nil {
		t.Fatal(err)
	}
	frozen := frozenSnapshotText(t, layout, task.ID, snapshot)
	if match := frozenCandidateForbidden.FindString(frozen); match != "" {
		t.Fatalf("frozen candidate context contains %q:\n%s", match, frozen)
	}
	for _, required := range []string{
		"The smith keeps every tool polished.\nThe forge opens at dawn.",
		"The seal cracked; every tool failed to open it.",
		"Hidden Archive",
		"No file or Lore tools are available.",
		"outline body",
		"opening prose",
	} {
		if !strings.Contains(frozen, required) {
			t.Fatalf("frozen candidate context lost %q:\n%s", required, frozen)
		}
	}
}

func TestFrozenCandidateContextForNewWorkIsReadOnly(t *testing.T) {
	layout := testLayout(t)
	if err := book.NewState(layout.ContentRoot).InitWorkspace(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := freezeWritingSnapshot(context.Background(), config.Config{}, layout, testTask(layout), NewStore(layout))
	if err != nil {
		t.Fatal(err)
	}
	frozen := frozenSnapshotText(t, layout, testTask(layout).ID, snapshot)
	if match := frozenCandidateForbidden.FindString(frozen); match != "" {
		t.Fatalf("new-work frozen context contains %q:\n%s", match, frozen)
	}
	if !strings.Contains(frozen, "Do not ask the author questions") {
		t.Fatalf("new-work frozen context is missing the read-only notice:\n%s", frozen)
	}
}

func TestTruncateUTF8KeepsRuneBoundary(t *testing.T) {
	truncated := truncateUTF8("中文正文内容", len([]byte("中文正文")))
	if truncated != "中文正文" || !utf8.ValidString(truncated) {
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
