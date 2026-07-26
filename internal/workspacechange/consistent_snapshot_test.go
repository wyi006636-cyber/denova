package workspacechange

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestReplaceFileWithConsistentSnapshotRejectsStaleBaseBeforeCallback(t *testing.T) {
	service, path := newConsistentSnapshotService(t, "before")
	callbackCalled := false

	_, err := service.ReplaceFileWithConsistentSnapshot(context.Background(), ReplaceFileRequest{
		Path:         path,
		Content:      "candidate",
		BaseRevision: Revision([]byte("stale")),
	}, func(ChangeSet) error {
		callbackCalled = true
		return nil
	})
	assertChangeErrorCode(t, err, ErrorCodeRevisionConflict)
	if callbackCalled {
		t.Fatal("stale replacement invoked snapshot callback")
	}
	content, revision, err := service.ReadFile(path)
	if err != nil {
		t.Fatalf("read visible file: %v", err)
	}
	if content != "before" || revision != Revision([]byte("before")) {
		t.Fatalf("stale replacement changed visible file: content=%q revision=%q", content, revision)
	}
}

func TestReplaceFileWithConsistentSnapshotRejectsNilInputsBeforeWrite(t *testing.T) {
	var nilService *Service
	_, err := nilService.ReplaceFileWithConsistentSnapshot(
		context.Background(),
		ReplaceFileRequest{},
		func(ChangeSet) error { return nil },
	)
	assertChangeErrorCode(t, err, ErrorCodeConflict)

	service, path := newConsistentSnapshotService(t, "before")
	_, err = service.ReplaceFileWithConsistentSnapshot(context.Background(), ReplaceFileRequest{
		Path:         path,
		Content:      "candidate",
		BaseRevision: Revision([]byte("before")),
	}, nil)
	assertChangeErrorCode(t, err, ErrorCodeConflict)
	content, revision, err := service.ReadFile(path)
	if err != nil {
		t.Fatalf("read visible file: %v", err)
	}
	if content != "before" || revision != Revision([]byte("before")) {
		t.Fatalf("nil callback changed visible file: content=%q revision=%q", content, revision)
	}
}

func TestReplaceFileWithConsistentSnapshotReturnsAppliedChangeOnCallbackError(t *testing.T) {
	service, path := newConsistentSnapshotService(t, "before")
	snapshotErr := errors.New("snapshot failed")

	applied, err := service.ReplaceFileWithConsistentSnapshot(context.Background(), ReplaceFileRequest{
		Path:         path,
		Content:      "candidate",
		BaseRevision: Revision([]byte("before")),
	}, func(change ChangeSet) error {
		if change.ID == "" || change.Path != path || change.Revision != Revision([]byte("candidate")) ||
			len(change.Edits) != 1 || change.Edits[0].NewString != "candidate" {
			t.Fatalf("callback received unexpected applied change: %#v", change)
		}
		return snapshotErr
	})
	if !errors.Is(err, snapshotErr) {
		t.Fatalf("error = %v, want original callback error", err)
	}
	if applied.ID == "" || applied.Path != path || applied.Revision != Revision([]byte("candidate")) ||
		len(applied.Edits) != 1 || applied.Edits[0].NewString != "candidate" {
		t.Fatalf("callback error hid the applied change: %#v", applied)
	}
	content, revision, err := service.ReadFile(path)
	if err != nil {
		t.Fatalf("read visible file: %v", err)
	}
	if content != "candidate" || revision != applied.Revision {
		t.Fatalf("callback error hid committed bytes: content=%q revision=%q applied=%#v", content, revision, applied)
	}
}

func TestReplaceFileWithConsistentSnapshotReturnsChangeIdentityWhenVisibleWriteDurabilityIsPending(t *testing.T) {
	service, path := newConsistentSnapshotService(t, "before")
	originalSync := service.durability.syncRootDirFn
	chapterSyncs := 0
	service.durability.syncRootDirFn = func(root *os.Root, rel string) error {
		if rel == "chapters" {
			chapterSyncs++
			if chapterSyncs >= 2 {
				return errInjectedParentSync
			}
		}
		return originalSync(root, rel)
	}
	callbackCalled := false

	change, err := service.ReplaceFileWithConsistentSnapshot(context.Background(), ReplaceFileRequest{
		Path:         path,
		Content:      "candidate",
		BaseRevision: Revision([]byte("before")),
	}, func(ChangeSet) error {
		callbackCalled = true
		return nil
	})
	assertDurabilityPending(t, err, true)
	if callbackCalled {
		t.Fatal("durability-pending write invoked the snapshot callback")
	}
	if change.ID == "" || change.GroupID == "" || change.Path != path || change.Revision != Revision([]byte("candidate")) {
		t.Fatalf("durability-pending write lost change identity: %#v", change)
	}
	if got := readTestFile(t, service.workspace, path); got != "candidate" {
		t.Fatalf("visible candidate bytes = %q", got)
	}
}

func TestReplaceFileWithConsistentSnapshotIsolatesReturnedChangeFromCallbackMutation(t *testing.T) {
	service, path := newConsistentSnapshotService(t, "before")
	snapshotErr := errors.New("snapshot failed after mutation")

	applied, err := service.ReplaceFileWithConsistentSnapshot(context.Background(), ReplaceFileRequest{
		Path:         path,
		Content:      "candidate",
		BaseRevision: Revision([]byte("before")),
	}, func(change ChangeSet) error {
		change.Edits[0].NewString = "tampered"
		change.Edits[0].Hunks[0].AfterEnd = 0
		return snapshotErr
	})
	if !errors.Is(err, snapshotErr) {
		t.Fatalf("error = %v, want original callback error", err)
	}
	if len(applied.Edits) != 1 || applied.Edits[0].NewString != "candidate" ||
		len(applied.Edits[0].Hunks) != 1 || applied.Edits[0].Hunks[0].AfterEnd != len("candidate") {
		t.Fatalf("callback mutated returned applied change: %#v", applied)
	}
}

func TestReplaceFileWithConsistentSnapshotHoldsLeaseThroughCallback(t *testing.T) {
	service, path := newConsistentSnapshotService(t, "before")
	applied, err := service.ReplaceFileWithConsistentSnapshot(context.Background(), ReplaceFileRequest{
		Path:         path,
		Content:      "candidate",
		BaseRevision: Revision([]byte("before")),
	}, func(ChangeSet) error {
		if service.mu.TryLock() {
			service.mu.Unlock()
			t.Fatal("snapshot callback ran without the workspace mutation lease")
		}
		return nil
	})
	if err != nil || applied.Revision != Revision([]byte("candidate")) {
		t.Fatalf("replacement result: change=%#v err=%v", applied, err)
	}
	if !service.mu.TryLock() {
		t.Fatal("workspace mutation lease remained held after callback returned")
	}
	service.mu.Unlock()

	result, err := service.SaveFile(context.Background(), path, "later", applied.Revision)
	if err != nil || !result.Changed || result.Revision != Revision([]byte("later")) {
		t.Fatalf("managed save after callback: result=%#v err=%v", result, err)
	}
	content, _, err := service.ReadFile(path)
	if err != nil || content != "later" {
		t.Fatalf("final visible file: content=%q err=%v", content, err)
	}
}

func newConsistentSnapshotService(t *testing.T, content string) (*Service, string) {
	t.Helper()
	service, err := NewService(t.TempDir())
	if err != nil {
		t.Fatalf("create workspace change service: %v", err)
	}
	path := "chapters/ch01.md"
	if _, err := service.SaveFile(context.Background(), path, content, "missing"); err != nil {
		t.Fatalf("seed visible file: %v", err)
	}
	return service, path
}
