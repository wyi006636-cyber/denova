package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMigrationRollbackRequiresSeparateAuthorBoundRecoveryChoice(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	if _, err := executeMigrationUntil(t, preview, authorization, faultAfterStaged, nil); err == nil {
		t.Fatal("expected staged boundary fault")
	}
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	before := readMigrationRecordTest(t, workspace, authorization.MigrationID)
	_, err = executor.Rollback(context.Background(), MigrationRecoveryRequest{
		Migration: MigrationRequest{Preview: preview, Authorization: authorization},
	})
	assertMigrationError(t, err, CodeMigrationAuthorizationRequired, MigrationStepAuthorize)
	after := readMigrationRecordTest(t, workspace, authorization.MigrationID)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("missing rollback authorization changed durable state")
	}
}

func TestMigrationRollbackPendingResumesBeforeSwitchAndRetainsVerifiedBackup(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	if _, err := executeMigrationUntil(t, preview, authorization, faultAfterStaged, nil); err == nil {
		t.Fatal("expected staged boundary fault")
	}
	recovery := newRollbackAuthorizationTest(t, authorization)
	executor, err := newMigrationExecutorWithDependencies(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()}, migrationExecutorDependencies{
		fault: func(point migrationFaultPoint) error {
			if point == faultAfterRollbackPending {
				return errors.New("stop after rollback_pending")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := executor.Rollback(context.Background(), MigrationRecoveryRequest{
		Migration:     MigrationRequest{Preview: preview, Authorization: authorization},
		Authorization: recovery,
	})
	if err == nil || first.State != MigrationRollbackPending || first.NextAction != MigrationNextRollback {
		t.Fatalf("rollback pending result/error = %#v / %v", first, err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova-migration", "migration-01", "backup", "manifest.json")); statErr != nil {
		t.Fatalf("verified backup missing at rollback_pending: %v", statErr)
	}

	fresh, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := fresh.Resume(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.State != MigrationRolledBack || resumed.NextAction != MigrationNextNone || !resumed.RollbackAvailable {
		t.Fatalf("resumed rollback = %#v", resumed)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova-migration", "migration-01", "backup", "manifest.json")); statErr != nil {
		t.Fatalf("rollback deleted the only verified backup: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova-migration", "migration-01", "rollback", "stage")); statErr != nil {
		t.Fatalf("pre-switch stage was not safely quarantined: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("pre-switch rollback published a marker: %v", statErr)
	}
}

func TestMigrationRollbackAfterCurrentSwitchRestoresMarkerAbsenceAndQuarantinesReceipt(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization}); err != nil {
		t.Fatal(err)
	}
	recovery := newRollbackAuthorizationTest(t, authorization)
	rolledBack, err := executor.Rollback(context.Background(), MigrationRecoveryRequest{
		Migration:     MigrationRequest{Preview: preview, Authorization: authorization},
		Authorization: recovery,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.State != MigrationRolledBack || rolledBack.Receipt != nil {
		t.Fatalf("rolled-back result = %#v", rolledBack)
	}
	for _, rel := range []string{MarkerRelativePath, receiptRelativePath(authorization.MigrationID)} {
		if _, statErr := os.Stat(filepath.Join(workspace, filepath.FromSlash(rel))); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("rollback did not restore absence for %s: %v", rel, statErr)
		}
	}
	for _, name := range []string{"published-workspace-schema.json", "migration-receipt.json"} {
		if _, statErr := os.Stat(filepath.Join(workspace, ".denova-migration", "migration-01", "rollback", name)); statErr != nil {
			t.Fatalf("rollback quarantine %s missing: %v", name, statErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova-migration", "migration-01", "backup", "manifest.json")); statErr != nil {
		t.Fatalf("verified backup missing after rollback: %v", statErr)
	}
	before := workspaceTreeDigest(t, workspace)
	replayed, err := executor.Rollback(context.Background(), MigrationRecoveryRequest{
		Migration:     MigrationRequest{Preview: preview, Authorization: authorization},
		Authorization: recovery,
	})
	if err != nil || replayed.State != MigrationRolledBack || replayed.WorkspaceMutated {
		t.Fatalf("rolled-back replay = %#v / %v", replayed, err)
	}
	if after := workspaceTreeDigest(t, workspace); !reflect.DeepEqual(after, before) {
		t.Fatal("rolled-back replay changed bytes")
	}
}

func TestMigrationRollbackAfterLegacySwitchRepinsRetainedNova(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".nova/lore/items.json", `{"items":[1]}`)
	legacyBefore := fileHashesByPathTest(t, workspace, []string{".nova/lore/items.json"})
	preview, authorization := newMigrationTestRequest(t, workspace, "legacy-migration-01")
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization}); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := executor.Rollback(context.Background(), MigrationRecoveryRequest{
		Migration:     MigrationRequest{Preview: preview, Authorization: authorization},
		Authorization: newRollbackAuthorizationTest(t, authorization),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.State != MigrationRolledBack {
		t.Fatalf("legacy rollback = %#v", rolledBack)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("legacy rollback left live .denova: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova-migration", "legacy-migration-01", "rollback", ".denova")); statErr != nil {
		t.Fatalf("legacy published namespace not quarantined: %v", statErr)
	}
	legacyAfter := fileHashesByPathTest(t, workspace, []string{".nova/lore/items.json"})
	if !reflect.DeepEqual(legacyAfter, legacyBefore) {
		t.Fatal("legacy rollback changed retained .nova recovery input")
	}
	inspection, err := newSchemaV1Inspector(t, schemaV1PreviewOptions().Inspector.ApplicationVersion).Inspect(workspace)
	if err != nil || inspection.ActiveRoot != ".nova" || !inspection.CanOpen() {
		t.Fatalf("rolled-back legacy inspection = %#v / %v", inspection, err)
	}
}

func TestMigrationRollbackRefusesDivergentLiveDestinationWithoutOverwrite(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization}); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath))
	external := []byte("external author bytes after migration\n")
	if err := os.WriteFile(markerPath, external, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := executor.Rollback(context.Background(), MigrationRecoveryRequest{
		Migration:     MigrationRequest{Preview: preview, Authorization: authorization},
		Authorization: newRollbackAuthorizationTest(t, authorization),
	})
	migrationErr := assertMigrationError(t, err, CodeMigrationRollbackConflict, MigrationStepRollback)
	if result.State != MigrationNeedsRecovery || migrationErr.NextAction != MigrationNextManualRecovery {
		t.Fatalf("divergent rollback = %#v / %#v", result, migrationErr)
	}
	after, readErr := os.ReadFile(markerPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(after, external) {
		t.Fatal("rollback overwrote divergent external bytes")
	}
}

func TestMigrationRollbackResumesPartialCurrentEntrySequence(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	base, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization}); err != nil {
		t.Fatal(err)
	}
	faulting, err := newMigrationExecutorWithDependencies(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()}, migrationExecutorDependencies{fault: func(point migrationFaultPoint) error {
		if point == faultAfterRollbackReceiptQuarantined {
			return errors.New("crash after first rollback entry")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := faulting.Rollback(context.Background(), MigrationRecoveryRequest{
		Migration:     MigrationRequest{Preview: preview, Authorization: authorization},
		Authorization: newRollbackAuthorizationTest(t, authorization),
	})
	if err == nil || first.State != MigrationRollbackPending {
		t.Fatalf("partial rollback = %#v / %v", first, err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, receiptRelativePath(authorization.MigrationID))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("receipt was not quarantined before injected crash: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath))); statErr != nil {
		t.Fatalf("marker should remain live at partial boundary: %v", statErr)
	}
	fresh, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := fresh.Resume(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	if err != nil || resumed.State != MigrationRolledBack {
		t.Fatalf("partial rollback resume = %#v / %v", resumed, err)
	}
}

func TestMigrationRollbackResumeValidatesQuarantinedStageBytes(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	if _, err := executeMigrationUntil(t, preview, authorization, faultAfterStaged, nil); err == nil {
		t.Fatal("expected staged fault")
	}
	faulting, err := newMigrationExecutorWithDependencies(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()}, migrationExecutorDependencies{fault: func(point migrationFaultPoint) error {
		if point == faultAfterRollbackVisible {
			return errors.New("crash after stage quarantine")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := faulting.Rollback(context.Background(), MigrationRecoveryRequest{
		Migration:     MigrationRequest{Preview: preview, Authorization: authorization},
		Authorization: newRollbackAuthorizationTest(t, authorization),
	}); err == nil {
		t.Fatal("expected rollback visible fault")
	}
	quarantinedMarker := filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "rollback", "stage", "workspace-schema.json")
	if err := os.WriteFile(quarantinedMarker, []byte("tampered quarantine"), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fresh.Resume(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	assertMigrationError(t, err, CodeMigrationRollbackConflict, MigrationStepRollback)
	if result.State != MigrationNeedsRecovery {
		t.Fatalf("tampered quarantine state = %s, want needs_recovery", result.State)
	}
}

func newRollbackAuthorizationTest(t *testing.T, authorization MigrationAuthorization) MigrationRecoveryAuthorization {
	t.Helper()
	recovery, err := BuildMigrationRecoveryAuthorization(authorization, RecoveryActionRollback, AuthorConfirmation{
		ID:       "rollback-confirmation-01",
		Evidence: "author explicitly chose safe rollback",
	})
	if err != nil {
		t.Fatal(err)
	}
	return recovery
}
