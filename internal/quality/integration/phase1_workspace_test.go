package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	qualityworkspace "denova/internal/quality/workspace"
)

type integrationWorkspaceLease struct{}

func (integrationWorkspaceLease) WithExclusiveWorkspace(_ context.Context, mutate func() error) error {
	return mutate()
}

func TestPhase1WorkspaceOpenAndPreviewMatrixIsReadOnly(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		kind       qualityworkspace.WorkspaceKind
		activeRoot string
		sourceRoot string
	}{
		{
			name:       "fresh workspace",
			files:      map[string]string{"ideas.md": "fresh author idea\n"},
			kind:       qualityworkspace.WorkspaceKindNew,
			activeRoot: ".denova",
			sourceRoot: "",
		},
		{
			name: "canonical workspace",
			files: map[string]string{
				"chapters/0001.md":        "canonical author chapter\n",
				".denova/lore/items.json": `{"items":[]}`,
			},
			kind:       qualityworkspace.WorkspaceKindCurrent,
			activeRoot: ".denova",
			sourceRoot: ".denova",
		},
		{
			name: "legacy-only workspace",
			files: map[string]string{
				"chapters/0001.md":      "legacy author chapter\n",
				".nova/lore/items.json": `{"items":[]}`,
			},
			kind:       qualityworkspace.WorkspaceKindLegacy,
			activeRoot: ".nova",
			sourceRoot: ".nova",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeIntegrationFiles(t, workspace, test.files)
			before := integrationFileDigest(t, workspace, nil)

			inspector := newIntegrationInspector(t)
			inspection, err := inspector.Inspect(workspace)
			if err != nil {
				t.Fatalf("Inspect: %v", err)
			}
			if !inspection.CanOpen() || inspection.ActiveRoot != test.activeRoot {
				t.Fatalf("inspection = %#v", inspection)
			}

			preview, err := qualityworkspace.BuildMigrationPreview(workspace, integrationPreviewOptions())
			if err != nil {
				t.Fatalf("BuildMigrationPreview: %v", err)
			}
			if preview.Kind != test.kind || preview.SourceRoot != test.sourceRoot || preview.HasConflicts() {
				t.Fatalf("preview = %#v", preview)
			}
			if digest, err := qualityworkspace.PreviewDigest(preview); err != nil || digest == "" {
				t.Fatalf("PreviewDigest = %q, %v", digest, err)
			}

			after := integrationFileDigest(t, workspace, nil)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("inspection or preview wrote workspace\nbefore=%#v\nafter=%#v", before, after)
			}
		})
	}
}

func TestPhase1MigrationBackupExecuteResumeAndRollbackPreserveAuthorBytes(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "canonical adoption",
			files: map[string]string{
				"chapters/0001.md":        "canonical author bytes\n",
				".denova/lore/items.json": `{"items":[{"id":"canonical"}]}`,
			},
		},
		{
			name: "legacy migration",
			files: map[string]string{
				"chapters/0001.md":      "legacy author bytes\n",
				".nova/lore/items.json": `{"items":[{"id":"legacy"}]}`,
			},
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeIntegrationFiles(t, workspace, test.files)
			original := integrationFileDigest(t, workspace, migrationEvidencePath)
			preview, authorization := integrationMigrationRequest(t, workspace, fmt.Sprintf("phase1-migration-%02d", index+1))
			executor := newIntegrationMigrationExecutor(t)

			result, err := executor.Execute(context.Background(), qualityworkspace.MigrationRequest{
				Preview: preview, Authorization: authorization,
			})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if result.State != qualityworkspace.MigrationCompleted || !result.RollbackAvailable || result.Receipt == nil {
				t.Fatalf("migration result = %#v", result)
			}
			backupManifest := filepath.Join(workspace, ".denova-migration", authorization.MigrationID, "backup", "manifest.json")
			if info, err := os.Stat(backupManifest); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("verified backup manifest unavailable: %v", err)
			}

			completed := integrationFileDigest(t, workspace, nil)
			resumed, err := executor.Resume(context.Background(), qualityworkspace.MigrationRequest{
				Preview: preview, Authorization: authorization,
			})
			if err != nil || resumed.State != qualityworkspace.MigrationCompleted || resumed.WorkspaceMutated {
				t.Fatalf("Resume = %#v, %v", resumed, err)
			}
			if afterResume := integrationFileDigest(t, workspace, nil); !reflect.DeepEqual(afterResume, completed) {
				t.Fatal("completed migration replay changed bytes")
			}

			recovery, err := qualityworkspace.BuildMigrationRecoveryAuthorization(
				authorization,
				qualityworkspace.RecoveryActionRollback,
				qualityworkspace.AuthorConfirmation{ID: "phase1-rollback-01", Evidence: "author selected rollback for the completed Phase 1 migration"},
			)
			if err != nil {
				t.Fatalf("BuildMigrationRecoveryAuthorization: %v", err)
			}
			rolledBack, err := executor.Rollback(context.Background(), qualityworkspace.MigrationRecoveryRequest{
				Migration:     qualityworkspace.MigrationRequest{Preview: preview, Authorization: authorization},
				Authorization: recovery,
			})
			if err != nil || rolledBack.State != qualityworkspace.MigrationRolledBack {
				t.Fatalf("Rollback = %#v, %v", rolledBack, err)
			}

			restored := integrationFileDigest(t, workspace, migrationEvidencePath)
			if !reflect.DeepEqual(restored, original) {
				t.Fatalf("rollback changed author file set or bytes\noriginal=%#v\nrestored=%#v", original, restored)
			}
		})
	}
}

func TestPhase1FreshWorkspaceAdoptionNeedsNoBackupOrRollback(t *testing.T) {
	workspace := t.TempDir()
	writeIntegrationFiles(t, workspace, map[string]string{"ideas.md": "fresh workspace bytes\n"})
	authorBefore := integrationFileDigest(t, workspace, nil)
	preview, authorization := integrationMigrationRequest(t, workspace, "phase1-fresh-01")

	result, err := newIntegrationMigrationExecutor(t).Execute(context.Background(), qualityworkspace.MigrationRequest{
		Preview: preview, Authorization: authorization,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.State != qualityworkspace.MigrationNotRequired || result.RollbackAvailable || result.Receipt != nil {
		t.Fatalf("fresh adoption result = %#v", result)
	}
	if got := readIntegrationFile(t, workspace, "ideas.md"); got != "fresh workspace bytes\n" {
		t.Fatalf("fresh author bytes = %q", got)
	}
	if _, exists := authorBefore["ideas.md"]; !exists {
		t.Fatal("fresh workspace fixture did not contain author file")
	}
	if _, err := os.Stat(filepath.Join(workspace, ".denova-migration")); !os.IsNotExist(err) {
		t.Fatalf("fresh adoption created migration residue: %v", err)
	}
}

func integrationPreviewOptions() qualityworkspace.PreviewOptions {
	return qualityworkspace.PreviewOptions{
		Inspector: qualityworkspace.InspectorOptions{
			ApplicationVersion: "1.7.0",
			SupportedFeatures: map[string]string{
				"quality_harness": ">=1.0.0 <2.0.0",
			},
		},
		TargetFeatures: map[string]qualityworkspace.FeatureContract{
			"quality_harness": {Version: "1.1.0", Required: true},
		},
	}
}

func newIntegrationInspector(t *testing.T) *qualityworkspace.Inspector {
	t.Helper()
	inspector, err := qualityworkspace.NewInspector(integrationPreviewOptions().Inspector)
	if err != nil {
		t.Fatal(err)
	}
	return inspector
}

func integrationMigrationRequest(t *testing.T, workspace, migrationID string) (qualityworkspace.MigrationPreview, qualityworkspace.MigrationAuthorization) {
	t.Helper()
	preview, err := qualityworkspace.BuildMigrationPreview(workspace, integrationPreviewOptions())
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := qualityworkspace.BuildMigrationAuthorization(preview, migrationID, qualityworkspace.AuthorConfirmation{
		ID: "phase1-author-confirmation-01", Evidence: "author approved this exact Phase 1 migration preview",
	})
	if err != nil {
		t.Fatal(err)
	}
	return preview, authorization
}

func newIntegrationMigrationExecutor(t *testing.T) *qualityworkspace.MigrationExecutor {
	t.Helper()
	executor, err := qualityworkspace.NewMigrationExecutor(qualityworkspace.MigrationExecutorOptions{
		Lease: integrationWorkspaceLease{}, PreviewOptions: integrationPreviewOptions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func writeIntegrationFiles(t *testing.T, workspace string, files map[string]string) {
	t.Helper()
	for relative, content := range files {
		path := filepath.Join(workspace, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readIntegrationFile(t *testing.T, workspace, relative string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func migrationEvidencePath(relative string) bool {
	return relative == ".denova-migration" || len(relative) > len(".denova-migration/") && relative[:len(".denova-migration/")] == ".denova-migration/"
}

func integrationFileDigest(t *testing.T, workspace string, exclude func(string) bool) map[string]string {
	t.Helper()
	result := make(map[string]string)
	err := filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == workspace {
			return nil
		}
		relative, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if exclude != nil && exclude(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(raw)
		result[relative] = hex.EncodeToString(digest[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
