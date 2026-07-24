package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type migrationTestLease struct {
	invoke bool
	calls  int
}

func (lease *migrationTestLease) WithExclusiveWorkspace(_ context.Context, fn func() error) error {
	lease.calls++
	if !lease.invoke {
		return nil
	}
	return fn()
}

func TestMigrationExecutorRequiresSharedLeaseCallbackBeforeAnyWrite(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, "ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	before := workspaceTreeDigest(t, workspace)
	lease := &migrationTestLease{invoke: false}
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{
		Lease:          lease,
		PreviewOptions: schemaV1PreviewOptions(),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	migrationErr := assertMigrationError(t, err, CodeMigrationLeaseViolation, MigrationStepAcquireLease)
	if migrationErr.WorkspaceMutated {
		t.Fatal("bypassed lease must report workspace_mutated=false")
	}
	if lease.calls != 1 {
		t.Fatalf("lease calls = %d, want 1", lease.calls)
	}
	if after := workspaceTreeDigest(t, workspace); !reflect.DeepEqual(after, before) {
		t.Fatalf("workspace changed without a lease callback\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestDurableRootWriteOrdersFileAndDirectoryDurability(t *testing.T) {
	workspace := t.TempDir()
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	events := make([]durableWriteEvent, 0, 5)
	written, err := durableRootWriteObserved(root, workspace, "record.json", []byte("durable bytes\n"), 0o600, func(event durableWriteEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("first durable publication must report a visible write")
	}
	want := []durableWriteEvent{
		durableWriteContentsWritten,
		durableWriteFileSynced,
		durableWriteFileClosed,
		durableWriteRenamed,
		durableWriteParentSynced,
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("durability events = %v, want %v", events, want)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "record.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "durable bytes\n" {
		t.Fatalf("published bytes = %q", raw)
	}
}

func TestMigrationExecutorRejectsAuthorizationMismatchInsideLeaseWithZeroWrites(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, "ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	authorization.CanonicalWorkspace += "-wrong"
	before := workspaceTreeDigest(t, workspace)
	lease := &migrationTestLease{invoke: true}
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: lease, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}

	_, err = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	assertMigrationError(t, err, CodeMigrationAuthorizationMismatch, MigrationStepAuthorize)
	if after := workspaceTreeDigest(t, workspace); !reflect.DeepEqual(after, before) {
		t.Fatalf("authorization mismatch wrote paths\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestMigrationExecutorDurablyPersistsPreviewedUnderLease(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	lease := &migrationTestLease{invoke: true}
	injected := errors.New("stop after previewed")
	executor, err := newMigrationExecutorWithDependencies(MigrationExecutorOptions{
		Lease:          lease,
		PreviewOptions: schemaV1PreviewOptions(),
	}, migrationExecutorDependencies{
		fault: func(point migrationFaultPoint) error {
			if point == faultAfterPreviewed {
				return injected
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	if !errors.Is(err, injected) {
		t.Fatalf("Execute error = %v, want injected failure", err)
	}
	if lease.calls != 1 {
		t.Fatalf("lease calls = %d, want 1", lease.calls)
	}
	recordPath := filepath.Join(workspace, ".denova-migration", "migration-01", "record.json")
	raw, readErr := os.ReadFile(recordPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	record, decodeErr := decodeMigrationRecord(raw)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if record.State != MigrationPreviewed || record.NextAction != MigrationNextValidate {
		t.Fatalf("record state/next = %s/%s, want previewed/validate", record.State, record.NextAction)
	}
	if record.AuthorizationSHA256 != authorization.PayloadSHA256 || record.PreviewSHA256 != authorization.PreviewSHA256 {
		t.Fatal("durable record lost authorization or preview binding")
	}
}

func TestMigrationExecutorPreservesInvalidRecordBytesAndRejectsDifferentPayload(t *testing.T) {
	t.Run("invalid record", func(t *testing.T) {
		workspace := t.TempDir()
		writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
		preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
		recordPath := filepath.Join(workspace, ".denova-migration", "migration-01", "record.json")
		if err := os.MkdirAll(filepath.Dir(recordPath), 0o755); err != nil {
			t.Fatal(err)
		}
		original := []byte(`{"record_version":1,"record_version":1}`)
		if err := os.WriteFile(recordPath, original, 0o600); err != nil {
			t.Fatal(err)
		}
		executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
		if err != nil {
			t.Fatal(err)
		}

		_, err = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
		assertMigrationError(t, err, CodeMigrationRecordInvalid, MigrationStepLoadRecord)
		after, readErr := os.ReadFile(recordPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !reflect.DeepEqual(after, original) {
			t.Fatalf("invalid record bytes were changed\n got: %q\nwant: %q", after, original)
		}
	})

	t.Run("same id different confirmation", func(t *testing.T) {
		workspace := t.TempDir()
		writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
		preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
		executor, err := newMigrationExecutorWithDependencies(MigrationExecutorOptions{
			Lease:          &migrationTestLease{invoke: true},
			PreviewOptions: schemaV1PreviewOptions(),
		}, migrationExecutorDependencies{fault: func(point migrationFaultPoint) error {
			if point == faultAfterPreviewed {
				return errors.New("stop")
			}
			return nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})

		recordPath := filepath.Join(workspace, ".denova-migration", "migration-01", "record.json")
		before, readErr := os.ReadFile(recordPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		different, buildErr := BuildMigrationAuthorization(preview, "migration-01", AuthorConfirmation{ID: "confirmation-02", Evidence: "different approval"})
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		executor, err = NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
		if err != nil {
			t.Fatal(err)
		}
		_, err = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: different})
		assertMigrationError(t, err, CodeMigrationRecordConflict, MigrationStepLoadRecord)
		after, readErr := os.ReadFile(recordPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !reflect.DeepEqual(after, before) {
			t.Fatal("payload conflict must preserve the existing record bytes")
		}
	})
}

func TestMigrationExecutorRejectsPortableMigrationIDCollision(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "author bytes\n")
	if err := os.MkdirAll(filepath.Join(workspace, ".denova-migration", "Migration-01"), 0o755); err != nil {
		t.Fatal(err)
	}
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	executor, err := NewMigrationExecutor(MigrationExecutorOptions{Lease: &migrationTestLease{invoke: true}, PreviewOptions: schemaV1PreviewOptions()})
	if err != nil {
		t.Fatal(err)
	}

	_, err = executor.Execute(context.Background(), MigrationRequest{Preview: preview, Authorization: authorization})
	assertMigrationError(t, err, CodeMigrationIDCollision, MigrationStepLoadRecord)
	entries, readErr := os.ReadDir(filepath.Join(workspace, ".denova-migration"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if entry.Name() == "migration-01" {
			t.Fatal("colliding migration directory was created with the requested spelling")
		}
	}
}

func newMigrationTestRequest(t *testing.T, workspace, migrationID string) (MigrationPreview, MigrationAuthorization) {
	t.Helper()
	preview, err := BuildMigrationPreview(workspace, schemaV1PreviewOptions())
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := BuildMigrationAuthorization(preview, migrationID, AuthorConfirmation{
		ID:       "author-confirmation-01",
		Evidence: "author explicitly approved this exact preview",
	})
	if err != nil {
		t.Fatal(err)
	}
	return preview, authorization
}
