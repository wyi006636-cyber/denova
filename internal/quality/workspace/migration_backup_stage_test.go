package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestMigrationBackupCurrentAdoptionRecordsMissingMarkerWithoutCreativeRewrite(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "idea bytes\n")
	writeWorkspaceTestFile(t, workspace, ".denova/lore/items.json", `{"items":[]}`)
	writeWorkspaceTestFile(t, workspace, ".denova/runs/run-01.jsonl", "runtime\n")
	before := workspaceTreeDigest(t, workspace)
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	result, err := executeMigrationUntil(t, preview, authorization, faultAfterBackedUp, nil)
	if err == nil || result.State != MigrationBackedUp {
		t.Fatalf("result/error = %#v / %v, want durable backed_up fault", result, err)
	}

	manifest := readBackupManifestTest(t, workspace, authorization.MigrationID)
	if manifest.RecordVersion != BackupManifestVersionV1 || manifest.MigrationID != authorization.MigrationID {
		t.Fatalf("backup manifest identity = %#v", manifest)
	}
	if len(manifest.Entries) != 1 {
		t.Fatalf("backup entries = %#v, want only the affected marker absence", manifest.Entries)
	}
	entry := manifest.Entries[0]
	if entry.Path != MarkerRelativePath || entry.Exists || entry.NodeType != BackupNodeMissing || entry.BackupPath != "" {
		t.Fatalf("marker backup entry = %#v", entry)
	}
	assertSHA256String(t, manifest.SourceExpectationsSHA256)
	assertSHA256String(t, resultBackupHash(t, workspace, authorization.MigrationID))

	after := workspaceTreeDigest(t, workspace)
	if !containsAllTreeEntries(after, before) {
		t.Fatalf("backup changed pre-existing creative/runtime bytes\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestMigrationBackupLegacyCopiesCompleteMappedInputsInStableHashManifest(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".nova/lore/items.json", `{"items":[{"name":"潮汐"}]}`)
	writeWorkspaceTestFile(t, workspace, ".nova/runs/run-01.jsonl", "runtime checkpoint\n")
	writeWorkspaceTestFile(t, workspace, "ideas.md", "root idea remains in place\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "legacy-migration-01")
	if preview.Kind != WorkspaceKindLegacy {
		t.Fatalf("preview kind = %s, want legacy", preview.Kind)
	}
	_, err := executeMigrationUntil(t, preview, authorization, faultAfterBackedUp, nil)
	if err == nil {
		t.Fatal("expected injected backed_up fault")
	}

	manifest := readBackupManifestTest(t, workspace, authorization.MigrationID)
	paths := make([]string, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		paths = append(paths, entry.Path)
	}
	if !sort.StringsAreSorted(paths) {
		t.Fatalf("backup paths are not stable: %#v", paths)
	}
	wantPaths := []string{".denova", ".nova/lore/items.json", ".nova/runs/run-01.jsonl"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("backup paths = %#v, want %#v", paths, wantPaths)
	}
	for _, entry := range manifest.Entries {
		if entry.Path == ".denova" {
			if entry.Exists || entry.NodeType != BackupNodeMissing {
				t.Fatalf("target absence entry = %#v", entry)
			}
			continue
		}
		if !entry.Exists || entry.NodeType != BackupNodeFile {
			t.Fatalf("source backup entry = %#v", entry)
		}
		assertSHA256String(t, entry.SHA256)
		backupRaw, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(entry.BackupPath)))
		if readErr != nil {
			t.Fatal(readErr)
		}
		sourceRaw, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(entry.Path)))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !reflect.DeepEqual(backupRaw, sourceRaw) {
			t.Fatalf("backup %s differs from source %s", entry.BackupPath, entry.Path)
		}
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("backup created the live target: %v", statErr)
	}
}

func TestMigrationBackupRevalidatesSourceAfterCopyAndPreservesEvidence(t *testing.T) {
	workspace := t.TempDir()
	sourcePath := filepath.Join(workspace, ".nova", "lore", "items.json")
	writeWorkspaceTestFile(t, workspace, ".nova/lore/items.json", `{"items":[1]}`)
	preview, authorization := newMigrationTestRequest(t, workspace, "legacy-migration-01")
	mutated := false
	result, err := executeMigrationUntil(t, preview, authorization, "", func(point migrationFaultPoint) error {
		if point == checkpointBeforeBackupPostVerify && !mutated {
			mutated = true
			return os.WriteFile(sourcePath, []byte(`{"items":[2]}`), 0o644)
		}
		return nil
	})
	migrationErr := assertMigrationError(t, err, CodeMigrationSourceChanged, MigrationStepBackup)
	if !migrationErr.WorkspaceMutated || migrationErr.Path != ".nova/lore/items.json" {
		t.Fatalf("source-change evidence = %#v", migrationErr)
	}
	assertSHA256String(t, migrationErr.ExpectedSHA256)
	assertSHA256String(t, migrationErr.ActualSHA256)
	if result.State != MigrationValidated {
		t.Fatalf("result state = %s, want validated before durable backup manifest", result.State)
	}
	backupPath := filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "backup", "files", ".nova", "lore", "items.json")
	if _, statErr := os.Stat(backupPath); statErr != nil {
		t.Fatalf("verified copy evidence was not retained: %v", statErr)
	}
}

func TestMigrationBackupRevalidatesRequiredAbsenceAfterPreparation(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	externalMarker := []byte("external marker\n")
	created := false
	result, err := executeMigrationUntil(t, preview, authorization, "", func(point migrationFaultPoint) error {
		if point == checkpointBeforeBackupPostVerify && !created {
			created = true
			return os.WriteFile(filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath)), externalMarker, 0o600)
		}
		return nil
	})
	migrationErr := assertMigrationError(t, err, CodeMigrationSourceChanged, MigrationStepBackup)
	if result.State != MigrationValidated || migrationErr.Path != MarkerRelativePath || migrationErr.ExpectedSHA256 != "absent" {
		t.Fatalf("missing-state change = %#v / %#v", result, migrationErr)
	}
	after, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, externalMarker) {
		t.Fatalf("external marker bytes were overwritten: %q", after)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "backup")); statErr != nil {
		t.Fatalf("backup evidence was not retained: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "backup", "manifest.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("changed missing-state published a false manifest: %v", statErr)
	}
}

func TestMigrationStageCurrentAdoptionContainsOnlyCompleteMarker(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author idea\n")
	writeWorkspaceTestFile(t, workspace, ".denova/lore/items.json", `{"items":[]}`)
	creativeBefore := workspaceTreeDigest(t, workspace)
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	result, err := executeMigrationUntil(t, preview, authorization, faultAfterStaged, nil)
	if err == nil || result.State != MigrationStaged {
		t.Fatalf("result/error = %#v / %v, want durable staged fault", result, err)
	}

	manifest := readStageManifestTest(t, workspace, authorization.MigrationID)
	if len(manifest.Entries) != 1 || manifest.Entries[0].Path != "workspace-schema.json" {
		t.Fatalf("current adoption stage entries = %#v", manifest.Entries)
	}
	markerPath := filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "stage", "workspace-schema.json")
	raw, readErr := os.ReadFile(markerPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	marker, issues := parseMarker(raw)
	if len(issues) != 0 {
		t.Fatalf("staged marker issues = %#v", issues)
	}
	if marker.Migration != MigrationVerifying || marker.SchemaVersion != 1 || marker.Writer.Version != schemaV1PreviewOptions().Inspector.ApplicationVersion {
		t.Fatalf("staged marker = %#v", marker)
	}
	if after := workspaceTreeDigest(t, workspace); !containsAllTreeEntries(after, creativeBefore) {
		t.Fatalf("staging changed pre-existing live bytes\nbefore: %#v\nafter: %#v", creativeBefore, after)
	}
}

func TestMigrationStageLegacyBuildsCompleteFutureDenovaNamespaceAndDraftReceipt(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".nova/lore/items.json", `{"items":[{"name":"潮汐"}]}`)
	writeWorkspaceTestFile(t, workspace, ".nova/runs/run-01.jsonl", "runtime\n")
	legacyBefore := workspaceTreeDigest(t, workspace)
	preview, authorization := newMigrationTestRequest(t, workspace, "legacy-migration-01")
	result, err := executeMigrationUntil(t, preview, authorization, faultAfterStaged, nil)
	if err == nil || result.State != MigrationStaged {
		t.Fatalf("result/error = %#v / %v, want durable staged fault", result, err)
	}

	stageBase := filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "stage", ".denova")
	assertFileBytesEqual(t,
		filepath.Join(workspace, ".nova", "lore", "items.json"),
		filepath.Join(stageBase, "lore", "items.json"),
	)
	assertFileBytesEqual(t,
		filepath.Join(workspace, ".nova", "runs", "run-01.jsonl"),
		filepath.Join(stageBase, "runs", "run-01.jsonl"),
	)
	markerRaw, readErr := os.ReadFile(filepath.Join(stageBase, "workspace-schema.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	marker, issues := parseMarker(markerRaw)
	if len(issues) != 0 || marker.Migration != MigrationVerifying {
		t.Fatalf("legacy staged marker/issues = %#v / %#v", marker, issues)
	}
	draftPath := filepath.Join(stageBase, "quality", "migration-receipts", authorization.MigrationID+".json")
	draftRaw, readErr := os.ReadFile(draftPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	draft, decodeErr := decodeMigrationReceipt(draftRaw)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if draft.Result != ReceiptPendingVerification || draft.MigrationID != authorization.MigrationID {
		t.Fatalf("staged receipt draft = %#v", draft)
	}
	manifest := readStageManifestTest(t, workspace, authorization.MigrationID)
	if len(manifest.Entries) != 4 {
		t.Fatalf("legacy stage entries = %#v, want source copies + marker + draft receipt", manifest.Entries)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stage published live .denova early: %v", statErr)
	}
	if after := workspaceTreeDigest(t, workspace); !containsAllTreeEntries(after, legacyBefore) {
		t.Fatalf("stage changed retained legacy bytes\nbefore: %#v\nafter: %#v", legacyBefore, after)
	}
}

func executeMigrationUntil(t *testing.T, preview MigrationPreview, authorization MigrationAuthorization, stop migrationFaultPoint, hook func(migrationFaultPoint) error) (MigrationResult, error) {
	t.Helper()
	executor, err := newMigrationExecutorWithDependencies(MigrationExecutorOptions{
		Lease:          &migrationTestLease{invoke: true},
		PreviewOptions: schemaV1PreviewOptions(),
	}, migrationExecutorDependencies{fault: func(point migrationFaultPoint) error {
		if hook != nil {
			if err := hook(point); err != nil {
				return err
			}
		}
		if stop != "" && point == stop {
			return errors.New("injected durable-boundary fault")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
}

func readBackupManifestTest(t *testing.T, workspace, migrationID string) BackupManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(workspace, ".denova-migration", migrationID, "backup", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := decodeBackupManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func readStageManifestTest(t *testing.T, workspace, migrationID string) StageManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(workspace, ".denova-migration", migrationID, "stage", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := decodeStageManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func resultBackupHash(t *testing.T, workspace, migrationID string) string {
	t.Helper()
	record, _, exists, err := loadMigrationRecord(workspace, migrationID)
	if err != nil || !exists || record.Backup == nil {
		t.Fatalf("load record backup: exists=%t record=%#v error=%v", exists, record, err)
	}
	return record.Backup.SHA256
}

func containsAllTreeEntries(after, before []string) bool {
	afterSet := make(map[string]struct{}, len(after))
	for _, entry := range after {
		afterSet[entry] = struct{}{}
	}
	for _, entry := range before {
		if _, exists := afterSet[entry]; !exists {
			return false
		}
	}
	return true
}

func assertFileBytesEqual(t *testing.T, first, second string) {
	t.Helper()
	firstRaw, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondRaw, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstRaw, secondRaw) {
		t.Fatalf("file bytes differ: %s != %s", first, second)
	}
}
