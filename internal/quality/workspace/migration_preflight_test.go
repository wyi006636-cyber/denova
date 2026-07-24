package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationPreflightRejectsUnsafeCapabilitiesBeforeBackupOrStage(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(MigrationPreflightCapabilities) MigrationPreflightCapabilities
	}{
		{name: "permission", mutate: func(capabilities MigrationPreflightCapabilities) MigrationPreflightCapabilities {
			capabilities.Writable = false
			return capabilities
		}},
		{name: "space", mutate: func(capabilities MigrationPreflightCapabilities) MigrationPreflightCapabilities {
			capabilities.AvailableBytes = capabilities.RequiredBytes - 1
			return capabilities
		}},
		{name: "same filesystem", mutate: func(capabilities MigrationPreflightCapabilities) MigrationPreflightCapabilities {
			capabilities.SameFilesystem = false
			return capabilities
		}},
		{name: "atomic namespace rename", mutate: func(capabilities MigrationPreflightCapabilities) MigrationPreflightCapabilities {
			capabilities.AtomicNamespaceRename = false
			return capabilities
		}},
		{name: "long path capability", mutate: func(capabilities MigrationPreflightCapabilities) MigrationPreflightCapabilities {
			capabilities.LongPaths = false
			return capabilities
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
			preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
			executor, err := newMigrationExecutorWithDependencies(MigrationExecutorOptions{
				Lease:          &migrationTestLease{invoke: true},
				PreviewOptions: schemaV1PreviewOptions(),
			}, migrationExecutorDependencies{
				preflight: func(request MigrationPreflightRequest) (MigrationPreflightCapabilities, error) {
					return test.mutate(MigrationPreflightCapabilities{
						AvailableBytes:        request.RequiredBytes + 1024,
						RequiredBytes:         request.RequiredBytes,
						Writable:              true,
						SameFilesystem:        true,
						AtomicNamespaceRename: true,
						LongPaths:             true,
					}), nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
			migrationErr := assertMigrationError(t, err, CodeMigrationPreflight, MigrationStepPreflight)
			if result.State != MigrationPreviewed || !migrationErr.WorkspaceMutated {
				t.Fatalf("preflight result/error = %#v / %#v", result, migrationErr)
			}
			for _, rel := range []string{"backup", "stage"} {
				if _, statErr := os.Stat(filepath.Join(workspace, ".denova-migration", "migration-01", rel)); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("preflight failure created %s: %v", rel, statErr)
				}
			}
			if _, statErr := os.Stat(filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath))); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("preflight failure switched marker: %v", statErr)
			}
		})
	}
}

func TestMigrationPreflightProbeErrorIsStructuredAndRetainsPreviewedRecord(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	injected := errors.New("filesystem capability probe failed")
	executor, err := newMigrationExecutorWithDependencies(MigrationExecutorOptions{
		Lease:          &migrationTestLease{invoke: true},
		PreviewOptions: schemaV1PreviewOptions(),
	}, migrationExecutorDependencies{preflight: func(MigrationPreflightRequest) (MigrationPreflightCapabilities, error) {
		return MigrationPreflightCapabilities{}, injected
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	migrationErr := assertMigrationError(t, err, CodeMigrationPreflight, MigrationStepPreflight)
	if !errors.Is(migrationErr, injected) || result.State != MigrationPreviewed || migrationErr.NextAction != MigrationNextResume {
		t.Fatalf("probe error = %#v, result = %#v", migrationErr, result)
	}
	if record := readMigrationRecordTest(t, workspace, authorization.MigrationID); record.State != MigrationPreviewed {
		t.Fatalf("record state = %s, want previewed", record.State)
	}
}
