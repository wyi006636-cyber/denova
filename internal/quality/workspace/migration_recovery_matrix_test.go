package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMigrationResumeFromEveryPreSwitchDurableStateCompletes(t *testing.T) {
	tests := []struct {
		name      string
		fault     migrationFaultPoint
		wantState MigrationState
	}{
		{name: "previewed", fault: faultAfterPreviewed, wantState: MigrationPreviewed},
		{name: "validated", fault: faultAfterValidated, wantState: MigrationValidated},
		{name: "backed_up", fault: faultAfterBackedUp, wantState: MigrationBackedUp},
		{name: "staged", fault: faultAfterStaged, wantState: MigrationStaged},
		{name: "switch_pending", fault: faultAfterSwitchPending, wantState: MigrationSwitchPending},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
			preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
			first, err := executeMigrationUntil(t, preview, authorization, test.fault, nil)
			if err == nil || first.State != test.wantState {
				t.Fatalf("first result/error = %#v / %v, want state %s", first, err, test.wantState)
			}
			fresh, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
			if err != nil {
				t.Fatal(err)
			}
			resumed, err := fresh.Resume(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
			if err != nil {
				t.Fatal(err)
			}
			if resumed.State != MigrationCompleted {
				t.Fatalf("resumed state = %s, want completed", resumed.State)
			}
			assertCompletedWorkspaceTest(t, workspace, authorization.MigrationID)
			receiptRaw, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(receiptRelativePath(authorization.MigrationID))))
			if readErr != nil {
				t.Fatal(readErr)
			}
			receipt, decodeErr := decodeMigrationReceipt(receiptRaw)
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if len(receipt.Failures) == 0 {
				t.Fatal("resumed receipt omitted durable recovery evidence")
			}
		})
	}
}

func TestMigrationResumeReusesVerifiedBackupBytesWithoutRewrite(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".nova/lore/items.json", `{"items":[1]}`)
	preview, authorization := newMigrationTestRequest(t, workspace, "legacy-migration-01")
	if _, err := executeMigrationUntil(t, preview, authorization, faultAfterBackedUp, nil); err == nil {
		t.Fatal("expected backed_up boundary fault")
	}
	manifestPath := filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "backup", "manifest.json")
	backupPath := filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "backup", "files", ".nova", "lore", "items.json")
	manifestInfo, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	backupInfo, err := os.Stat(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := newMigrationExecutorWithDependencies(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()}, migrationExecutorDependencies{fault: func(point migrationFaultPoint) error {
		if point == faultAfterStaged {
			return errors.New("stop after staged")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Resume(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization}); err == nil {
		t.Fatal("expected staged boundary fault")
	}
	manifestAfter, _ := os.Stat(manifestPath)
	backupAfter, _ := os.Stat(backupPath)
	if !os.SameFile(manifestInfo, manifestAfter) || !os.SameFile(backupInfo, backupAfter) || manifestInfo.ModTime() != manifestAfter.ModTime() || backupInfo.ModTime() != backupAfter.ModTime() {
		t.Fatal("resume rewrote already verified backup output")
	}
}

func TestMigrationResumeReusesVerifiedStageBytesWithoutRewrite(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	if _, err := executeMigrationUntil(t, preview, authorization, faultAfterStaged, nil); err == nil {
		t.Fatal("expected staged boundary fault")
	}
	stagePaths := []string{
		filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "stage", "manifest.json"),
		filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "stage", "workspace-schema.json"),
	}
	before := make([]os.FileInfo, len(stagePaths))
	for index, path := range stagePaths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		before[index] = info
	}
	fresh, err := newMigrationExecutorWithDependencies(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()}, migrationExecutorDependencies{fault: func(point migrationFaultPoint) error {
		if point == faultAfterSwitchPending {
			return errors.New("stop before visible switch")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fresh.Resume(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	if err == nil || result.State != MigrationSwitchPending {
		t.Fatalf("switch-pending boundary = %#v / %v", result, err)
	}
	for index, path := range stagePaths {
		after, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if !os.SameFile(before[index], after) || before[index].ModTime() != after.ModTime() {
			t.Fatalf("resume rewrote verified stage output %s", path)
		}
	}
}

func TestMigrationCorruptOrMissingBackupAndStageCannotAdvance(t *testing.T) {
	tests := []struct {
		name   string
		fault  migrationFaultPoint
		tamper func(t *testing.T, workspace, migrationID string)
		step   MigrationStep
	}{
		{
			name:  "missing backup manifest",
			fault: faultAfterBackedUp,
			tamper: func(t *testing.T, workspace, migrationID string) {
				t.Helper()
				if err := os.Remove(filepath.Join(workspace, ".denova-migration", migrationID, "backup", "manifest.json")); err != nil {
					t.Fatal(err)
				}
			},
			step: MigrationStepBackup,
		},
		{
			name:  "corrupt backup file",
			fault: faultAfterBackedUp,
			tamper: func(t *testing.T, workspace, migrationID string) {
				t.Helper()
				path := filepath.Join(workspace, ".denova-migration", migrationID, "backup", "files", ".nova", "lore", "items.json")
				if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			step: MigrationStepBackup,
		},
		{
			name:  "corrupt stage marker",
			fault: faultAfterStaged,
			tamper: func(t *testing.T, workspace, migrationID string) {
				t.Helper()
				path := filepath.Join(workspace, ".denova-migration", migrationID, "stage", ".denova", "workspace-schema.json")
				if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			step: MigrationStepStage,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeWorkspaceTestFile(t, workspace, ".nova/lore/items.json", `{"items":[1]}`)
			preview, authorization := newMigrationTestRequest(t, workspace, "legacy-migration-01")
			first, err := executeMigrationUntil(t, preview, authorization, test.fault, nil)
			if err == nil {
				t.Fatalf("expected boundary fault at %s", test.fault)
			}
			test.tamper(t, workspace, authorization.MigrationID)
			fresh, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
			if err != nil {
				t.Fatal(err)
			}
			result, err := fresh.Resume(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
			migrationErr := assertMigrationError(t, err, CodeMigrationArtifactInvalid, test.step)
			if result.State != first.State || migrationErr.NextAction != MigrationNextManualRecovery {
				t.Fatalf("corrupt resume = %#v / %#v", result, migrationErr)
			}
			if _, statErr := os.Stat(filepath.Join(workspace, ".denova")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("corrupt artifact reached live destination: %v", statErr)
			}
		})
	}
}

func TestMigrationStageRejectsFileOutsideAuthorizedPublicationSet(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".nova/lore/items.json", `{"items":[1]}`)
	preview, authorization := newMigrationTestRequest(t, workspace, "legacy-migration-01")
	first, err := executeMigrationUntil(t, preview, authorization, faultAfterBackedUp, nil)
	if err == nil || first.State != MigrationBackedUp {
		t.Fatalf("backed-up boundary = %#v / %v", first, err)
	}
	roguePath := filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "stage", ".denova", "rogue.md")
	if err := os.MkdirAll(filepath.Dir(roguePath), 0o700); err != nil {
		t.Fatal(err)
	}
	rogueBytes := []byte("not authorized\n")
	if err := os.WriteFile(roguePath, rogueBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	fresh, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fresh.Resume(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	migrationErr := assertMigrationError(t, err, CodeMigrationArtifactInvalid, MigrationStepStage)
	if result.State != MigrationBackedUp || migrationErr.Path != ".denova-migration/legacy-migration-01/stage/.denova/rogue.md" {
		t.Fatalf("unauthorized stage result/error = %#v / %#v", result, migrationErr)
	}
	after, err := os.ReadFile(roguePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, rogueBytes) {
		t.Fatalf("unauthorized stage evidence was overwritten: %q", after)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".denova")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unauthorized stage reached live namespace: %v", statErr)
	}
}

func TestMigrationUnknownArtifactVersionsAreRejected(t *testing.T) {
	workspace := t.TempDir()
	base := `{"record_version":2,"migration_id":"migration-01","canonical_workspace":` + quoteJSONTest(t, workspace) + `}`
	if _, err := decodeBackupManifest([]byte(base)); err == nil {
		t.Fatal("unknown backup manifest version was accepted")
	}
	stage := `{"record_version":2,"migration_id":"migration-01","workspace_kind":"current_denova","canonical_workspace":` + quoteJSONTest(t, workspace) + `,"published_entry":".denova/workspace-schema.json","entries":[]}`
	if _, err := decodeStageManifest([]byte(stage)); err == nil {
		t.Fatal("unknown stage manifest version was accepted")
	}
	receipt := `{"record_version":2,"migration_id":"migration-01"}`
	if _, err := decodeMigrationReceipt([]byte(receipt)); err == nil {
		t.Fatal("unknown receipt version was accepted")
	}
}

func TestMigrationBackupManifestRejectsCanonicalEscape(t *testing.T) {
	workspace := t.TempDir()
	manifest := BackupManifest{
		RecordVersion:            BackupManifestVersionV1,
		MigrationID:              "migration-01",
		CanonicalWorkspace:       workspace,
		SourceExpectationsSHA256: sha256Hex([]byte("sources")),
		Entries: []BackupManifestEntry{{
			Path:       ".nova/lore/items.json",
			BackupPath: ".denova-migration/migration-01/backup/files/../../outside.json",
			Exists:     true,
			NodeType:   BackupNodeFile,
			Mode:       0o600,
			Size:       2,
			SHA256:     sha256Hex([]byte("{}")),
		}},
	}
	if _, err := encodeBackupManifest(manifest); err == nil {
		t.Fatal("backup manifest accepted an escaping artifact reference")
	}
}

func TestMigrationSourceChangeBeforeFirstWriteLeavesNoMigrationPaths(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, ".denova", "ideas.md")
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "v1\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	if err := os.WriteFile(path, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	assertMigrationError(t, err, CodeMigrationAuthorizationMismatch, MigrationStepAuthorize)
	if _, statErr := os.Stat(filepath.Join(workspace, MigrationRootRelativePath)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stale preview created migration state: %v", statErr)
	}
}

func TestMigrationRejectsIdenticalByteSourceReplacementAndModeChange(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, path string)
	}{
		{name: "identical byte replacement", mutate: func(t *testing.T, path string) {
			t.Helper()
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			replacement := path + ".replacement"
			if err := os.WriteFile(replacement, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(replacement, path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "mode change", mutate: func(t *testing.T, path string) {
			t.Helper()
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			path := filepath.Join(workspace, ".denova", "ideas.md")
			writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "same bytes\n")
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
			preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
			test.mutate(t, path)
			executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
			if err != nil {
				t.Fatal(err)
			}
			_, err = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
			assertMigrationError(t, err, CodeMigrationAuthorizationMismatch, MigrationStepAuthorize)
			if _, statErr := os.Stat(filepath.Join(workspace, MigrationRootRelativePath)); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("identity change created migration state: %v", statErr)
			}
		})
	}
}

func TestMigrationRejectsWorkspaceRootReplacementAtSameCanonicalPath(t *testing.T) {
	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "same bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	oldWorkspace := filepath.Join(parent, "workspace-old")
	if err := os.Rename(workspace, oldWorkspace); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".denova"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(oldWorkspace, ".denova", "ideas.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".denova", "ideas.md"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	assertMigrationError(t, err, CodeMigrationAuthorizationMismatch, MigrationStepAuthorize)
	if _, statErr := os.Stat(filepath.Join(workspace, MigrationRootRelativePath)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("replacement root received migration state: %v", statErr)
	}
}

func TestMigrationPostSwitchNormalReaderFailureCannotComplete(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	first, err := executeMigrationUntil(t, preview, authorization, faultAfterVerifying, nil)
	if err == nil || first.State != MigrationVerifying {
		t.Fatalf("first result/error = %#v / %v", first, err)
	}
	unsupported := schemaV1PreviewOptions()
	unsupported.Inspector.SupportedFeatures = map[string]string{}
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: unsupported})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Resume(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	assertMigrationError(t, err, CodeMigrationVerification, MigrationStepVerify)
	if result.State != MigrationVerifying {
		t.Fatalf("verification failure state = %s, want verifying", result.State)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, receiptRelativePath(authorization.MigrationID))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("verification failure published a terminal receipt: %v", statErr)
	}
	inspection, inspectErr := newSchemaV1Inspector(t, schemaV1PreviewOptions().Inspector.ApplicationVersion).Inspect(workspace)
	if inspectErr != nil || inspection.Marker.Contract.Migration != MigrationVerifying {
		t.Fatalf("verification failure falsely completed marker: %#v / %v", inspection, inspectErr)
	}
}

func TestMigrationExecutorRejectsMalformedNewerOrUnsupportedMarkerWithoutWriting(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "newer schema", mutate: func(marker map[string]any) { marker["schema_version"] = 2 }},
		{name: "unsupported required feature", mutate: func(marker map[string]any) {
			marker["features"] = map[string]any{"future_required": map[string]any{"version": "1.0.0", "required": true}}
		}},
		{name: "out of range writer", mutate: func(marker map[string]any) {
			marker["writer"].(map[string]any)["version"] = "2.0.0"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeSchemaMarker(t, workspace, newSchemaV1Marker(t, test.mutate))
			preview, err := BuildMigrationPreview(workspace, schemaV1PreviewOptions())
			if err != nil {
				t.Fatal(err)
			}
			authorization, err := BuildMigrationAuthorization(preview, "migration-01", AuthorConfirmation{ID: "confirmation", Evidence: "approved preview"})
			if err != nil {
				t.Fatal(err)
			}
			executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
			if err != nil {
				t.Fatal(err)
			}
			_, err = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
			assertMigrationError(t, err, CodeMigrationPreflight, MigrationStepValidate)
			if _, statErr := os.Stat(filepath.Join(workspace, MigrationRootRelativePath)); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("unsupported marker was mutated: %v", statErr)
			}
		})
	}
}

func TestMigrationExecutorRejectsAlreadyManagedV1WorkspaceAsNotRequiringMigration(t *testing.T) {
	workspace := t.TempDir()
	markerRaw := newSchemaV1Marker(t, func(marker map[string]any) {
		marker["migration"].(map[string]any)["state"] = string(MigrationCompleted)
	})
	writeSchemaMarker(t, workspace, markerRaw)
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	before := workspaceTreeDigest(t, workspace)
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	migrationErr := assertMigrationError(t, err, CodeMigrationPreflight, MigrationStepValidate)
	if migrationErr.WorkspaceMutated {
		t.Fatal("already-managed v1 rejection reported mutation")
	}
	if after := workspaceTreeDigest(t, workspace); !reflect.DeepEqual(after, before) {
		t.Fatalf("already-managed workspace was changed\nbefore: %#v\nafter: %#v", before, after)
	}
}

func quoteJSONTest(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
