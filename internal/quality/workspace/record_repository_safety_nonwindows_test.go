//go:build !windows

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactRepositoryRejectsSymlinkRecordIdentity(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	provider := &repositoryReferenceProvider{candidate: candidateRepositoryReferences()}
	repository := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, candidateRepositoryRaw(t, provider.candidate), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(workspace, filepath.FromSlash(candidateSetRecordRoot))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "candidate-set-001.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Read(context.Background(), "candidate-set-001"); err == nil {
		t.Fatal("Read followed symbolic-link record")
	}
}
