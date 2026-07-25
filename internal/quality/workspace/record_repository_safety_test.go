package workspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestArtifactRepositoryRejectsPortableCollisionAndBoundedRead(t *testing.T) {
	provider := &repositoryReferenceProvider{candidate: candidateRepositoryReferences()}
	raw := candidateRepositoryRaw(t, provider.candidate)

	t.Run("portable collision", func(t *testing.T) {
		workspace := managedRepositoryWorkspace(t)
		writeWorkspaceTestFile(t, workspace, candidateSetRecordRoot+"/CANDIDATE-SET-001.json", `{}`)
		repository := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
		if _, err := repository.Create(context.Background(), raw); !errors.Is(err, ErrRecordConflict) {
			t.Fatalf("Create error=%v, want portable ErrRecordConflict", err)
		}
	})

	t.Run("bounded read", func(t *testing.T) {
		workspace := managedRepositoryWorkspace(t)
		writeWorkspaceTestFile(t, workspace, candidateSetRecordRoot+"/candidate-set-001.json", string(raw))
		repository := newCandidateRepositoryWithLimitsForTest(t, workspace, provider, RecordRepositoryLimits{MaxArtifactBytes: int64(len(raw) - 1)})
		if _, err := repository.Read(context.Background(), "candidate-set-001"); !errors.Is(err, ErrRecordTooLarge) {
			t.Fatalf("Read error=%v, want ErrRecordTooLarge", err)
		}
	})
}

func TestArtifactRepositoryNonBlockingLockRejectsConcurrentWriter(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	provider := &repositoryReferenceProvider{candidate: candidateRepositoryReferences()}
	repository := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := ensureRootDirectoryTree(root, workspace, recordLockParent, 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireRecordRepositoryLock(root, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()

	_, err = repository.Create(context.Background(), candidateRepositoryRaw(t, provider.candidate))
	if !errors.Is(err, ErrRecordLocked) {
		t.Fatalf("Create error=%v, want ErrRecordLocked", err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, filepath.FromSlash(candidateSetRecordRoot), "candidate-set-001.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("locked writer changed target: %v", statErr)
	}
}

func TestArtifactRepositoryFaultsNeverExposePartialRecord(t *testing.T) {
	provider := &repositoryReferenceProvider{candidate: candidateRepositoryReferences()}
	raw := candidateRepositoryRaw(t, provider.candidate)
	for _, point := range []RepositoryFaultPoint{
		RepositoryFaultAfterWrite,
		RepositoryFaultBeforeFileSync,
		RepositoryFaultBeforePublish,
		RepositoryFaultBeforeParentSync,
	} {
		t.Run(string(point), func(t *testing.T) {
			workspace := managedRepositoryWorkspace(t)
			repository := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{Fail: func(got RepositoryFaultPoint) error {
				if got == point {
					return errors.New("injected fault")
				}
				return nil
			}})
			_, err := repository.Create(context.Background(), raw)
			if !errors.Is(err, ErrRecordDurability) {
				t.Fatalf("Create error=%v, want ErrRecordDurability", err)
			}
			target := filepath.Join(workspace, filepath.FromSlash(candidateSetRecordRoot), "candidate-set-001.json")
			got, readErr := os.ReadFile(target)
			if point == RepositoryFaultBeforeParentSync {
				if readErr != nil || !bytes.Equal(got, raw) {
					t.Fatalf("visible pre-sync record is not complete: got=%q err=%v", got, readErr)
				}
			} else if !errors.Is(readErr, os.ErrNotExist) {
				t.Fatalf("pre-publish fault exposed target: got=%q err=%v", got, readErr)
			}
			assertNoRepositoryTemps(t, workspace)
		})
	}
}

func TestRecordRepositoryInjectedIOFailuresPreserveCommittedBytes(t *testing.T) {
	provider := &repositoryReferenceProvider{candidate: candidateRepositoryReferences()}
	raw := candidateRepositoryRaw(t, provider.candidate)
	operationErr := errors.New("injected operation failure")

	createCases := []struct {
		name  string
		hooks RecordRepositoryHooks
	}{
		{"write", RecordRepositoryHooks{writeFile: func(*os.File, []byte) (int, error) { return 0, operationErr }}},
		{"file sync", RecordRepositoryHooks{syncFile: func(*os.File) error { return operationErr }}},
		{"create link", RecordRepositoryHooks{linkFile: func(*os.Root, string, string) error { return operationErr }}},
	}
	for _, test := range createCases {
		t.Run("create/"+test.name, func(t *testing.T) {
			workspace := managedRepositoryWorkspace(t)
			repository := newCandidateRepositoryForTest(t, workspace, provider, test.hooks)
			if _, err := repository.Create(context.Background(), raw); err == nil {
				t.Fatal("Create succeeded through injected I/O failure")
			}
			target := filepath.Join(workspace, filepath.FromSlash(candidateSetRecordRoot), "candidate-set-001.json")
			if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed create exposed target: %v", err)
			}
			assertNoRepositoryTemps(t, workspace)
		})
	}

	updateCases := []struct {
		name  string
		hooks func() RecordRepositoryHooks
	}{
		{"atomic replace", func() RecordRepositoryHooks {
			return RecordRepositoryHooks{replaceFile: func(*os.Root, string, string, string) (string, error) { return "", operationErr }}
		}},
		{"parent sync", func() RecordRepositoryHooks {
			calls := 0
			return RecordRepositoryHooks{syncParent: func(root *os.Root, workspace, rel string) error {
				calls++
				if calls == 1 {
					return operationErr
				}
				return syncRootDirectory(root, workspace, rel)
			}}
		}},
	}
	for _, test := range updateCases {
		t.Run("update/"+test.name, func(t *testing.T) {
			workspace := managedRepositoryWorkspace(t)
			base := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
			created, err := base.Create(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}
			faulted := newCandidateRepositoryForTest(t, workspace, provider, test.hooks())
			next := append(append([]byte(nil), raw...), ' ')
			if _, err := faulted.Update(context.Background(), "candidate-set-001", next, ArtifactUpdateExpectation{RawSHA256: created.RawSHA256, WorkspaceRevision: 1}); err == nil {
				t.Fatal("Update succeeded through injected I/O failure")
			}
			got, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(created.RelativePath)))
			if readErr != nil || !bytes.Equal(got, raw) {
				t.Fatalf("failed update lost committed bytes: got=%q err=%v", got, readErr)
			}
			assertNoRepositoryTemps(t, workspace)
		})
	}
}

func TestArtifactRepositoryDetectsInodeSubstitutionAtPublishBarrier(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	provider := &repositoryReferenceProvider{candidate: candidateRepositoryReferences()}
	raw := candidateRepositoryRaw(t, provider.candidate)
	repository := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
	created, err := repository.Create(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workspace, filepath.FromSlash(created.RelativePath))
	manual := []byte("external author replacement")
	hooked := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{Fail: func(point RepositoryFaultPoint) error {
		if point != RepositoryFaultAfterCASBeforePublish {
			return nil
		}
		if err := os.Rename(target, target+".author-backup"); err != nil {
			return err
		}
		return os.WriteFile(target, manual, 0o600)
	}})
	next := append(append([]byte(nil), raw...), ' ')
	_, err = hooked.Update(context.Background(), "candidate-set-001", next, ArtifactUpdateExpectation{RawSHA256: created.RawSHA256, WorkspaceRevision: 1})
	if !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("Update error=%v, want ErrRecordConflict", err)
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil || !bytes.Equal(got, manual) {
		t.Fatalf("inode substitution was overwritten: got=%q err=%v", got, readErr)
	}
	assertNoRepositoryTemps(t, workspace)
}

func TestRecordRepositoriesWriteOnlyOwnedQualityRecordPaths(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	provider := &repositoryReferenceProvider{
		candidate:  candidateRepositoryReferences(),
		issue:      reviewRepositoryReferences(),
		preference: preferenceRepositoryReferences(),
	}
	if _, err := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{}).Create(context.Background(), candidateRepositoryRaw(t, provider.candidate)); err != nil {
		t.Fatal(err)
	}
	if _, err := newReviewIssueRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{}).Create(context.Background(), reviewRepositoryRaw(t, provider.issue)); err != nil {
		t.Fatal(err)
	}
	preference := preferenceRepositoryRaw(t, provider.preference)
	if _, err := newPreferenceRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{}).Append(context.Background(), preference, PreferenceAppendExpectation{PriorRawSHA256: recordSHA256(nil)}); err != nil {
		t.Fatal(err)
	}

	var files []string
	if err := filepath.WalkDir(workspace, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			rel, relErr := filepath.Rel(workspace, path)
			if relErr != nil {
				return relErr
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	want := []string{
		".denova/quality/artifacts/candidate-sets/candidate-set-001.json",
		".denova/quality/artifacts/review-issues/review-issue-001.json",
		".denova/quality/preferences.jsonl",
		".denova/quality/runs/record-repositories.lock",
		".denova/workspace-schema.json",
	}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("repository write audit:\n got=%#v\nwant=%#v", files, want)
	}
}

func newCandidateRepositoryWithLimitsForTest(t *testing.T, workspace string, provider RecordReferenceProvider, limits RecordRepositoryLimits) *CandidateSetRepository {
	t.Helper()
	repository, err := NewCandidateSetRepository(RecordRepositoryConfig{
		Workspace: workspace,
		Inspector: InspectorOptions{
			ApplicationVersion: "1.6.2",
			SupportedFeatures:  map[string]string{"quality_harness": ">=1.0.0 <2.0.0"},
		},
		Decoder:    newRecordDecoderForTest(t),
		Lease:      &migrationTestLease{invoke: true},
		References: provider,
		Authority:  &repositoryAuthorityVerifier{},
		Limits:     limits,
	})
	if err != nil {
		t.Fatalf("NewCandidateSetRepository: %v", err)
	}
	return repository
}

func assertNoRepositoryTemps(t *testing.T, workspace string) {
	t.Helper()
	err := filepath.WalkDir(workspace, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("repository left temporary file %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
