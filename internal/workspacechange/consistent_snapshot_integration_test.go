package workspacechange_test

import (
	"context"
	"fmt"
	"testing"

	"denova/internal/book/versions"
	"denova/internal/workspacechange"
)

func TestConsistentSnapshotCapturesCandidateBeforeLaterManagedMutation(t *testing.T) {
	workspace := t.TempDir()
	changeService, err := workspacechange.NewService(workspace)
	if err != nil {
		t.Fatalf("create workspace change service: %v", err)
	}
	path := "chapters/ch01.md"
	if _, err := changeService.SaveFile(context.Background(), path, "before", "missing"); err != nil {
		t.Fatalf("seed visible file: %v", err)
	}
	versionService := versions.NewService(workspace)
	settings := versions.DefaultAutoSettings()
	var candidateVersionID string

	applied, err := changeService.ReplaceFileWithConsistentSnapshot(
		context.Background(),
		workspacechange.ReplaceFileRequest{
			Path:         path,
			Content:      "candidate",
			BaseRevision: workspacechange.Revision([]byte("before")),
		},
		func(change workspacechange.ChangeSet) error {
			if change.Revision != workspacechange.Revision([]byte("candidate")) {
				return fmt.Errorf("callback revision = %q", change.Revision)
			}
			result, err := versionService.Create(
				"candidate snapshot",
				versions.VersionSourceManual,
				settings,
			)
			if err != nil {
				return err
			}
			if result.Version == nil {
				return fmt.Errorf("candidate snapshot returned no version")
			}
			candidateVersionID = result.Version.ID
			return nil
		},
	)
	if err != nil {
		t.Fatalf("replace and snapshot candidate: %v", err)
	}
	if candidateVersionID == "" {
		t.Fatal("callback did not capture a candidate version")
	}
	if _, err := changeService.SaveFile(
		context.Background(),
		path,
		"later",
		applied.Revision,
	); err != nil {
		t.Fatalf("commit later managed mutation: %v", err)
	}

	diff, err := versionService.Diff(candidateVersionID, path)
	if err != nil {
		t.Fatalf("diff candidate snapshot: %v", err)
	}
	if !diff.Text || diff.Original != "candidate" || diff.Modified != "later" {
		t.Fatalf("version diff did not preserve candidate before later bytes: %#v", diff)
	}
	content, revision, err := changeService.ReadFile(path)
	if err != nil {
		t.Fatalf("read live workspace: %v", err)
	}
	if content != "later" || revision != workspacechange.Revision([]byte("later")) {
		t.Fatalf("live workspace = content %q revision %q", content, revision)
	}
}
