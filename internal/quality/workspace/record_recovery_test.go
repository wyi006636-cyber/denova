package workspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestArtifactRepositoryPersistentParentSyncFailurePreservesInspectableRecoveryBytes(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	provider := &repositoryReferenceProvider{candidate: candidateRepositoryReferences()}
	raw := candidateRepositoryRaw(t, provider.candidate)
	base := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
	created, err := base.Create(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}

	next := append(append([]byte(nil), raw...), ' ')
	faulted := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{
		syncParent: func(*os.Root, string, string) error {
			return errors.New("persistent parent sync failure")
		},
	})
	_, err = faulted.Update(context.Background(), "candidate-set-001", next, ArtifactUpdateExpectation{
		RawSHA256:         created.RawSHA256,
		WorkspaceRevision: created.WorkspaceRevision,
	})
	if !errors.Is(err, ErrRecordDurability) {
		t.Fatalf("Update error=%v, want ErrRecordDurability", err)
	}
	got, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(created.RelativePath)))
	if readErr != nil || !bytes.Equal(got, raw) {
		t.Fatalf("failed update lost prior committed bytes: got=%q err=%v", got, readErr)
	}

	ids, err := faulted.List(context.Background())
	if err != nil || !reflect.DeepEqual(ids, []string{"candidate-set-001"}) {
		t.Fatalf("List with recovery sibling=%v err=%v", ids, err)
	}
	recovery, err := faulted.ListRecovery(context.Background())
	if err != nil {
		t.Fatalf("ListRecovery: %v", err)
	}
	if len(recovery) != 1 {
		t.Fatalf("ListRecovery=%#v, want one preserved complete sibling", recovery)
	}
	entry := recovery[0]
	if entry.TargetRelativePath != created.RelativePath || entry.Kind != RecordRecoveryTemporarySibling || entry.RawSHA256 != recordSHA256(next) || entry.Size != int64(len(next)) {
		t.Fatalf("recovery entry=%#v", entry)
	}
	preserved, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(entry.PreservedRelativePath)))
	if err != nil || !bytes.Equal(preserved, next) {
		t.Fatalf("preserved recovery bytes=%q err=%v", preserved, err)
	}
}

func TestArtifactRepositoryWithdrawsCreateWhenStagedSiblingIsSubstituted(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	provider := &repositoryReferenceProvider{candidate: candidateRepositoryReferences()}
	raw := candidateRepositoryRaw(t, provider.candidate)
	foreign := []byte("external staged substitution")
	repository := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{
		linkFile: func(root *os.Root, from, to string) error {
			if err := root.Remove(filepath.FromSlash(from)); err != nil {
				return err
			}
			if err := root.WriteFile(filepath.FromSlash(from), foreign, 0o600); err != nil {
				return err
			}
			return root.Link(filepath.FromSlash(from), filepath.FromSlash(to))
		},
	})
	_, err := repository.Create(context.Background(), raw)
	if !errors.Is(err, ErrRecordConflict) || !errors.Is(err, ErrRecordRecoveryRequired) {
		t.Fatalf("Create error=%v, want conflict with explicit recovery", err)
	}
	target, _ := CandidateSetRelativePath("candidate-set-001")
	if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(target))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed create left a repository target: %v", err)
	}
	ids, err := repository.List(context.Background())
	if err != nil || len(ids) != 0 {
		t.Fatalf("List after withdrawn create=%v err=%v", ids, err)
	}
	recovery, err := repository.ListRecovery(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hashes := make(map[string]RecordRecoveryKind, len(recovery))
	for _, entry := range recovery {
		hashes[entry.RawSHA256] = entry.Kind
	}
	if hashes[recordSHA256(raw)] != RecordRecoveryTemporarySibling || hashes[recordSHA256(foreign)] != RecordRecoveryCreateConflict {
		t.Fatalf("recovery=%#v, want typed complete intended and displaced bytes", recovery)
	}
}

func TestArtifactRepositoryRecoveryRequiredUpdatePreservesIntendedBytes(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	provider := &repositoryReferenceProvider{candidate: candidateRepositoryReferences()}
	raw := candidateRepositoryRaw(t, provider.candidate)
	base := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
	created, err := base.Create(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	next := append(append([]byte(nil), raw...), ' ')
	faulted := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{
		replaceFile: func(root *os.Root, workspace, target, replacement string) (string, error) {
			displaced, err := replaceRecordAtomically(root, workspace, target, replacement)
			if err != nil {
				return "", err
			}
			if err := restoreRecordAtomically(root, workspace, target, displaced, replacement); err != nil {
				return "", err
			}
			return "", errors.Join(ErrRecordRecoveryRequired, ErrRecordConflict)
		},
	})
	_, err = faulted.Update(context.Background(), "candidate-set-001", next, ArtifactUpdateExpectation{
		RawSHA256:         created.RawSHA256,
		WorkspaceRevision: created.WorkspaceRevision,
	})
	if !errors.Is(err, ErrRecordRecoveryRequired) {
		t.Fatalf("Update error=%v, want ErrRecordRecoveryRequired", err)
	}
	current, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(created.RelativePath)))
	if err != nil || !bytes.Equal(current, raw) {
		t.Fatalf("reconciled target=%q err=%v, want prior bytes", current, err)
	}
	recovery, err := faulted.ListRecovery(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	foundIntended := false
	for _, entry := range recovery {
		foundIntended = foundIntended || entry.RawSHA256 == recordSHA256(next)
	}
	if !foundIntended {
		t.Fatalf("recovery=%#v, intended update bytes were not retained", recovery)
	}
}
