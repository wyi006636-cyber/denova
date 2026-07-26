package workspacechange

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPendingParentSyncKeepsUncertainPathPrivateOnLaterFailure(t *testing.T) {
	service, path := newConsistentSnapshotService(t, "same revision before the uncertain write")
	const redirectedPath = "redirect/ch01.md"
	writeTestFile(t, service.workspace, redirectedPath, "same revision before the uncertain write")
	service.durability.visibleWriteHookFn = func(stage, _ string) error {
		if stage == visibleWriteStageAfterReplace {
			swapVisibleParentForTest(t, service.workspace, "chapters.written")
		}
		return nil
	}

	_, err := service.ReplaceFileWithConsistentSnapshot(context.Background(), ReplaceFileRequest{
		Path:         path,
		Content:      "candidate written through the opened parent",
		BaseRevision: Revision([]byte("same revision before the uncertain write")),
	}, func(ChangeSet) error {
		t.Fatal("identity-uncertain write invoked the snapshot callback")
		return nil
	})
	assertDurabilityPendingWithoutPath(t, err)

	service.durability.visibleWriteHookFn = nil
	originalSync := service.durability.syncRootDirFn
	service.durability.syncRootDirFn = func(root *os.Root, rel string) error {
		if rel == "chapters" {
			return errInjectedParentSync
		}
		return originalSync(root, rel)
	}
	_, err = service.SaveFile(context.Background(), "other.md", "must remain unwritten", "missing")
	assertDurabilityPendingWithoutPath(t, err)
	if _, statErr := os.Stat(filepath.Join(service.workspace, "other.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("write barrier allowed a later mutation: %v", statErr)
	}
}

func TestPendingParentSyncDoesNotAdoptReplacementIdentity(t *testing.T) {
	t.Run("parent", func(t *testing.T) {
		service, path := newConsistentSnapshotService(t, "same revision before the uncertain write")
		const redirectedPath = "redirect/ch01.md"
		writeTestFile(t, service.workspace, redirectedPath, "same revision before the uncertain write")
		service.durability.visibleWriteHookFn = func(stage, _ string) error {
			if stage == visibleWriteStageAfterReplace {
				swapVisibleParentForTest(t, service.workspace, "chapters.written")
			}
			return nil
		}

		_, err := service.ReplaceFileWithConsistentSnapshot(context.Background(), ReplaceFileRequest{
			Path:         path,
			Content:      "candidate written through the opened parent",
			BaseRevision: Revision([]byte("same revision before the uncertain write")),
		}, func(ChangeSet) error {
			t.Fatal("identity-uncertain write invoked the snapshot callback")
			return nil
		})
		assertDurabilityPendingWithoutPath(t, err)

		service.durability.visibleWriteHookFn = nil
		_, err = service.SaveFile(context.Background(), "other.md", "must remain unwritten", "missing")
		assertDurabilityPendingWithoutPath(t, err)
		if len(service.pendingParentSync) != 1 {
			t.Fatalf("replacement parent consumed pending identity: %#v", service.pendingParentSync)
		}
		if _, statErr := os.Stat(filepath.Join(service.workspace, "other.md")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("replacement parent admitted a later write: %v", statErr)
		}
	})

	t.Run("workspace-root", func(t *testing.T) {
		const path = "chapters/ch01.md"
		service, replacement := newWorkspaceRootSwapService(t, path, "same revision before the uncertain write")
		movedWorkspace := service.workspace + ".written"
		service.durability.visibleWriteHookFn = func(stage, _ string) error {
			if stage == visibleWriteStageAfterReplace {
				swapWorkspaceRootForTest(t, service.workspace, replacement, movedWorkspace)
			}
			return nil
		}

		_, err := service.ReplaceFileWithConsistentSnapshot(context.Background(), ReplaceFileRequest{
			Path:         path,
			Content:      "candidate written through the opened workspace",
			BaseRevision: Revision([]byte("same revision before the uncertain write")),
		}, func(ChangeSet) error {
			t.Fatal("identity-uncertain write invoked the snapshot callback")
			return nil
		})
		assertDurabilityPendingWithoutPath(t, err)

		service.durability.visibleWriteHookFn = nil
		_, err = service.SaveFile(context.Background(), "other.md", "must remain unwritten", "missing")
		assertDurabilityPendingWithoutPath(t, err)
		if len(service.pendingParentSync) != 1 {
			t.Fatalf("replacement workspace consumed pending identity: %#v", service.pendingParentSync)
		}
		if _, statErr := os.Stat(filepath.Join(service.workspace, "other.md")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("replacement workspace admitted a later write: %v", statErr)
		}
	})
}

func TestPendingSaveKeepsUncertainPathPrivateAfterIdentityRecovery(t *testing.T) {
	service, path := newTestServiceWithFile(t, "draft")
	change, err := service.ReplaceFile(context.Background(), ReplaceFileRequest{
		Path: path, Content: "agent draft", BaseRevision: Revision([]byte("draft")),
		Metadata: ChangeMetadata{Origin: OriginAgent, ChangeGroupID: "uncertain-save-redo-group"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Undo(context.Background(), HistoryRequest{GroupID: change.GroupID}); err != nil {
		t.Fatal(err)
	}
	const redirectedPath = "redirect/ch01.md"
	writeTestFile(t, service.workspace, redirectedPath, "draft")
	service.durability.visibleWriteHookFn = func(stage, _ string) error {
		if stage == visibleWriteStageAfterReplace {
			swapVisibleParentForTest(t, service.workspace, "chapters.written")
		}
		return nil
	}

	_, err = service.SaveFile(context.Background(), path, "local draft", Revision([]byte("draft")))
	assertDurabilityPendingWithoutPath(t, err)
	service.durability.visibleWriteHookFn = nil
	if err := os.Rename(filepath.Join(service.workspace, "chapters"), filepath.Join(service.workspace, "redirect")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(service.workspace, "chapters.written"), filepath.Join(service.workspace, "chapters")); err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(service.workspace, ".denova", "changes", "ledger.jsonl")
	if err := os.Rename(ledgerPath, ledgerPath+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(ledgerPath, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err = service.SaveFile(context.Background(), path, "local draft", Revision([]byte("draft")))
	assertDurabilityPendingWithoutPath(t, err)
	if !strings.Contains(err.Error(), "ledger") {
		t.Fatalf("recovery did not reach redo finalization: %v", err)
	}
	if len(service.pendingParentSync) != 0 {
		t.Fatalf("restored identity did not complete parent sync: %#v", service.pendingParentSync)
	}
	if _, ok := service.pendingSaves[path]; !ok {
		t.Fatal("redo finalization failure discarded pending save truth")
	}
	if got := readTestFile(t, service.workspace, path); got != "local draft" {
		t.Fatalf("restored target bytes = %q", got)
	}
}

func TestReplaceFileWithConsistentSnapshotRejectsWorkspaceRootSwapBeforeRename(t *testing.T) {
	for _, path := range []string{"root-story.md", "chapters/ch01.md"} {
		t.Run(path, func(t *testing.T) {
			service, replacement := newWorkspaceRootSwapService(t, path, "same revision before the root swap")
			movedWorkspace := service.workspace + ".original"
			service.durability.visibleWriteHookFn = func(stage, _ string) error {
				if stage == visibleWriteStageBeforeReplace {
					swapWorkspaceRootForTest(t, service.workspace, replacement, movedWorkspace)
				}
				return nil
			}

			callbackCalled := false
			change, err := service.ReplaceFileWithConsistentSnapshot(context.Background(), ReplaceFileRequest{
				Path:         path,
				Content:      "candidate must remain unwritten",
				BaseRevision: Revision([]byte("same revision before the root swap")),
			}, func(ChangeSet) error {
				callbackCalled = true
				return nil
			})
			assertChangeErrorCode(t, err, ErrorCodeConflict)
			if callbackCalled {
				t.Fatal("pre-replacement workspace root swap invoked the snapshot callback")
			}
			if change.ID == "" || change.Path != path {
				t.Fatalf("workspace root swap lost prepared change identity: %#v", change)
			}
			if got := readTestFile(t, movedWorkspace, path); got != "same revision before the root swap" {
				t.Fatalf("opened workspace changed: %q", got)
			}
			if got := readTestFile(t, service.workspace, path); got != "same revision before the root swap" {
				t.Fatalf("replacement workspace changed: %q", got)
			}
		})
	}
}

func TestReplaceFileWithConsistentSnapshotReportsPendingWhenWorkspaceRootChangesAfterRename(t *testing.T) {
	for _, path := range []string{"root-story.md", "chapters/ch01.md"} {
		t.Run(path, func(t *testing.T) {
			service, replacement := newWorkspaceRootSwapService(t, path, "same revision before the root swap")
			movedWorkspace := service.workspace + ".written"
			service.durability.visibleWriteHookFn = func(stage, _ string) error {
				if stage == visibleWriteStageAfterReplace {
					swapWorkspaceRootForTest(t, service.workspace, replacement, movedWorkspace)
				}
				return nil
			}

			callbackCalled := false
			change, err := service.ReplaceFileWithConsistentSnapshot(context.Background(), ReplaceFileRequest{
				Path:         path,
				Content:      "candidate written through the opened workspace",
				BaseRevision: Revision([]byte("same revision before the root swap")),
			}, func(ChangeSet) error {
				callbackCalled = true
				return nil
			})
			assertDurabilityPendingWithoutPath(t, err)
			if callbackCalled {
				t.Fatal("post-replacement workspace root swap invoked the snapshot callback")
			}
			if change.ID == "" || change.Path != path || change.Revision != Revision([]byte("candidate written through the opened workspace")) {
				t.Fatalf("workspace root swap lost visible change identity: %#v", change)
			}
			if got := readTestFile(t, movedWorkspace, path); got != "candidate written through the opened workspace" {
				t.Fatalf("opened workspace bytes = %q", got)
			}
			if got := readTestFile(t, service.workspace, path); got != "same revision before the root swap" {
				t.Fatalf("replacement workspace changed: %q", got)
			}
		})
	}
}

func assertDurabilityPendingWithoutPath(t *testing.T, err error) {
	t.Helper()
	assertDurabilityPending(t, err, true)
	var pending *Error
	if !errors.As(err, &pending) {
		t.Fatalf("pending error type = %T", err)
	}
	if _, claimed := pending.Details["path"]; claimed {
		t.Fatalf("identity-uncertain write claimed an exact target: %#v", pending.Details)
	}
	if pending.Details["recovery_pending"] != true {
		t.Fatalf("identity-uncertain write lost recovery truth: %#v", pending.Details)
	}
}

func newWorkspaceRootSwapService(t *testing.T, path, content string) (*Service, string) {
	t.Helper()
	container := t.TempDir()
	workspace := filepath.Join(container, "workspace")
	replacement := filepath.Join(container, "replacement")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(replacement, 0o755); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveFile(context.Background(), path, content, "missing"); err != nil {
		t.Fatalf("seed opened workspace: %v", err)
	}
	writeTestFile(t, replacement, path, content)
	return service, replacement
}

func swapWorkspaceRootForTest(t *testing.T, workspace, replacement, movedWorkspace string) {
	t.Helper()
	if err := os.Rename(workspace, movedWorkspace); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, workspace); err != nil {
		t.Fatal(err)
	}
}
