package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWalkthroughNewWorkspacePublishesNotRequiredMarkerWithoutMigrationResidue(t *testing.T) {
	workspace := t.TempDir()
	preview, authorization := newMigrationTestRequest(t, workspace, "new-workspace-01")
	if preview.Kind != WorkspaceKindNew {
		t.Fatalf("preview kind = %s, want new", preview.Kind)
	}
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != MigrationNotRequired || result.NextAction != MigrationNextNone || result.RollbackAvailable || result.Receipt != nil {
		t.Fatalf("new workspace result = %#v", result)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, MigrationRootRelativePath)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("new workspace left migration state: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova", "quality", "migration-receipts")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("new workspace created a false receipt: %v", statErr)
	}
	raw, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath)))
	if readErr != nil {
		t.Fatal(readErr)
	}
	marker, issues := parseMarker(raw)
	if len(issues) != 0 || marker.Migration != MigrationNotRequired || marker.SchemaVersion != 1 {
		t.Fatalf("new marker/issues = %#v / %#v", marker, issues)
	}
	generated, decodeErr := decodeGeneratedMarker(raw)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if generated.Migration.BackupManifestRef != "" || generated.Migration.StagingRef != "" || generated.Migration.ReceiptRef != "" || generated.Migration.SwitchBoundary != "none" || generated.Migration.RollbackAvailable {
		t.Fatalf("new marker falsely claims migration work = %#v", generated.Migration)
	}
	before := workspaceTreeDigest(t, workspace)
	replayed, err := executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	if err != nil || replayed.State != MigrationNotRequired || replayed.WorkspaceMutated {
		t.Fatalf("new replay = %#v / %v", replayed, err)
	}
	if after := workspaceTreeDigest(t, workspace); !reflect.DeepEqual(after, before) {
		t.Fatalf("new replay rewrote the marker\nbefore: %#v\nafter: %#v", before, after)
	}
}

func TestWalkthroughNewWorkspaceSameIDDifferentConfirmationConflicts(t *testing.T) {
	workspace := t.TempDir()
	preview, authorization := newMigrationTestRequest(t, workspace, "new-workspace-01")
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization}); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath))
	before, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	different, err := BuildMigrationAuthorization(preview, "new-workspace-01", AuthorConfirmation{ID: "other-confirmation", Evidence: "other evidence"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: different})
	assertMigrationError(t, err, CodeMigrationRecordConflict, MigrationStepLoadRecord)
	after, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("new-workspace payload conflict overwrote the existing marker")
	}
}
