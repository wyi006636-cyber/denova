package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMigrationSwitchPersistsCompleteIntentBeforeVisibleRename(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	result, err := executeMigrationUntil(t, preview, authorization, faultAfterSwitchPending, nil)
	if err == nil || result.State != MigrationSwitchPending || result.NextAction != MigrationNextSwitch {
		t.Fatalf("result/error = %#v / %v, want durable switch_pending fault", result, err)
	}
	record := readMigrationRecordTest(t, workspace, authorization.MigrationID)
	if record.Switch == nil {
		t.Fatal("switch_pending record has no switch intent")
	}
	intent := record.Switch
	if intent.SourceRoot != record.CanonicalSourceRoot || intent.TargetRoot != record.CanonicalTargetRoot || intent.PublishedEntry != MarkerRelativePath || intent.Boundary != SwitchBoundarySameFilesystemNamespaceRename || intent.NextAction != MigrationNextSwitch {
		t.Fatalf("switch intent = %#v", intent)
	}
	if intent.BackupManifest != *record.Backup || intent.Stage != *record.Stage {
		t.Fatal("switch intent does not bind the durable backup and stage")
	}
	assertSHA256String(t, intent.PublishedSHA256)
	if _, statErr := os.Stat(filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("marker became visible before the switch: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "stage", "workspace-schema.json")); statErr != nil {
		t.Fatalf("staged marker was not retained: %v", statErr)
	}
}

func TestMigrationSwitchRevalidatesSourceImmediatelyBeforeRename(t *testing.T) {
	workspace := t.TempDir()
	creativePath := filepath.Join(workspace, ".denova", "ideas.md")
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes v1\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	mutated := false
	result, err := executeMigrationUntil(t, preview, authorization, "", func(point migrationFaultPoint) error {
		if point == checkpointBeforeSwitchSourceVerify && !mutated {
			mutated = true
			return os.WriteFile(creativePath, []byte("author bytes v2\n"), 0o644)
		}
		return nil
	})
	migrationErr := assertMigrationError(t, err, CodeMigrationSourceChanged, MigrationStepSwitch)
	if migrationErr.Path != ".denova/ideas.md" || !migrationErr.WorkspaceMutated {
		t.Fatalf("switch source evidence = %#v", migrationErr)
	}
	if result.State != MigrationSwitchPending {
		t.Fatalf("result state = %s, want switch_pending", result.State)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("switch overwrote the live marker after a source change: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "backup", "manifest.json")); statErr != nil {
		t.Fatalf("verified backup was not retained: %v", statErr)
	}
}

func TestMigrationSwitchDestinationConflictNeverOverwritesExternalBytes(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	external := []byte("external marker bytes\n")
	created := false
	result, err := executeMigrationUntil(t, preview, authorization, "", func(point migrationFaultPoint) error {
		if point == checkpointBeforeSwitchSourceVerify && !created {
			created = true
			return os.WriteFile(filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath)), external, 0o644)
		}
		return nil
	})
	assertMigrationError(t, err, CodeMigrationSwitchConflict, MigrationStepSwitch)
	if result.State != MigrationNeedsRecovery {
		t.Fatalf("result state = %s, want needs_recovery", result.State)
	}
	after, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath)))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(after, external) {
		t.Fatalf("external destination bytes were overwritten: %q", after)
	}
}

func TestMigrationCurrentAdoptionVerifiesWithNormalReaderThenReceiptsAndCompletes(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author idea\n")
	writeWorkspaceTestFile(t, workspace, ".denova/lore/items.json", `{"items":[]}`)
	creativeBefore := fileHashesByPathTest(t, workspace, []string{".denova/ideas.md", ".denova/lore/items.json"})
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != MigrationCompleted || result.NextAction != MigrationNextNone || !result.RollbackAvailable || result.Receipt == nil {
		t.Fatalf("completed result = %#v", result)
	}
	assertCompletedWorkspaceTest(t, workspace, authorization.MigrationID)
	creativeAfter := fileHashesByPathTest(t, workspace, []string{".denova/ideas.md", ".denova/lore/items.json"})
	if !reflect.DeepEqual(creativeAfter, creativeBefore) {
		t.Fatalf("adoption rewrote creative bytes\nbefore: %#v\nafter: %#v", creativeBefore, creativeAfter)
	}
}

func TestMigrationLegacyPublishesOneDenovaRootRetainsNovaAndNeverDualWrites(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".nova/lore/items.json", `{"items":[{"name":"潮汐"}]}`)
	writeWorkspaceTestFile(t, workspace, ".nova/runs/run-01.jsonl", "runtime\n")
	legacyBefore := fileHashesByPathTest(t, workspace, []string{".nova/lore/items.json", ".nova/runs/run-01.jsonl"})
	preview, authorization := newMigrationTestRequest(t, workspace, "legacy-migration-01")
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != MigrationCompleted || result.Receipt == nil {
		t.Fatalf("legacy result = %#v", result)
	}
	assertCompletedWorkspaceTest(t, workspace, authorization.MigrationID)
	assertFileBytesEqual(t, filepath.Join(workspace, ".nova/lore/items.json"), filepath.Join(workspace, ".denova/lore/items.json"))
	legacyAfter := fileHashesByPathTest(t, workspace, []string{".nova/lore/items.json", ".nova/runs/run-01.jsonl"})
	if !reflect.DeepEqual(legacyAfter, legacyBefore) {
		t.Fatalf("retained legacy recovery input changed\nbefore: %#v\nafter: %#v", legacyBefore, legacyAfter)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".denova/lore/items.json"), []byte(`{"items":[{"name":"new writer root"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyFinal := fileHashesByPathTest(t, workspace, []string{".nova/lore/items.json", ".nova/runs/run-01.jsonl"})
	if !reflect.DeepEqual(legacyFinal, legacyBefore) {
		t.Fatal("writing the current root dual-wrote retained legacy bytes")
	}
}

func TestMigrationLegacyVisibleSwitchRejectsRogueDestinationAndLateSourceEdit(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, workspace string)
		path   string
	}{
		{
			name: "rogue destination file",
			mutate: func(t *testing.T, workspace string) {
				t.Helper()
				writeWorkspaceTestFile(t, workspace, ".denova/rogue.md", "unapproved\n")
			},
			path: ".denova",
		},
		{
			name: "late retained source edit",
			mutate: func(t *testing.T, workspace string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(workspace, ".nova", "lore", "items.json"), []byte(`{"items":[2]}`), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			path: ".nova/lore/items.json",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeWorkspaceTestFile(t, workspace, ".nova/lore/items.json", `{"items":[1]}`)
			preview, authorization := newMigrationTestRequest(t, workspace, "legacy-migration-01")
			first, err := executeMigrationUntil(t, preview, authorization, faultAfterVisibleSwitch, nil)
			if err == nil || first.State != MigrationSwitchPending {
				t.Fatalf("visible-switch boundary = %#v / %v", first, err)
			}
			test.mutate(t, workspace)
			fresh, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
			if err != nil {
				t.Fatal(err)
			}
			result, err := fresh.Resume(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
			var migrationErr *MigrationError
			if !errors.As(err, &migrationErr) {
				t.Fatalf("resume error = %T %v, want *MigrationError", err, err)
			}
			if result.State != MigrationNeedsRecovery || migrationErr.NextAction != MigrationNextManualRecovery {
				t.Fatalf("late divergence = %#v / %#v", result, migrationErr)
			}
			if migrationErr.Path != test.path {
				t.Fatalf("conflict path = %q, want %q", migrationErr.Path, test.path)
			}
			if _, statErr := os.Stat(filepath.Join(workspace, ".denova")); statErr != nil {
				t.Fatalf("published recovery evidence was lost: %v", statErr)
			}
			if _, statErr := os.Stat(filepath.Join(workspace, ".nova")); statErr != nil {
				t.Fatalf("retained source recovery input was lost: %v", statErr)
			}
		})
	}
}

func TestMigrationResumeReconcilesVisibleSwitchAndReceiptBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		fault migrationFaultPoint
	}{
		{name: "visible rename before parent sync", fault: faultAfterVisibleSwitch},
		{name: "parent sync before switched state", fault: faultAfterSwitchParentSync},
		{name: "switched state", fault: faultAfterSwitched},
		{name: "verifying state", fault: faultAfterVerifying},
		{name: "receipt publication", fault: faultAfterReceiptPublication},
		{name: "final receipt publication", fault: faultAfterFinalReceiptPublication},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
			preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
			first, err := executeMigrationUntil(t, preview, authorization, test.fault, nil)
			if err == nil {
				t.Fatalf("expected fault at %s", test.fault)
			}
			if first.State == MigrationCompleted {
				t.Fatalf("fault %s falsely completed", test.fault)
			}
			executor, buildErr := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			resumed, resumeErr := executor.Resume(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
			if resumeErr != nil {
				t.Fatal(resumeErr)
			}
			if resumed.State != MigrationCompleted {
				t.Fatalf("resumed state = %s, want completed", resumed.State)
			}
			assertCompletedWorkspaceTest(t, workspace, authorization.MigrationID)
		})
	}
}

func TestMigrationReceiptIsNonTerminalUntilCompletedMarkerIsNormallyVerified(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	first, err := executeMigrationUntil(t, preview, authorization, faultAfterReceiptPublication, nil)
	if err == nil || first.State != MigrationVerifying {
		t.Fatalf("receipt boundary = %#v / %v", first, err)
	}
	record := readMigrationRecordTest(t, workspace, authorization.MigrationID)
	if record.FinalReceiptDurable || !validSHA256(record.PublishedMarkerSHA256) {
		t.Fatalf("receipt-boundary record = %#v", record)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(receiptRelativePath(authorization.MigrationID))))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := decodeMigrationReceipt(raw)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Result != ReceiptVerifiedPendingCompletion || receipt.State != MigrationVerifying || receipt.ExpectedCompletedMarkerSHA256 != record.PublishedMarkerSHA256 {
		t.Fatalf("pre-completion receipt = %#v", receipt)
	}
	markerRaw, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	marker, issues := parseMarker(markerRaw)
	if len(issues) != 0 || marker.Migration != MigrationVerifying {
		t.Fatalf("marker advanced before terminal receipt protocol = %#v / %#v", marker, issues)
	}
}

func TestMigrationResumeReusesVerifiedPendingReceiptBeforeCompletion(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	first, err := executeMigrationUntil(t, preview, authorization, faultAfterReceiptPublication, nil)
	if err == nil || first.State != MigrationVerifying {
		t.Fatalf("receipt boundary = %#v / %v", first, err)
	}
	receiptPath := filepath.Join(workspace, filepath.FromSlash(receiptRelativePath(authorization.MigrationID)))
	before, err := os.Stat(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := newMigrationExecutorWithDependencies(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()}, migrationExecutorDependencies{fault: func(point migrationFaultPoint) error {
		if point == faultAfterCompletedMarkerPublication {
			return errors.New("stop before final receipt")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fresh.Resume(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	if err == nil || result.State != MigrationVerifying {
		t.Fatalf("completed-marker boundary = %#v / %v", result, err)
	}
	after, err := os.Stat(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
		t.Fatal("resume rewrote the already verified pending receipt")
	}
}

func TestMigrationResumeAfterCompletedMarkerVisibleBeforeFinalReceipt(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	first, err := executeMigrationUntil(t, preview, authorization, faultAfterCompletedMarkerPublication, nil)
	if err == nil || first.State != MigrationVerifying {
		t.Fatalf("completed-marker boundary = %#v / %v", first, err)
	}
	record := readMigrationRecordTest(t, workspace, authorization.MigrationID)
	if record.FinalReceiptDurable || !validSHA256(record.PublishedMarkerSHA256) {
		t.Fatalf("completed-marker boundary record = %#v", record)
	}
	markerRaw, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	if sha256Hex(markerRaw) != record.PublishedMarkerSHA256 {
		t.Fatal("visible completed marker does not match the precommitted expected hash")
	}
	fresh, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := fresh.Resume(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	if err != nil || resumed.State != MigrationCompleted {
		t.Fatalf("completed-marker resume = %#v / %v", resumed, err)
	}
	assertCompletedWorkspaceTest(t, workspace, authorization.MigrationID)
}

func TestMigrationCompletedReplayValidatesReceiptAndDoesNotSwitchAgain(t *testing.T) {
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
	receiptPath := filepath.Join(workspace, ".denova", "quality", "migration-receipts", "migration-01.json")
	markerPath := filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath))
	receiptInfo, err := os.Stat(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	markerInfo, err := os.Stat(markerPath)
	if err != nil {
		t.Fatal(err)
	}

	replayed, err := executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	if err != nil || replayed.State != MigrationCompleted || replayed.WorkspaceMutated {
		t.Fatalf("completed replay = %#v / %v", replayed, err)
	}
	receiptAfter, _ := os.Stat(receiptPath)
	markerAfter, _ := os.Stat(markerPath)
	if !os.SameFile(receiptInfo, receiptAfter) || !os.SameFile(markerInfo, markerAfter) || receiptInfo.ModTime() != receiptAfter.ModTime() || markerInfo.ModTime() != markerAfter.ModTime() {
		t.Fatal("completed replay rewrote terminal receipt or marker")
	}

	raw, readErr := os.ReadFile(receiptPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	raw[len(raw)/2] ^= 1
	if err := os.WriteFile(receiptPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	migrationErr := assertMigrationError(t, err, CodeMigrationArtifactInvalid, MigrationStepPublishReceipt)
	if migrationErr.State != MigrationCompleted || !strings.Contains(migrationErr.Message, "receipt") {
		t.Fatalf("tampered receipt evidence = %#v", migrationErr)
	}
}

func TestMigrationCompletedReplayPreservesLaterAuthorCreativeBytes(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".nova/lore/items.json", `{"items":[1]}`)
	preview, authorization := newMigrationTestRequest(t, workspace, "legacy-migration-01")
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization}); err != nil {
		t.Fatal(err)
	}
	livePath := filepath.Join(workspace, ".denova", "lore", "items.json")
	authorBytes := []byte(`{"items":[2],"edited_by":"author"}`)
	if err := os.WriteFile(livePath, authorBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	replayed, err := executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	if err != nil || replayed.State != MigrationCompleted || replayed.WorkspaceMutated {
		t.Fatalf("completed replay after author edit = %#v / %v", replayed, err)
	}
	after, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, authorBytes) {
		t.Fatal("completed replay overwrote later author creative bytes")
	}
}

func assertCompletedWorkspaceTest(t *testing.T, workspace, migrationID string) {
	t.Helper()
	inspection, err := newSchemaV1Inspector(t, schemaV1PreviewOptions().Inspector.ApplicationVersion).Inspect(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.CanManagedMutate() || inspection.ActiveRoot != ".denova" || inspection.Marker.Contract.Migration != MigrationCompleted {
		t.Fatalf("completed normal inspection = %#v", inspection)
	}
	receiptPath := filepath.Join(workspace, ".denova", "quality", "migration-receipts", migrationID+".json")
	raw, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := decodeMigrationReceipt(raw)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Result != ReceiptCompleted || !receipt.Verification.Passed || receipt.State != MigrationCompleted || !receipt.RollbackAvailable {
		t.Fatalf("completed receipt = %#v", receipt)
	}
	if strings.Contains(strings.ToLower(receipt.AtomicityClaim), "global_acid") || !strings.Contains(receipt.AtomicityClaim, "not_cross_filesystem_or_filesystem_plus_git_acid") {
		t.Fatalf("receipt atomicity claim = %q", receipt.AtomicityClaim)
	}
}

func readMigrationRecordTest(t *testing.T, workspace, migrationID string) MigrationRecord {
	t.Helper()
	record, _, exists, err := loadMigrationRecord(workspace, migrationID)
	if err != nil || !exists {
		t.Fatalf("load migration record: exists=%t error=%v", exists, err)
	}
	return record
}

func fileHashesByPathTest(t *testing.T, workspace string, paths []string) map[string]string {
	t.Helper()
	hashes := make(map[string]string, len(paths))
	for _, rel := range paths {
		raw, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		hashes[rel] = sha256Hex(raw)
	}
	return hashes
}
